package game

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"2droundserver/internal/gateway"
)

func newDungeonEnv(t *testing.T) *env {
	t.Helper()
	env := newGameEnv(t, 100*time.Hour)
	env.connect = serveTestPipe(t, env.gw, false)
	return env
}

func serveTestPipe(t *testing.T, gw *gateway.Server, bufferServer bool) func() (net.Conn, error) {
	t.Helper()
	listener := &testPipeListener{incoming: make(chan net.Conn), closed: make(chan struct{}), bufferServer: bufferServer}
	done := make(chan error, 1)
	go func() { done <- gw.Serve(listener) }()
	t.Cleanup(func() {
		listener.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
		gw.Close()
	})
	return listener.connect
}

type testPipeListener struct {
	incoming     chan net.Conn
	closed       chan struct{}
	once         sync.Once
	bufferServer bool
}

func (l *testPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *testPipeListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "gateway-test", Net: "pipe"}
}

func (l *testPipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *testPipeListener) connect() (net.Conn, error) {
	client, server := net.Pipe()
	if l.bufferServer {
		server = bufferTestPipe(server)
	} else {
		client = bufferTestPipe(client)
	}
	select {
	case l.incoming <- server:
		return client, nil
	case <-l.closed:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	}
}

type bufferedTestPipe struct {
	net.Conn
	writes chan []byte
	closed chan struct{}
	once   sync.Once
}

func bufferTestPipe(conn net.Conn) net.Conn {
	pipe := &bufferedTestPipe{Conn: conn, writes: make(chan []byte, 128), closed: make(chan struct{})}
	go pipe.flush()
	return pipe
}

func (pipe *bufferedTestPipe) Write(data []byte) (int, error) {
	select {
	case <-pipe.closed:
		return 0, net.ErrClosed
	default:
	}
	select {
	case pipe.writes <- bytes.Clone(data):
		return len(data), nil
	case <-pipe.closed:
		return 0, net.ErrClosed
	}
}

func (pipe *bufferedTestPipe) flush() {
	defer pipe.Close()
	for {
		select {
		case data := <-pipe.writes:
			if _, err := pipe.Conn.Write(data); err != nil {
				return
			}
		case <-pipe.closed:
			return
		}
	}
}

func (pipe *bufferedTestPipe) Close() error {
	pipe.once.Do(func() { close(pipe.closed) })
	return pipe.Conn.Close()
}
