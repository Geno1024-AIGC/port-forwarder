package tunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// startEcho runs a TCP echo server and returns its address. This stands in
// for "192.168.1.2:7777" reachable from the client side.
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

// freePort returns an unused 127.0.0.1 port by binding then releasing.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestTunnelEndToEnd(t *testing.T) {
	target := startEcho(t)

	srv, err := NewServer(freePort(t))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	ctrlAddr := srv.Addr()

	client := NewClient(ctrlAddr)
	client.Add(ClientRule{ID: "echo", Name: "echo", Listen: freePort(t), Target: target})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	// The client needs a moment to dial and register.
	time.Sleep(300 * time.Millisecond)

	// Discover the public address actually bound on the server.
	var public string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		states := srv.Clients()
		if len(states) == 1 && len(states[0].Rules) == 1 {
			public = states[0].Rules[0].Listen
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if public == "" {
		t.Fatal("remote rule never registered on server")
	}

	// A "public internet" client connects to the address the server bound.
	conn, err := net.DialTimeout("tcp", public, 5*time.Second)
	if err != nil {
		t.Fatalf("public dial: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello from public internet")
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

func TestRemoteUnregister(t *testing.T) {
	target := startEcho(t)
	srv, err := NewServer(freePort(t))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	go srv.Serve()
	defer srv.Close()

	client := NewClient(srv.Addr())
	listen := freePort(t)
	client.Add(ClientRule{ID: "r", Name: "r", Listen: listen, Target: target})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	time.Sleep(300 * time.Millisecond)

	if _, err := net.DialTimeout("tcp", listen, time.Second); err != nil {
		t.Fatalf("public listener not reachable: %v", err)
	}

	client.Remove("r")
	time.Sleep(300 * time.Millisecond)

	if _, err := net.DialTimeout("tcp", listen, 300*time.Millisecond); err == nil {
		t.Fatal("public listener still accepting after unregister")
	}
}
