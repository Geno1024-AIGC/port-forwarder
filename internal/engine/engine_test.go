package engine

import (
	"io"
	"net"
	"testing"
	"time"
)

func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String()
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestEngineAddForward(t *testing.T) {
	target := startEcho(t)
	listen := freePort(t)

	e := New()
	r, err := e.Add(RuleTypeLocal, "echo", listen, target)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.Status != StatusRunning {
		t.Fatalf("status = %s, want running", r.Status)
	}

	conn, err := net.DialTimeout("tcp", listen, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload := []byte("ping")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("got %q, want %q", buf, payload)
	}
}

func TestEngineRemove(t *testing.T) {
	listen := freePort(t)
	e := New()
	r, err := e.Add(RuleTypeLocal, "x", listen, "127.0.0.1:1")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := e.Remove(r.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := net.DialTimeout("tcp", listen, time.Second); err == nil {
		t.Fatal("listener still accepting after Remove")
	}

	if err := e.Remove(r.ID); err == nil {
		t.Fatal("Remove of missing rule should fail")
	}
}

func TestEngineListOrder(t *testing.T) {
	e := New()
	_, err := e.Add(RuleTypeLocal, "a", freePort(t), "127.0.0.1:1")
	if err != nil {
		t.Fatalf("Add a: %v", err)
	}
	_, err = e.Add(RuleTypeLocal, "b", freePort(t), "127.0.0.1:1")
	if err != nil {
		t.Fatalf("Add b: %v", err)
	}
	_ = e.Remove("1")

	rules := e.List()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	if rules[0].Name != "b" {
		t.Fatalf("got rule %q, want b", rules[0].Name)
	}
}

// fakeRemote records rules delegated to it by the engine.
type fakeRemote struct {
	added   []Rule
	removed []string
}

func (f *fakeRemote) AddRemote(r Rule)       { f.added = append(f.added, r) }
func (f *fakeRemote) RemoveRemote(id string) { f.removed = append(f.removed, id) }

func TestEngineRemoteRuleDelegates(t *testing.T) {
	e := New()
	fake := &fakeRemote{}
	e.SetRemoteBackend(fake)

	r, err := e.Add(RuleTypeRemote, "web", ":8080", "127.0.0.1:3000")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.Status != StatusPending {
		t.Fatalf("status = %s, want pending", r.Status)
	}
	if len(fake.added) != 1 || fake.added[0].ID != r.ID {
		t.Fatalf("backend not called with the rule: %+v", fake.added)
	}

	e.UpdateRemoteStatus(r.ID, ":8080", nil)
	if got := e.List()[0].Status; got != StatusRunning {
		t.Fatalf("status after ack = %s, want running", got)
	}

	if err := e.Remove(r.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != r.ID {
		t.Fatalf("backend RemoveRemote not called: %+v", fake.removed)
	}
}

func TestEngineRemoteRuleWithoutBackend(t *testing.T) {
	e := New()
	r, err := e.Add(RuleTypeRemote, "web", ":8080", "127.0.0.1:3000")
	if err == nil {
		t.Fatal("expected error when no backend is configured")
	}
	if r.Status != StatusError {
		t.Fatalf("status = %s, want error", r.Status)
	}
}
