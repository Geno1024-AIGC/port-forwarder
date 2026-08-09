package sshx

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/forward"
	"golang.org/x/crypto/ssh"
)

// ClientRule is a reverse forwarding rule handed to the SSH client backend.
// Listen is the address to open on the remote sshd (e.g. ":7788");
// Target is the local network address to reach through the tunnel.
type ClientRule struct {
	ID     string
	Name   string
	Listen string
	Target string
}

// OnRule reports a rule's remote listener status: nil err means the remote
// port is open. listen carries the address actually bound on the server.
type OnRule func(id, listen string, err error)

// Client opens SSH reverse-forward tunnels (the pf equivalent of `ssh -R`)
// against a plain sshd. Nothing needs to be installed on the remote host.
type Client struct {
	addr string
	user string
	auth []ssh.AuthMethod

	mu      sync.Mutex
	pending map[string]ClientRule
	lns     map[string]*lnEntry
	onRule  OnRule
	conn    *ssh.Client
}

// lnEntry pairs an open remote reverse listener with its stop signal.
type lnEntry struct {
	ln   net.Listener
	stop chan struct{}
}

// NewClient configures a reverse-forwarding SSH client. auth may contain
// private-key and/or password credentials.
func NewClient(addr, user string, auth []ssh.AuthMethod) *Client {
	return &Client{
		addr:    addr,
		user:    user,
		auth:    auth,
		pending: make(map[string]ClientRule),
		lns:     make(map[string]*lnEntry),
	}
}

// SetOnRule registers the callback used to surface per-rule status.
func (c *Client) SetOnRule(fn OnRule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRule = fn
}

// Add registers a reverse forwarding rule. If a connection is already up it
// takes effect immediately; otherwise it is applied on the next reconnect.
func (c *Client) Add(r ClientRule) {
	c.mu.Lock()
	c.pending[r.ID] = r
	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		c.subscribe(r, conn)
	} else {
		c.report(r.ID, r.Listen, nil)
	}
}

// Remove stops the reverse listener for rule id and forgets the rule.
func (c *Client) Remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ent, ok := c.lns[id]; ok {
		_ = ent.ln.Close()
		close(ent.stop)
		delete(c.lns, id)
	}
	delete(c.pending, id)
}

// Run keeps the SSH connection alive, reconnecting with a short backoff and
// re-establishing every rule, until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		conn, err := c.dial(ctx, backoff)
		if err != nil {
			c.failAll(err)
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = time.Second

		c.mu.Lock()
		c.conn = conn
		// listeners still referenced from a previous session are stale.
		for id, ent := range c.lns {
			_ = ent.ln.Close()
			delete(c.lns, id)
		}
		c.mu.Unlock()

		c.subscribeAll(conn)

		// Wait for the connection to drop or the context to end.
		dead := make(chan struct{})
		go func() {
			_ = conn.Wait()
			close(dead)
		}()
		select {
		case <-ctx.Done():
			_ = conn.Close()
			c.clearConn()
			return
		case <-dead:
			//u connection died; drain local listeners, then reconnect.
			c.drainListeners()
			c.clearConn()
		}
	}
}

func (c *Client) clearConn() {
	c.mu.Lock()
	c.conn = nil
	c.mu.Unlock()
}

// dial blocks until the ssh transport is established or ctx is done. On
// failure it waits backoff before returning, so failures pace themselves.
func (c *Client) dial(ctx context.Context, backoff time.Duration) (*ssh.Client, error) {
	if backoff > 0 {
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	cfg := &ssh.ClientConfig{
		User:            c.user,
		Auth:            c.auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("tcp", c.addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", c.addr, err)
	}
	return conn, nil
}

// subscribeAll opens reverse listeners for every pending rule.
func (c *Client) subscribeAll(conn *ssh.Client) {
	c.mu.Lock()
	pending := make([]ClientRule, 0, len(c.pending))
	for _, r := range c.pending {
		pending = append(pending, r)
	}
	c.mu.Unlock()
	for _, r := range pending {
		c.subscribe(r, conn)
	}
}

// subscribe opens one reverse forward on the live connection.
func (c *Client) subscribe(r ClientRule, conn *ssh.Client) {
	c.mu.Lock()
	if _, exists := c.lns[r.ID]; exists {
		c.mu.Unlock()
		return
	}
	ln, err := conn.Listen("tcp", r.Listen)
	if err != nil {
		c.mu.Unlock()
		c.report(r.ID, "", fmt.Errorf("remote listen %s: %w", r.Listen, err))
		return
	}
	ent := &lnEntry{ln: ln, stop: make(chan struct{})}
	c.lns[r.ID] = ent
	c.mu.Unlock()

	c.report(r.ID, ln.Addr().String(), nil)
	go c.acceptLoop(r, ent)
}

// acceptLoop relays remote connections to the rule target.
func (c *Client) acceptLoop(r ClientRule, ent *lnEntry) {
	for {
		conn, err := ent.ln.Accept()
		if err != nil {
			select {
			case <-ent.stop:
				return
			default:
			}
			if err != io.EOF && !c.dead() {
				// transient accept error: keep trying.
				continue
			}
			return
		}
		go c.relay(r, conn)
	}
}

// relay bridges an inbound reverse-forward connection to the target.
func (c *Client) relay(r ClientRule, in net.Conn) {
	defer in.Close()
	out, err := net.DialTimeout("tcp", r.Target, 10*time.Second)
	if err != nil {
		c.report(r.ID, "", fmt.Errorf("dial %s: %w", r.Target, err))
		return
	}
	defer out.Close()
	forward.Relay(in, out)
}

// dead reports whether the ssh.Conn is expected to have gone away.
func (c *Client) dead() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn == nil
}

// drainListeners removes listeners that died with their transport.
func (c *Client) drainListeners() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ent := range c.lns {
		_ = ent.ln.Close()
		delete(c.lns, id)
	}
}

// failAll marks every pending rule as failed.
func (c *Client) failAll(err error) {
	c.mu.Lock()
	rules := make([]ClientRule, 0, len(c.pending))
	for _, r := range c.pending {
		rules = append(rules, r)
	}
	c.mu.Unlock()
	for _, r := range rules {
		c.report(r.ID, r.Listen, err)
	}
}

// report invokes the registered status callback, if any.
func (c *Client) report(id, listen string, err error) {
	c.mu.Lock()
	fn := c.onRule
	c.mu.Unlock()
	if fn != nil {
		fn(id, listen, err)
	}
}

// nextBackoff grows the reconnect delay up to a cap.
func nextBackoff(d time.Duration) time.Duration {
	const max = 30 * time.Second
	if d >= max {
		return max
	}
	if d *= 2; d > max {
		return max
	}
	return d
}
