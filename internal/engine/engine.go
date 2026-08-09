package engine

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/forward"
)

// Status describes the runtime state of a forwarding rule.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

// RuleType discriminates local and remote forwarding rules.
type RuleType string

const (
	RuleTypeLocal  RuleType = "local"
	RuleTypeRemote RuleType = "remote"
)

// Rule is a single forwarding rule. Local rules bind a listener on this
// machine; remote rules are registered with a server through the tunnel.
type Rule struct {
	ID     string   `json:"id"`
	Type   RuleType `json:"type"`
	Name   string   `json:"name"`
	Listen string   `json:"listen"`
	Target string   `json:"target"`
	Status Status   `json:"status"`
	// Credential names the SSH credential a remote rule should use. Empty
	// means the remoted backend's default (e.g. the `pf server` tunnel).
	Credential string `json:"credential,omitempty"`
}

// RemoteBackend registers remote rules on a server. It is provided by the
// tunnel layer and injected by the application wiring. Implementations must
// be safe for concurrent use.
type RemoteBackend interface {
	AddRemote(rule Rule)
	RemoveRemote(id string)
}

// Engine owns a set of rules and their listeners.
type Engine struct {
	mu      sync.Mutex
	rules   map[string]*Rule
	lns     map[string]net.Listener
	stop    map[string]chan struct{}
	nextID  int
	onError func(id string, err error)
	remote  RemoteBackend
}

// New creates an empty Engine.
func New() *Engine {
	return &Engine{
		rules:  make(map[string]*Rule),
		lns:    make(map[string]net.Listener),
		stop:   make(map[string]chan struct{}),
		nextID: 1,
	}
}

// SetErrorHandler registers a callback invoked when a rule reports an error.
func (e *Engine) SetErrorHandler(fn func(id string, err error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onError = fn
}

// SetRemoteBackend attaches a tunnel backend used to service remote rules.
func (e *Engine) SetRemoteBackend(b RemoteBackend) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.remote = b
}

// Add registers a new rule and starts it. For remote rules it only records
// the rule and delegates registration to the tunnel backend; the status is
// updated later via UpdateStatus. It returns the created rule.
func (e *Engine) Add(typ RuleType, name, listen, target, credential string) (*Rule, error) {
	r := &Rule{
		ID:         fmt.Sprintf("%d", e.nextID),
		Type:       typ,
		Name:       name,
		Listen:     listen,
		Target:     target,
		Credential: credential,
		Status:     StatusRunning,
	}
	e.mu.Lock()
	e.nextID++
	e.rules[r.ID] = r
	e.mu.Unlock()

	if typ == RuleTypeRemote {
		e.mu.Lock()
		remote := e.remote
		e.mu.Unlock()
		if remote == nil {
			e.setStatus(r, StatusError)
			return r, errors.New("remote forwarding backend not configured")
		}
		e.setStatus(r, StatusPending)
		// Called without e.mu held: the backend may re-enter the engine.
		remote.AddRemote(*r)
		return r, nil
	}

	if err := e.startLocked(r); err != nil {
		r.Status = StatusError
		return r, err
	}
	return r, nil
}

// setStatus updates a rule's status under the engine lock.
func (e *Engine) setStatus(r *Rule, s Status) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r.Status = s
}

// Remove stops and deletes a rule by ID.
func (e *Engine) Remove(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.rules[id]
	if !ok {
		return errors.New("rule not found")
	}
	if r.Type == RuleTypeRemote {
		if e.remote != nil {
			e.remote.RemoveRemote(id)
		}
		delete(e.rules, id)
		return nil
	}
	e.stopLocked(r)
	delete(e.rules, id)
	return nil
}

// Update re-registers a rule with new forwarding details, preserving its ID.
// It stops the old listener (local) or re-registers on the backend (remote).
// Update re-registers a rule with new forwarding details, preserving its ID.
// It stops the old listener (local) or re-registers on the backend (remote).
func (e *Engine) Update(id, name, listen, target, credential string) (*Rule, error) {
	e.mu.Lock()
	r, ok := e.rules[id]
	if !ok {
		e.mu.Unlock()
		return nil, errors.New("rule not found")
	}
	typ := r.Type
	remote := e.remote

	if typ == RuleTypeRemote {
		r.Name, r.Listen, r.Target, r.Credential = name, listen, target, credential
		r.Status = StatusPending
		e.mu.Unlock()
		if remote != nil {
			remote.RemoveRemote(id)
			remote.AddRemote(*r)
		}
		return r, nil
	}

	e.stopLocked(r)
	r.Name, r.Listen, r.Target = name, listen, target
	r.Status = StatusRunning
	err := e.startLocked(r)
	if err != nil {
		r.Status = StatusError
	}
	e.mu.Unlock()
	if err != nil {
		return r, err
	}
	return r, nil
}

// UpdateRemoteStatus reflects the tunnel backend's report for a remote rule.
// actualListen may carry the address the server actually bound; a non-nil
// error marks the rule as failed.
func (e *Engine) UpdateRemoteStatus(id, actualListen string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.rules[id]
	if !ok {
		return
	}
	if err != nil {
		r.Status = StatusError
		e.reportError(id, err)
		return
	}
	if actualListen != "" {
		r.Listen = actualListen
	}
	r.Status = StatusRunning
}

// Get returns the rule with the given ID, or nil.
func (e *Engine) Get(id string) *Rule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rules[id]
}

// List returns all rules, in creation order.
func (e *Engine) List() []*Rule {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]*Rule, 0, len(e.rules))
	for i := 1; i <= e.nextID; i++ {
		id := fmt.Sprintf("%d", i)
		if r, ok := e.rules[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// Restart stops and restarts every running local rule, preserving state.
// Remote rules are left to the backend.
func (e *Engine) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range e.rules {
		if r.Type == RuleTypeRemote {
			continue
		}
		e.stopLocked(r)
		if err := e.startLocked(r); err != nil {
			r.Status = StatusError
			e.reportError(r.ID, err)
			continue
		}
		r.Status = StatusRunning
	}
}

// Restore re-registers persisted rules, reusing their IDs so the frontend
// keeps stable handles. Each rule is (re)started like a fresh Add.
func (e *Engine) Restore(rules []*Rule) {
	for _, r := range rules {
		e.mu.Lock()
		if n, err := strconv.Atoi(r.ID); err == nil && n >= e.nextID {
			e.nextID = n + 1
		}
		// Remember the ID inside the rule too; Add normally drops it.
		r2 := &Rule{
			ID:         r.ID,
			Type:       r.Type,
			Name:       r.Name,
			Listen:     r.Listen,
			Target:     r.Target,
			Credential: r.Credential,
		}
		e.rules[r.ID] = r2
		e.mu.Unlock()

		if r.Type == RuleTypeRemote {
			e.mu.Lock()
			remote := e.remote
			e.mu.Unlock()
			if remote == nil {
				e.setStatus(r2, StatusError)
				continue
			}
			e.setStatus(r2, StatusPending)
			remote.AddRemote(*r2)
			continue
		}
		e.mu.Lock()
		err := e.startLocked(r2)
		if err != nil {
			r2.Status = StatusError
		}
		e.mu.Unlock()
	}
}

// startLocked binds the listener for r and spawns the accept loop.
// Caller must hold e.mu.
func (e *Engine) startLocked(r *Rule) error {
	ln, err := net.Listen("tcp", r.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", r.Listen, err)
	}
	stopCh := make(chan struct{})
	e.lns[r.ID] = ln
	e.stop[r.ID] = stopCh
	go e.acceptLoop(r, ln, stopCh)
	return nil
}

// stopLocked closes the listener for r and waits for its loop to exit.
// Caller must hold e.mu.
func (e *Engine) stopLocked(r *Rule) {
	ln, ok := e.lns[r.ID]
	if !ok {
		return
	}
	_ = ln.Close()
	close(e.stop[r.ID])
	delete(e.lns, r.ID)
	delete(e.stop, r.ID)
	r.Status = StatusStopped
}

func (e *Engine) acceptLoop(r *Rule, ln net.Listener, stopCh chan struct{}) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			e.reportError(r.ID, err)
			return
		}
		go e.handleConn(r, conn)
	}
}

func (e *Engine) handleConn(r *Rule, client net.Conn) {
	defer client.Close()

	upstream, err := net.DialTimeout("tcp", r.Target, 10*time.Second)
	if err != nil {
		e.reportError(r.ID, fmt.Errorf("dial %s: %w", r.Target, err))
		return
	}
	defer upstream.Close()
	forward.Relay(client, upstream)
}

func (e *Engine) reportError(id string, err error) {
	e.mu.Lock()
	fn := e.onError
	e.mu.Unlock()
	if fn != nil {
		fn(id, err)
	}
}
