package game

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

// Exercise the normal gateway and wire protocol even in builds that forbid binds.
func newDungeonEnv(t *testing.T) *env {
	t.Helper()
	e := setupEnv(t, 100*time.Hour)
	ln := &dungeonPipeListener{incoming: make(chan net.Conn), closed: make(chan struct{})}
	e.connect = ln.connect
	done := make(chan error, 1)
	go func() { done <- e.gw.Serve(ln) }()
	t.Cleanup(func() {
		ln.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
		e.gw.Close()
	})
	return e
}

type dungeonPipeListener struct {
	incoming chan net.Conn
	closed   chan struct{}
	once     sync.Once
}

func (l *dungeonPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *dungeonPipeListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "dungeon-test", Net: "pipe"}
}

func (l *dungeonPipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *dungeonPipeListener) connect() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.incoming <- server:
	case <-l.closed:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	}
	c := &dungeonPipeClient{Conn: client, writes: make(chan []byte, 16), closed: make(chan struct{})}
	go c.flush()
	return c, nil
}

// Buffer client writes so a request can be sent while earlier pushes are being read.
type dungeonPipeClient struct {
	net.Conn
	writes chan []byte
	closed chan struct{}
	once   sync.Once
}

func (c *dungeonPipeClient) Write(b []byte) (int, error) {
	select {
	case c.writes <- bytes.Clone(b):
		return len(b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *dungeonPipeClient) flush() {
	defer c.Close()
	for {
		select {
		case b := <-c.writes:
			if _, err := c.Conn.Write(b); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *dungeonPipeClient) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}
