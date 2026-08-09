package sshx

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/engine"
	"golang.org/x/crypto/ssh"
)

// startEcho returns a TCP listener that echoes any received bytes.
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
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

func TestReverseForward(t *testing.T) {
	srv := newSSHServer(t)
	defer srv.ln.Close()

	echoAddr := startEcho(t)

	c := NewClient(srv.Addr(), "root", []ssh.AuthMethod{ssh.Password("x")})
	statuses := make(chan error, 8)
	c.SetOnRule(func(id, listen string, err error) {
		select {
		case statuses <- err:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// Add a rule listening on an ephemeral remote port.
	c.Add(ClientRule{ID: "r1", Name: "echo", Listen: ":0", Target: echoAddr})

	// The test sshd signals the reverse forward is ready and reports its port.
	var reversePort uint32
	select {
	case reversePort = <-srv.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("reverse forward never became ready")
	}
	if reversePort == 0 {
		t.Fatal("expected a bound reverse port")
	}

	// The client should report running for r1.
	select {
	case err := <-statuses:
		if err != nil {
			t.Fatalf("rule status error: %v", err)
		}
	case <-time.After(2 * time.Second):
	}

	// Connect to the reverse-forward port on the sshd side; bytes must echo
	// all the way to the target through the SSH tunnel.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", u32(reversePort)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial reverse port: %v", err)
	}
	defer conn.Close()
	payload := "hello-through-ssh"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo mismatch: got %q want %q", buf, payload)
	}

	// Removing the rule must drop the local entry so a later Add re-applies.
	c.Remove("r1")
	c.mu.Lock()
	_, pending := c.pending["r1"]
	_, open := c.lns["r1"]
	c.mu.Unlock()
	if pending || open {
		t.Fatalf("Remove left pending=%v open=%v", pending, open)
	}
}

func TestAuthHelpers(t *testing.T) {
	if _, err := KeyAuth("", ""); err == nil {
		t.Fatal("KeyAuth with empty path should error")
	}
	if PasswordAuth("p") == nil {
		t.Fatal("PasswordAuth returned nil method")
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	if _, err := AgentAuth(); err == nil {
		t.Fatal("AgentAuth without SSH_AUTH_SOCK should error")
	}
}

// u32 renders a port number from a uint32.
func u32(v uint32) string {
	return fmt.Sprintf("%d", v)
}

// engineAdapter is the same bridge main.go wires for the `pf ssh` command.
type engineAdapter struct {
	client   *Client
	onStatus func(id, listen string, err error)
}

func (b *engineAdapter) AddRemote(rule engine.Rule) {
	b.client.Add(ClientRule{ID: rule.ID, Name: rule.Name, Listen: rule.Listen, Target: rule.Target})
	if b.onStatus != nil {
		b.onStatus(rule.ID, "", nil)
	}
}

func (b *engineAdapter) RemoveRemote(id string) { b.client.Remove(id) }

func TestSSHBackendThroughEngine(t *testing.T) {
	srv := newSSHServer(t)
	defer srv.ln.Close()
	echoAddr := startEcho(t)

	eng := engine.New()
	sc := NewClient(srv.Addr(), "root", []ssh.AuthMethod{ssh.Password("x")})
	adapter := &engineAdapter{client: sc}
	adapter.onStatus = func(id, listen string, err error) {
		eng.UpdateRemoteStatus(id, listen, err)
	}
	sc.SetOnRule(func(id, listen string, err error) {
		adapter.onStatus(id, listen, err)
	})
	eng.SetRemoteBackend(adapter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sc.Run(ctx)

	rule, err := eng.Add(engine.RuleTypeRemote, "echo", ":0", echoAddr, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	var reversePort uint32
	select {
	case reversePort = <-srv.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("reverse forward never became ready")
	}

	// Engine should have flipped the rule to running and filled the port.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rs := eng.List()
		if len(rs) == 1 && rs[0].Status == engine.StatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rs := eng.List()
	if len(rs) != 1 || rs[0].Status != engine.StatusRunning {
		t.Fatalf("rule status = %v", rs)
	}
	_ = rule

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", u32(reversePort)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial reverse: %v", err)
	}
	defer conn.Close()
	const payload = "engine-echo"
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo mismatch %q", buf)
	}

	if err := eng.Remove(rule.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestManagerCredentialFlow(t *testing.T) {
	srv := newSSHServer(t)
	defer srv.ln.Close()
	echoAddr := startEcho(t)

	path := t.TempDir() + "/creds.json"
	store, err := NewCredStore(path)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	cred, err := store.Add(&Credential{
		Name:     "vps",
		Host:     srv.Addr(),
		User:     "root",
		AuthType: "password",
		Password: "x",
	})
	if err != nil {
		t.Fatalf("add cred: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := NewManager(ctx, store)

	if err := mgr.Probe(cred.ID); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if err := mgr.Probe("nope"); err == nil {
		t.Fatal("probe of missing credential should fail")
	}

	// Reload from disk to prove persistence.
	reloaded, err := NewCredStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reloaded.Get(cred.ID)
	if !ok || got.Password != "x" {
		t.Fatalf("reloaded credential mismatch: %+v ok=%v", got, ok)
	}

	mgr.AddRemote(engine.Rule{ID: "r1", Name: "echo", Listen: ":0", Target: echoAddr, Credential: cred.ID})

	var reversePort uint32
	select {
	case reversePort = <-srv.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("reverse forward never became ready")
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", u32(reversePort)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial reverse: %v", err)
	}
	defer conn.Close()
	const payload = "manager-echo"
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo mismatch %q", buf)
	}

	if err := mgr.RemoveCredential(cred.ID); err != nil {
		t.Fatalf("remove credential: %v", err)
	}
	afterDelete, err := NewCredStore(path)
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if _, ok := afterDelete.Get(cred.ID); ok {
		t.Fatal("credential still present in store after RemoveCredential")
	}
}
