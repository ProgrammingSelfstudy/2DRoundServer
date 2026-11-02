package game

import (
	"log"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/store"
)

// Operating data: what the gateway records for the admin page "运营数据" (store.Analytics). Recording never fails a request.

// quietCmds are not counted: they are sent by the client on a timer, whatever the player does.
var quietCmds = map[uint16]bool{1169: true, 20060: true}

// RecordCommand is the gateway's OnCommand hook: it counts a command of a role that is in the game.
func RecordCommand(st *store.Store) func(*gateway.Session, uint16) {
	return func(s *gateway.Session, cmd uint16) {
		if r := s.Role(); r != nil && !quietCmds[cmd] {
			st.RecordCmd(r.RID, cmd)
		}
	}
}

// RecordLogout is called when a session ends: a session of a role in the game is one play session, its length in seconds.
func RecordLogout(st *store.Store, s *gateway.Session) {
	r := s.Role()
	if r == nil {
		return
	}
	sec := int64(s.Now().Sub(s.Connected).Seconds())
	if sec < 0 {
		sec = 0
	}
	st.RecordEvent("logout", r.RID, r.AccountID, sec)
}

// RunAnalytics writes the counted commands and the number of roles online every interval until stop is closed, then writes the
// counts once more.
func RunAnalytics(st *store.Store, hub *gateway.Hub, logger *log.Logger, interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if err := st.SampleOnline(hub.Count()); err != nil {
				logger.Printf("analytics: online sample: %v", err)
			}
			if err := st.FlushCmds(); err != nil {
				logger.Printf("analytics: command counts: %v", err)
			}
		case <-stop:
			if err := st.FlushCmds(); err != nil {
				logger.Printf("analytics: command counts: %v", err)
			}
			return
		}
	}
}
