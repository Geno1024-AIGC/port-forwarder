package engine

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/forward"
)

// Status describes the runtime state of a forwarding rule.
type Status string

const (
	StatusStopped Status = "stopped"
	StatusRunning Status = "running"
	StatusError   Status = "error"
)

// Rule is a single forwarding rule: it listens on Listen and forwards
// every accepted connection to Target.
type Rule struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Listen string `json:"listen"`
	Target string `json:"target"`
	Status Status `json:"status"`
}

// Engine owns a set of rules and their listeners.
type Engine struct {
	mu      sync.Mutex
	rules   map[string]*Rule
	lns     map[string]net.Listener
	stop    map[string]chan struct{}
	nextID  int
	onError func(id string, err error)
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

// SetErrorHandler registers a callback invoked when a listener errors out.
func (e *Engine) SetErrorHandler(fn func(id string, err error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onError = fn
}

// Add registers a new rule and starts it. It returns the created rule.
func (e *Engine) Add(name, listen, target string) (*Rule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	r := &Rule{
		ID:     fmt.Sprintf("%d", e.nextID),
		Name:   name,
		Listen: listen,
		Target: target,
		Status: StatusRunning,
	}
	e.nextID++

	if err := e.startLocked(r); err != nil {
		r.Status = StatusError
		return r, err
	}
	e.rules[r.ID] = r
	return r, nil
}

// Remove stops and deletes a rule by ID.
func (e *Engine) Remove(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	r, ok := e.rules[id]
	if !ok {
		return errors.New("rule not found")
	}
	e.stopLocked(r)
	delete(e.rules, id)
	return nil
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

// Restart stops and restarts every running rule, preserving state.
func (e *Engine) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range e.rules {
		e.stopLocked(r)
		if err := e.startLocked(r); err != nil {
			r.Status = StatusError
			e.reportError(r.ID, err)
			continue
		}
		r.Status = StatusRunning
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
