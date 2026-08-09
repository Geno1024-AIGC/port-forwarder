package sshx

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	"golang.org/x/crypto/ssh"
)

// Manager owns credential-backed reverse-forward clients and implements
// engine.RemoteBackend. Each credential gets its own SSH connection so rules
// can target different public hosts.
type Manager struct {
	store *CredStore
	ctx   context.Context

	mu      sync.Mutex
	clients map[string]*Client // credential ID -> live client
	cancel  map[string]context.CancelFunc
	byRule  map[string]string      // rule ID -> credential ID
	rules   map[string]engine.Rule // rule ID -> own rule
	onRule  func(id, listen string, err error)
}

// NewManager creates a manager backed by store. If ctx is nil a background
// context is used; callers may later call Run to re-assert a live context.
func NewManager(ctx context.Context, store *CredStore) *Manager {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Manager{
		store:   store,
		ctx:     ctx,
		clients: make(map[string]*Client),
		cancel:  make(map[string]context.CancelFunc),
		byRule:  make(map[string]string),
		rules:   make(map[string]engine.Rule),
	}
}

// SetOnRule routes status reports from every client to one global callback.
func (m *Manager) SetOnRule(fn func(id, listen string, err error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onRule = fn
}

// Store returns the backing credential store.
func (m *Manager) Store() *CredStore { return m.store }

// AddRemote is engine.RemoteBackend: register a remote rule in the SSH layer
// keyed by the credential the rule references.
func (m *Manager) AddRemote(rule engine.Rule) {
	m.mu.Lock()
	m.rules[rule.ID] = rule
	m.byRule[rule.ID] = rule.Credential
	m.mu.Unlock()

	if cl := m.ensureClient(rule.Credential); cl != nil {
		cl.Add(ClientRule{ID: rule.ID, Name: rule.Name, Listen: rule.Listen, Target: rule.Target})
	} else {
		m.report(rule.ID, "", fmt.Errorf("credential %q unavailable", rule.Credential))
	}
}

// RemoveRemote stops the rule's reverse forward.
func (m *Manager) RemoveRemote(id string) {
	m.mu.Lock()
	credID := m.byRule[id]
	cl := m.clients[credID]
	delete(m.rules, id)
	delete(m.byRule, id)
	m.mu.Unlock()
	if cl != nil {
		cl.Remove(id)
	}
}

// RemoveCredential tears down a credential's client and all rules that used
// it, then forgets them in the store.
func (m *Manager) RemoveCredential(id string) error {
	m.mu.Lock()
	cl := m.clients[id]
	cd := m.cancel[id]
	var removed []string
	for rid, rs := range m.rules {
		if rs.Credential == id {
			delete(m.rules, rid)
			delete(m.byRule, rid)
			removed = append(removed, rid)
		}
	}
	delete(m.clients, id)
	delete(m.cancel, id)
	m.mu.Unlock()

	if cd != nil {
		cd()
	}
	if cl != nil {
		cl.Shutdown()
	}
	if _, exists := m.store.Get(id); !exists {
		return fmt.Errorf("credential %s not found", id)
	}
	return m.store.Delete(id)
}

// ensureClient returns an existing client for a credential, or starts one.
func (m *Manager) ensureClient(credID string) *Client {
	m.mu.Lock()
	if cl, ok := m.clients[credID]; ok {
		m.mu.Unlock()
		return cl
	}
	m.mu.Unlock()

	c, ok := m.store.Get(credID)
	if !ok {
		return nil
	}
	auth, err := CredentialAuth(c)
	if err != nil {
		return nil
	}
	cl := NewClient(m.normalizeHost(c.Host), c.User, auth)
	cl.SetOnRule(func(id, listen string, err error) {
		m.report(id, listen, err)
	})

	ctx, cancel := context.WithCancel(m.ctx)
	go cl.Run(ctx)

	m.mu.Lock()
	if existing, ok := m.clients[credID]; ok {
		m.mu.Unlock()
		cancel()
		return existing
	}
	m.clients[credID] = cl
	m.cancel[credID] = cancel
	m.mu.Unlock()
	return cl
}

func (m *Manager) normalizeHost(h string) string {
	_, _, err := net.SplitHostPort(h)
	if err != nil {
		return h + ":22"
	}
	return h
}

func (m *Manager) report(id, listen string, err error) {
	m.mu.Lock()
	fn := m.onRule
	m.mu.Unlock()
	if fn != nil {
		fn(id, listen, err)
	}
}

// Probe verifies a credential can actually log into its host.
func (m *Manager) Probe(id string) error {
	c, ok := m.store.Get(id)
	if !ok {
		return fmt.Errorf("credential %s not found", id)
	}
	auth, err := CredentialAuth(c)
	if err != nil {
		return err
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("tcp", m.normalizeHost(c.Host), cfg)
	if err != nil {
		return fmt.Errorf("ssh %s: %w", c.Host, err)
	}
	_ = conn.Close()
	return nil
}
