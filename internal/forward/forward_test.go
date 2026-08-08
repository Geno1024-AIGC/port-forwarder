package forward

import (
	"io"
	"net"
	"testing"
	"time"
)

// startEcho starts a TCP server that echoes everything back.
func startEcho(t *testing.T) (addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestRelayEcho(t *testing.T) {
	target := startEcho(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		client, err := listener.Accept()
		if err != nil {
			return
		}
		upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
		if err != nil {
			client.Close()
			return
		}
		Relay(client, upstream)
	}()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello relay")
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

func TestRelayClosesOnEOF(t *testing.T) {
	// A listener that never accepts (target side). Dial succeeds but the
	// peer is immediately closed by the server after a short delay.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close() // signal EOF to the relay immediately
	}()

	client, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Create a pipe peer; the read side will observe EOF once Relay returns.
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	done := make(chan struct{})
	go func() {
		Relay(client, b)
		close(done)
	}()

	select {
	case <-done:
		// Relay returned, good.
	case <-time.After(5 * time.Second):
		t.Fatal("Relay did not return after peer EOF")
	}
}
