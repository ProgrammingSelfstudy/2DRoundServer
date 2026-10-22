package gateway

import (
	"context"
	"net"
	"net/http"

	"github.com/coder/websocket"
)

// WSOptions configures the WebSocket entry used by mini-game clients (WeChat,
// Douyin), which cannot open raw TCP.
type WSOptions struct {
	// OriginPatterns lists the browser Origin hosts allowed to connect (path.Match
	// patterns on the host). Requests without an Origin header, which is what native
	// and mini-game clients send, are always accepted; a browser page from another
	// origin is refused unless listed here.
	OriginPatterns []string
	// MaxMessage bounds one WebSocket message; 0 means 64 KiB.
	MaxMessage int64
}

const defaultMaxMessage = 64 << 10

type strAddr string

func (a strAddr) Network() string { return "websocket" }
func (a strAddr) String() string  { return string(a) }

// wsConn is the WebSocket seen as a byte stream, with the client's real address.
type wsConn struct {
	net.Conn
	remote net.Addr
}

func (c *wsConn) RemoteAddr() net.Addr { return c.remote }

// WSHandler serves one game connection per WebSocket. The bytes are exactly those of
// the TCP protocol (the optional handshake, then frames), carried in binary messages
// and read as a stream, so a frame may span messages and a message may hold several
// frames. Everything after that, sessions, signatures, handlers, pushes, is shared
// with the TCP gateway.
func (s *Server) WSHandler(o WSOptions) http.Handler {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()
	limit := o.MaxMessage
	if limit <= 0 {
		limit = defaultMaxMessage
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		s.wg.Add(1)
		s.mu.Unlock()
		defer s.wg.Done()

		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns:  o.OriginPatterns,
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			return // Accept has already answered the request
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		nc := websocket.NetConn(ctx, c, websocket.MessageBinary)
		// NetConn switches the message size limit off, so it is set again here: without
		// it one client could send a message as large as it likes.
		c.SetReadLimit(limit)
		s.handleConn(&wsConn{Conn: nc, remote: strAddr(r.RemoteAddr)})
	})
}
