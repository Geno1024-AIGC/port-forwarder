package tunnel

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/Geno1024-AIGC/port-forwarder/internal/forward"
)

// RuleStatus is a callback the server exposes about a client's rule.
type RuleStatus struct {
	ID     string
	Listen string
	Target string
	Active bool
}

// ClientState describes the state of one connected client.
type ClientState struct {
	RemoteAddr string
	Rules      []RuleStatus
}

// Server is the public endpoint that accepts tunnel clients and exposes
// their forwarding rules on public listeners.
type Server struct {
	ctrl net.Listener

	mu      sync.Mutex
	clients map[*clientSession]struct{}

	onRuleChange func(ruleID string)
}

type clientSession struct {
	srv  *Server
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	mu      sync.Mutex
	rules   map[string]*ruleState
	pending map[uint64]net.Conn
	nextID  uint64
	closed  bool
}

type ruleState struct {
	rule   Frame
	listen net.Listener
	wg     sync.WaitGroup
}

// NewServer creates a Server that listens on ctrlAddr.
func NewServer(ctrlAddr string) (*Server, error) {
	ln, err := net.Listen("tcp", ctrlAddr)
	if err != nil {
		return nil, err
	}
	return &Server{
		ctrl:    ln,
		clients: make(map[*clientSession]struct{}),
	}, nil
}

// Addr returns the control listener address.
func (s *Server) Addr() string {
	return s.ctrl.Addr().String()
}

// SetOnRuleChange registers a callback invoked whenever a rule appears or
// disappears across all clients.
func (s *Server) SetOnRuleChange(fn func(ruleID string)) {
	s.mu.Lock()
	s.onRuleChange = fn
	s.mu.Unlock()
}

func (s *Server) ruleChanged(ruleID string) {
	s.mu.Lock()
	fn := s.onRuleChange
	s.mu.Unlock()
	if fn != nil {
		fn(ruleID)
	}
}

// Serve accepts client connections until the listener is closed.
func (s *Server) Serve() error {
	for {
		conn, err := s.ctrl.Accept()
		if err != nil {
			return err
		}
		se := &clientSession{
			srv:     s,
			conn:    conn,
			enc:     json.NewEncoder(conn),
			dec:     json.NewDecoder(bufio.NewReader(conn)),
			rules:   make(map[string]*ruleState),
			pending: make(map[uint64]net.Conn),
		}
		s.mu.Lock()
		s.clients[se] = struct{}{}
		s.mu.Unlock()
		go se.serve()
	}
}

// Close closes the control listener and all client sessions.
func (s *Server) Close() {
	_ = s.ctrl.Close()
	s.mu.Lock()
	clients := make([]*clientSession, 0, len(s.clients))
	for se := range s.clients {
		clients = append(clients, se)
	}
	s.mu.Unlock()
	for _, se := range clients {
		se.close()
	}
}

// Clients returns a snapshot of connected clients and their rules.
func (s *Server) Clients() []ClientState {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ClientState, 0, len(s.clients))
	for se := range s.clients {
		st := ClientState{RemoteAddr: se.conn.RemoteAddr().String()}
		se.mu.Lock()
		for _, rs := range se.rules {
			st.Rules = append(st.Rules, RuleStatus{
				ID:     rs.rule.ID,
				Listen: rs.listen.Addr().String(),
				Target: rs.rule.Target,
				Active: true,
			})
		}
		se.mu.Unlock()
		out = append(out, st)
	}
	return out
}

// serve runs the control loop for one client session.
func (se *clientSession) serve() {
	defer se.close()

	// read deadline detects dead clients
	_ = se.conn.SetReadDeadline(time.Now().Add(120 * time.Second))

	for {
		var f Frame
		if err := se.dec.Decode(&f); err != nil {
			return
		}
		_ = se.conn.SetReadDeadline(time.Now().Add(120 * time.Second))

		switch f.Type {
		case FrameRegister:
			se.register(f)
		case FrameUnregister:
			se.unregister(f.ID)
		case FramePing:
			_ = se.enc.Encode(Frame{Type: FramePong})
		}
	}
}

func (se *clientSession) register(f Frame) {
	rs := &ruleState{rule: f}
	ln, err := net.Listen("tcp", f.Listen)
	if err != nil {
		_ = se.enc.Encode(Frame{
			Type:    FrameError,
			ID:      f.ID,
			Message: "public listen failed: " + err.Error(),
		})
		return
	}
	rs.listen = ln

	se.mu.Lock()
	if old, ok := se.rules[f.ID]; ok {
		old.listen.Close()
	}
	se.rules[f.ID] = rs
	se.mu.Unlock()

	_ = se.enc.Encode(Frame{Type: FrameRegistered, ID: f.ID, Listen: ln.Addr().String()})
	se.srv.ruleChanged(f.ID)

	rs.wg.Add(1)
	go se.acceptPublic(rs)
}

func (se *clientSession) unregister(id string) {
	se.mu.Lock()
	rs, ok := se.rules[id]
	delete(se.rules, id)
	se.mu.Unlock()
	if !ok {
		return
	}
	_ = rs.listen.Close()
	rs.wg.Wait()
	se.srv.ruleChanged(id)
}

// acceptPublic relays one incoming public connection to a fresh data
// connection dialed back by the client.
func (se *clientSession) acceptPublic(rs *ruleState) {
	defer rs.wg.Done()
	for {
		pub, err := rs.listen.Accept()
		if err != nil {
			return
		}
		go se.openData(rs.rule.ID, pub)
	}
}

func (se *clientSession) openData(ruleID string, pub net.Conn) {
	defer pub.Close()

	dataLn, err := net.Listen("tcp", ":0")
	if err != nil {
		return
	}
	defer dataLn.Close()
	_ = dataLn.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Second))

	connID := se.nextConnID()
	addr := dataAddr(se.conn.LocalAddr(), dataLn.Addr())

	se.mu.Lock()
	se.pending[connID] = pub
	se.mu.Unlock()

	if err := se.enc.Encode(Frame{Type: FrameOpen, ID: ruleID, ConnID: connID, Listen: addr}); err != nil {
		return
	}

	data, err := dataLn.Accept()
	if err != nil {
		return
	}
	defer data.Close()

	// verify the data connection's handshake matches this connID
	br := bufio.NewReader(data)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	id, ok := parseDataHeader(line)
	if !ok || id != connID {
		return
	}

	se.mu.Lock()
	delete(se.pending, connID)
	se.mu.Unlock()

	forward.Relay(pub, &bridgeConn{Conn: data, r: br})
}

// bridgeConn re-exposes a connection whose reader may already carry buffered
// bytes (the consumed handshake line).
type bridgeConn struct {
	net.Conn
	r io.Reader
}

func (b *bridgeConn) Read(p []byte) (int, error) { return b.r.Read(p) }

func (se *clientSession) nextConnID() uint64 {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.nextID++
	return se.nextID
}

func (se *clientSession) close() {
	se.mu.Lock()
	if se.closed {
		se.mu.Unlock()
		return
	}
	se.closed = true
	_ = se.conn.Close()
	for _, rs := range se.rules {
		_ = rs.listen.Close()
	}
	se.mu.Unlock()

	se.srv.mu.Lock()
	delete(se.srv.clients, se)
	se.srv.mu.Unlock()
}

// dataAddr combines the host of the control connection (how the client
// reaches the server) with the port of the fresh data listener.
func dataAddr(ctrlLocal, dataLocal net.Addr) string {
	host, _, err := net.SplitHostPort(ctrlLocal.String())
	if err != nil {
		return dataLocal.String()
	}
	_, port, err := net.SplitHostPort(dataLocal.String())
	if err != nil {
		return dataLocal.String()
	}
	return net.JoinHostPort(host, port)
}
