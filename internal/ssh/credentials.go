package sshx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Credential describes how to log into a public sshd for reverse forwarding.
// Credentials are stored separately from rules so rules simply reference an ID.
type Credential struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	User       string `json:"user"`
	AuthType   string `json:"authType"` // "key" or "password"
	KeyPath    string `json:"keyPath,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	Password   string `json:"password,omitempty"`
}

// CredStore persists credentials to a JSON file with owner-only permissions.
type CredStore struct {
	mu   sync.Mutex
	path string
	next int
	cs   map[string]*Credential
}

// NewCredStore loads credentials from path, creating an empty store if the
// file does not exist yet.
func NewCredStore(path string) (*CredStore, error) {
	s := &CredStore{path: path, cs: make(map[string]*Credential)}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var list []*Credential
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, c := range list {
		if c == nil {
			continue
		}
		s.cs[c.ID] = c
		if n := atoiName(c.ID); n >= s.next {
			s.next = n + 1
		}
	}
	return s, nil
}

// DefaultCredPath returns the conventional credentials location.
func DefaultCredPath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "pf", "credentials.json")
	}
	return filepath.Join(os.TempDir(), "pf-credentials.json")
}

// List returns all credentials, newest first.
func (s *CredStore) List() []*Credential {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Credential, 0, len(s.cs))
	for _, c := range s.cs {
		out = append(out, c)
	}
	return out
}

// Get returns a credential by ID.
func (s *CredStore) Get(id string) (*Credential, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cs[id]
	return c, ok
}

// Add assigns an ID and persists the new credential.
func (s *CredStore) Add(c *Credential) (*Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	c.ID = fmt.Sprintf("credp-%d", s.next)
	s.cs[c.ID] = c
	if err := s.saveLocked(); err != nil {
		delete(s.cs, c.ID)
		return nil, err
	}
	return c, nil
}

// Update replaces the fields of an existing credential.
func (s *CredStore) Update(id string, c *Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cs[id]; !ok {
		return fmt.Errorf("credential %s not found", id)
	}
	c.ID = id
	s.cs[id] = c
	return s.saveLocked()
}

// Delete removes a credential.
func (s *CredStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cs[id]; !ok {
		return fmt.Errorf("credential %s not found", id)
	}
	delete(s.cs, id)
	return s.saveLocked()
}

func (s *CredStore) saveLocked() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(s.credsSlice(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *CredStore) credsSlice() []*Credential {
	out := make([]*Credential, 0, len(s.cs))
	for _, c := range s.cs {
		out = append(out, c)
	}
	return out
}

func atoiName(id string) int {
	var n int
	if _, err := fmt.Sscanf(id, "credp-%d", &n); err != nil {
		return 0
	}
	return n
}
