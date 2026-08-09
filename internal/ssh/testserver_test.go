package sshx

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshServer implements the reverse-forward side of a real sshd: it accepts a
// client, binds the requested tcpip-forward port locally on its "cloud" side,
// and bridges inbound connections back to the client over "forwarded-tcpip"
// channels. Nothing project-specific runs inside it.
type sshServer struct {
	t   *testing.T
	ln  net.Listener
	cfg *ssh.ServerConfig

	mu    sync.Mutex
	binds map[uint32]net.Listener // reverse-forward port -> local listener
	conn  *ssh.ServerConn
	ready chan uint32
}

// newSSHServer starts an in-process sshd on an ephemeral port.
func newSSHServer(t *testing.T) *sshServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("gen host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ssh listen: %v", err)
	}
	s := &sshServer{
		t:     t,
		ln:    ln,
		cfg:   cfg,
		binds: make(map[uint32]net.Listener),
		ready: make(chan uint32, 16),
	}
	go s.serve()
	return s
}

// Addr returns the sshd's listen address (i.e. the "cloud host").
func (s *sshServer) Addr() string { return s.ln.Addr().String() }

func (s *sshServer) serve() {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return
		}
		conn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
		if err != nil {
			s.t.Errorf("ssh handshake: %v", err)
			continue
		}
		s.mu.Lock()
		s.conn = conn
		s.mu.Unlock()
		go s.handleRequests(reqs)
		go s.handleChannels(chans)
	}
}

func (s *sshServer) handleRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "tcpip-forward" {
			req.Reply(false, nil)
			continue
		}
		var p struct {
			Addr string
			Port uint32
		}
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			req.Reply(false, nil)
			continue
		}
		port := p.Port
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			s.t.Errorf("reverse bind :%d: %v", port, err)
			req.Reply(false, nil)
			continue
		}
		if port == 0 {
			// Auto-assigned port must be reported back to the client.
			port = uint32(ncPort(ln))
		}
		s.mu.Lock()
		s.binds[port] = ln
		s.mu.Unlock()
		req.Reply(true, ssh.Marshal(struct{ Port uint32 }{Port: port}))
		select {
		case s.ready <- port:
		default:
		}
		go s.bridge(ln, p.Addr, port)
	}
}

// handleChannels accepts direct-tcpip channels, used to observe connects.
func (s *sshServer) handleChannels(chans <-chan ssh.NewChannel) {
	for nch := range chans {
		go func(nch ssh.NewChannel) {
			ch, reqs, err := nch.Accept()
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			// Just sit; not used by these tests.
			_ = ch.Close()
		}(nch)
	}
}

// ncPort returns the assigned port number of a bound TCP listener.
func ncPort(ln net.Listener) int {
	return ln.Addr().(*net.TCPAddr).Port
}

// bridge accepts TCP conns arriving at the reverse-forward port and delivers
// them to the client via a forwarded-tcpip channel.
func (s *sshServer) bridge(ln net.Listener, bindAddr string, port uint32) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.forward(c, bindAddr, port)
	}
}

func (s *sshServer) forward(c net.Conn, bindAddr string, port uint32) {
	defer c.Close()
	// The client registers its forward under net.JoinHostPort(host, port);
	// echo back the exact requested address so the lookup matches.
	payload := ssh.Marshal(struct {
		Addr       string
		Port       uint32
		OriginAddr string
		OriginPort uint32
	}{
		Addr:       bindAddr,
		Port:       port,
		OriginAddr: "127.0.0.1",
		OriginPort: 2222,
	})
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	ch, reqs, err := conn.OpenChannel("forwarded-tcpip", payload)
	if err != nil {
		s.t.Errorf("open forwarded-tcpip: %v", err)
		return
	}
	go ssh.DiscardRequests(reqs)
	pump(chanConn{Channel: ch, raddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}}, c)
}

func pump(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(a, b)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(b, a)
		done <- struct{}{}
	}()
	<-done
	<-done
}

// chanConn adapts an ssh.Channel to net.Conn for the test relay.
type chanConn struct {
	ssh.Channel
	laddr net.Addr
	raddr net.Addr
}

func (c chanConn) LocalAddr() net.Addr                { return c.laddr }
func (c chanConn) RemoteAddr() net.Addr               { return c.raddr }
func (c chanConn) SetDeadline(t time.Time) error      { return nil }
func (c chanConn) SetReadDeadline(t time.Time) error  { return nil }
func (c chanConn) SetWriteDeadline(t time.Time) error { return nil }
