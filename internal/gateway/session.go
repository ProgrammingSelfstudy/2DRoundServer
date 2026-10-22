package gateway

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// ErrClose can be returned by a handler to end the connection cleanly.
var ErrClose = errors.New("gateway: close connection")

// Session is one client connection. Handlers get it with the decoded request.
type Session struct {
	srv    *Server
	conn   net.Conn
	Remote string

	wmu sync.Mutex // serialises writes (handlers and Hub pushes run concurrently)

	mu      sync.Mutex
	account *store.Account
	role    *store.Role
	closed  bool

	// 1197 bookkeeping, owned by the read loop
	pending *signature
}

type signature struct {
	sign      string
	idx, time int64
	code      int64
}

func (s *Session) Account() *store.Account { s.mu.Lock(); defer s.mu.Unlock(); return s.account }
func (s *Session) Role() *store.Role       { s.mu.Lock(); defer s.mu.Unlock(); return s.role }

func (s *Session) SetAccount(a *store.Account) { s.mu.Lock(); s.account = a; s.mu.Unlock() }
func (s *Session) SetRole(r *store.Role)       { s.mu.Lock(); s.role = r; s.mu.Unlock() }

// Send encodes msg with the server->client field table of cmd and writes one frame.
func (s *Session) Send(cmd uint16, msg proto.Message) error {
	fields, ok := s.srv.Schema.Recv[cmd]
	if !ok {
		return fmt.Errorf("gateway: no receive schema for command %d", cmd)
	}
	payload, err := proto.Encode(s.srv.Codec, fields, msg)
	if err != nil {
		return fmt.Errorf("gateway: encode %d: %w", cmd, err)
	}
	b, err := proto.Frame{Command: cmd, Payload: payload}.Encode()
	if err != nil {
		return err
	}
	if s.srv.Verbose {
		s.srv.Log.Printf("gw %s -> cmd %d (%d bytes)", s.Remote, cmd, len(payload))
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = s.conn.Write(b)
	return err
}

// Close closes the connection; the read loop then returns.
func (s *Session) Close() {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.mu.Unlock()
	if !already {
		s.conn.Close()
	}
}

func (s *Session) Now() time.Time { return s.srv.Now() }
