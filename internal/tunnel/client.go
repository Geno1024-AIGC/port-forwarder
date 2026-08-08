package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/forward"
)

// ClientRule is a remote forwarding rule kept by the client.
type ClientRule struct {
	ID     string
	Name   string
	Listen string // public address desired by the user
	Target string // address reachable from the client
}

// OnRule callback lets the bridge observe rule state changes.
type OnRule func(rule ClientRule, err error)

// Client maintains a control connection to a Server and opens per-connection
// data tunnels when the server says a public connection arrived.
type Client struct {
	serverAddr string

	mu     sync.Mutex
	conn   net.Conn
	enc    *json.Encoder
	rules  map[string]*ClientRule
	onRule OnRule
}

// NewClient creates a Client that will dial serverAddr.
func NewClient(serverAddr string) *Client {
	return &Client{
		serverAddr: serverAddr,
		rules:      make(map[string]*ClientRule),
	}
}

// SetOnRule registers a callback invoked when a registered rule's state
// changes (registered / unregistered / error).
func (c *Client) SetOnRule(fn OnRule) {
	c.mu.Lock()
	c.onRule = fn
	c.mu.Unlock()
}

// Add registers a remote rule. It is stored immediately and (re)registered
// whenever a connection to the server is (re)established. If a control
// connection is already up, the rule is sent at once.
func (c *Client) Add(rule ClientRule) {
	c.mu.Lock()
	c.rules[rule.ID] = &rule
	enc := c.enc
	c.mu.Unlock()
	if enc != nil {
		_ = enc.Encode(Frame{Type: FrameRegister, ID: rule.ID, Name: rule.Name, Listen: rule.Listen, Target: rule.Target})
	}
}

// Remove deregisters a remote rule.
func (c *Client) Remove(id string) {
	c.mu.Lock()
	delete(c.rules, id)
	enc := c.enc
	c.mu.Unlock()
	if enc != nil {
		_ = enc.Encode(Frame{Type: FrameUnregister, ID: id})
	}
}

func (c *Client) snapshot() []*ClientRule {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*ClientRule, 0, len(c.rules))
	for _, r := range c.rules {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

func (c *Client) reportRule(r *ClientRule, err error) {
	c.mu.Lock()
	fn := c.onRule
	c.mu.Unlock()
	if fn != nil {
		fn(*r, err)
	}
}

// Run connects to the server and serves data connections until ctx is
// cancelled or the connection is unrecoverable. It reconnects with backoff.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.connect(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			delay := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				delay.Stop()
				return
			case <-delay.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second

		for _, r := range c.snapshot() {
			c.send(Frame{Type: FrameRegister, ID: r.ID, Name: r.Name, Listen: r.Listen, Target: r.Target})
		}
		c.serve(ctx)
	}
}

// connect dials the server control port and resets framing state.
func (c *Client) connect(ctx context.Context) error {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.serverAddr)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.enc = json.NewEncoder(conn)
	c.mu.Unlock()
	return nil
}

// serve handles the control loop while connected.
func (c *Client) serve(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
			c.enc = nil
		}
		c.mu.Unlock()
	}()

	dec := json.NewDecoder(bufio.NewReader(c.conn))
	for {
		var f Frame
		if err := dec.Decode(&f); err != nil {
			return
		}
		switch f.Type {
		case FrameOpen:
			go c.handleOpen(f)
		case FrameError:
			c.reportError(f)
		}
	}
}

// reportError surfaces a server error for a rule.
func (c *Client) reportError(f Frame) {
	c.mu.Lock()
	r, ok := c.rules[f.ID]
	c.mu.Unlock()
	if ok {
		c.reportRule(r, errors.New(f.Message))
	}
}

// handleOpen dials the data address back to the server and connects the
// public connection to the client-side target.
func (c *Client) handleOpen(f Frame) {
	data, err := net.DialTimeout("tcp", f.Listen, 10*time.Second)
	if err != nil {
		return
	}
	defer data.Close()
	if _, err := data.Write([]byte(dataHeader(f.ConnID))); err != nil {
		return
	}

	c.mu.Lock()
	rule, ok := c.rules[f.ID]
	c.mu.Unlock()
	if !ok {
		return
	}

	target, err := net.DialTimeout("tcp", rule.Target, 10*time.Second)
	if err != nil {
		return
	}
	defer target.Close()

	forward.Relay(data, target)
}

func (c *Client) send(f Frame) {
	c.mu.Lock()
	enc := c.enc
	c.mu.Unlock()
	if enc != nil {
		_ = enc.Encode(f)
	}
}
