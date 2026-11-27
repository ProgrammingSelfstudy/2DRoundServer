// Package gateway is the game TCP gateway: transport, framing, 1197 signature
// bookkeeping and command dispatch. Game logic lives in package game and is
// registered on the Router, so the same pipeline can later sit behind a
// WebSocket transport for mini-game clients.
package gateway

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"2droundserver/internal/proto"
)

type Handler func(s *Session, req proto.Message) error

type Router struct {
	handlers    map[uint16]Handler
	enter       []Handler
	before      []func(*Session) error
	sendFilters map[uint16]func(*Session, proto.Message) proto.Message
	strict      bool
}

func NewRouter() *Router { return &Router{handlers: map[uint16]Handler{}} }

func NewStrictRouter() *Router {
	router := NewRouter()
	router.strict = true
	return router
}

func (r *Router) Handle(cmd uint16, h Handler) {
	if _, exists := r.handlers[cmd]; r.strict && exists {
		panic(fmt.Sprintf("gateway: handler already registered for command %d", cmd))
	}
	r.handlers[cmd] = h
}

// FilterSend registers a transformation for outgoing messages before the server
// starts. Returning nil suppresses the frame for that session.
func (r *Router) FilterSend(cmd uint16, filter func(*Session, proto.Message) proto.Message) {
	if _, exists := r.sendFilters[cmd]; r.strict && exists {
		panic(fmt.Sprintf("gateway: send filter already registered for command %d", cmd))
	}
	if r.sendFilters == nil {
		r.sendFilters = make(map[uint16]func(*Session, proto.Message) proto.Message)
	}
	r.sendFilters[cmd] = filter
}

// OnEnter registers a push the original makes when a role comes into the game: RunEnter calls it
// (with no request) once the main city is up.
func (r *Router) OnEnter(h Handler) { r.enter = append(r.enter, h) }

// RunEnter calls the OnEnter pushes in the order they were registered.
func (r *Router) RunEnter(s *Session) error {
	for _, h := range r.enter {
		if err := h(s, nil); err != nil {
			return err
		}
	}
	return nil
}

// Has reports whether a handler is registered for cmd.
func (r *Router) Has(cmd uint16) bool { return r.handlers[cmd] != nil }

func (r *Router) BeforeCommand(h func(*Session) error) { r.before = append(r.before, h) }

type Server struct {
	Schema *proto.Schema
	Codec  proto.Options
	Router *Router
	Hub    *Hub
	Idle   time.Duration
	// MaxConnsPerIP caps the open connections from one client address (0 = no cap).
	MaxConnsPerIP int
	// LoginTimeout is how long a connection may stay without a logged-in account
	// (0 = the same as Idle): clients that connect and say nothing are dropped early.
	LoginTimeout time.Duration
	// Verbose logs every received and sent command (id and size, never payload contents).
	Verbose bool
	Log     *log.Logger
	Now     func() time.Time
	// OnDisconnect runs when a session ends (after the read loop returns).
	OnDisconnect func(s *Session)
	// OnCommand runs after a handler answered a command without an error (operating data).
	OnCommand func(s *Session, cmd uint16)

	mu      sync.Mutex
	perIP   map[string]int
	closing bool
	ln      net.Listener
	conns   map[*Session]struct{}
	wg      sync.WaitGroup
}

func (s *Server) init() {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = log.Default()
	}
	if s.Hub == nil {
		s.Hub = NewHub()
	}
	if s.Router == nil {
		s.Router = NewRouter()
	}
	if s.Idle == 0 {
		s.Idle = 120 * time.Second
	}
	if s.conns == nil {
		s.conns = map[*Session]struct{}{}
	}
}

// Serve accepts connections until the listener is closed.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.init()
	s.ln = ln
	s.mu.Unlock()
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(c)
		}()
	}
}

// Close stops accepting, closes every session and waits for the loops to end.
func (s *Server) Close() {
	s.mu.Lock()
	s.closing = true
	if s.ln != nil {
		s.ln.Close()
	}
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// clientIP is the address part of a connection's remote address.
func clientIP(c net.Conn) string {
	addr := c.RemoteAddr().String()
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// readLimit is how long the next read may wait: a connection that has not logged in
// yet gets the shorter LoginTimeout.
func (s *Server) readLimit(sess *Session) time.Duration {
	if s.LoginTimeout > 0 && s.LoginTimeout < s.Idle && sess.Account() == nil {
		return s.LoginTimeout
	}
	return s.Idle
}

func (s *Server) handleConn(c net.Conn) {
	sess := &Session{srv: s, conn: c, Remote: c.RemoteAddr().String(), Connected: s.Now()}
	ip := clientIP(c)
	s.mu.Lock()
	if s.perIP == nil {
		s.perIP = map[string]int{}
	}
	if s.MaxConnsPerIP > 0 && s.perIP[ip] >= s.MaxConnsPerIP {
		s.mu.Unlock()
		s.Log.Printf("gw %s refused: %d connections from %s already", sess.Remote, s.MaxConnsPerIP, ip)
		c.Close()
		return
	}
	s.perIP[ip]++
	s.conns[sess] = struct{}{}
	s.mu.Unlock()
	defer func() {
		sess.Close()
		if r := sess.Role(); r != nil {
			s.Hub.Unregister(r.RID, sess)
		}
		if s.OnDisconnect != nil {
			s.OnDisconnect(sess)
		}
		s.mu.Lock()
		delete(s.conns, sess)
		if s.perIP[ip]--; s.perIP[ip] <= 0 {
			delete(s.perIP, ip)
		}
		s.mu.Unlock()
		s.Log.Printf("gw %s disconnected", sess.Remote)
	}()
	s.Log.Printf("gw %s connected", sess.Remote)

	br := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(s.readLimit(sess)))
	// The real client sends the 23-byte handshake first (confirmed on device).
	// Frames start with flags=0, the handshake with 'g', so one byte tells them
	// apart; a client that skips the handshake is tolerated.
	if first, err := br.Peek(1); err == nil && first[0] == proto.Handshake[0] {
		if err := proto.ReadHandshake(br); err != nil {
			s.Log.Printf("gw %s bad handshake: %v", sess.Remote, err)
			return
		}
	}
	for {
		c.SetReadDeadline(time.Now().Add(s.readLimit(sess)))
		fr, err := proto.ReadFrame(br)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) && !isTimeout(err) {
				s.Log.Printf("gw %s read: %v", sess.Remote, err)
			}
			return
		}
		if err := s.safeDispatch(sess, fr); err != nil {
			if !errors.Is(err, ErrClose) {
				s.Log.Printf("gw %s cmd %d: %v", sess.Remote, fr.Command, err)
			}
			return
		}
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// safeDispatch runs dispatch and turns a panic in decoding or in a handler into an error
// that closes only this connection: one bad command must not take every player offline.
// The session's state may be half-updated by the failed handler, so it is not kept.
func (s *Server) safeDispatch(sess *Session, fr proto.Frame) (err error) {
	defer func() {
		if v := recover(); v != nil {
			var rid int64
			if r := sess.Role(); r != nil {
				rid = r.RID
			}
			s.Log.Printf("PANIC gw %s rid %d cmd %d: %v\n%s", sess.Remote, rid, fr.Command, v, debug.Stack())
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return s.dispatch(sess, fr)
}

// Safe runs fn and logs a panic with its stack instead of letting it end the process. Background
// loops wrap each tick in it, so one failing tick is skipped and the loop keeps running.
func Safe(l *log.Logger, what string, fn func()) (panicked bool) {
	defer func() {
		if v := recover(); v != nil {
			if l == nil {
				l = log.Default()
			}
			l.Printf("PANIC %s: %v\n%s", what, v, debug.Stack())
			panicked = true
		}
	}()
	fn()
	return false
}

func (s *Server) dispatch(sess *Session, fr proto.Frame) error {
	if s.Verbose {
		s.Log.Printf("gw %s <- cmd %d (%d bytes)", sess.Remote, fr.Command, len(fr.Payload))
	}
	fields, known := s.Schema.Send[fr.Command]
	if !known && len(fr.Payload) != 0 {
		s.Log.Printf("gw %s cmd %d: no send schema, %d payload bytes ignored", sess.Remote, fr.Command, len(fr.Payload))
		return nil
	}
	req, err := proto.Decode(s.Codec, fields, fr.Payload)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if fr.Command == 1197 {
		sess.pending = &signature{sign: req.Str("sign"), idx: req.Int("idx"), time: req.Int("time"), code: req.Int("code")}
		return nil
	}
	s.checkSignature(sess, fr.Command)
	h := s.Router.handlers[fr.Command]
	if h == nil {
		s.Log.Printf("gw %s cmd %d: no handler (payload %d bytes)", sess.Remote, fr.Command, len(fr.Payload))
		return nil
	}
	for _, before := range s.Router.before {
		if err := before(sess); err != nil {
			return err
		}
	}
	if err := h(sess, req); err != nil {
		return err
	}
	if s.OnCommand != nil {
		s.OnCommand(sess, fr.Command)
	}
	return nil
}

// checkSignature validates the 1197 that preceded cmd. Phase 1 only logs: the
// algorithm is a client artefact, so a mismatch is a hint, never a rejection.
func (s *Server) checkSignature(sess *Session, cmd uint16) {
	p := sess.pending
	sess.pending = nil
	switch {
	case signExempt(cmd):
		return
	case p == nil:
		s.Log.Printf("gw %s cmd %d: not preceded by 1197", sess.Remote, cmd)
	case p.sign != expectedSign(p.idx, p.time, cmd) || p.code != expectedCode(cmd, p.idx):
		s.Log.Printf("gw %s cmd %d: 1197 signature mismatch", sess.Remote, cmd)
	}
}
