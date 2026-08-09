package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store persists rules to a JSON file so they survive daemon restarts.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore opens a rule store at path; the file need not exist yet.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// List loads every rule on disk.
func (s *Store) List() ([]*Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	var rules []*Rule
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return rules, nil
}

// Save writes the given rules, dropping runtime status.
func (s *Store) Save(rules []*Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(rules)
}

func (s *Store) saveLocked(rules []*Rule) error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	type persisted struct {
		ID         string   `json:"id"`
		Type       RuleType `json:"type"`
		Name       string   `json:"name"`
		Listen     string   `json:"listen"`
		Target     string   `json:"target"`
		Credential string   `json:"credential,omitempty"`
	}
	out := make([]persisted, 0, len(rules))
	for _, r := range rules {
		out = append(out, persisted{
			ID:         r.ID,
			Type:       r.Type,
			Name:       r.Name,
			Listen:     r.Listen,
			Target:     r.Target,
			Credential: r.Credential,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}