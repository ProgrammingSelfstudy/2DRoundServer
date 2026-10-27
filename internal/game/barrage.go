package game

import (
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// The barrage (弹幕) shown over the champion matches: BarrageConst.type of the client.
const (
	barrageChampion      = 101
	barrageCrossChampion = 102
	barragePeak          = 103

	barrageKeep     = 100             // messages kept per type, shown to a role who enters
	barrageMaxRunes = 30              // longest message (ours, not in the client)
	barrageGap      = 3 * time.Second // shortest time between two messages of a role (ours)
)

type barrageMsg struct {
	ID, RID int64
	SrvID   string
	Name    string
	Msg     string
	Time    int64
}

func (m barrageMsg) message() proto.Message {
	return proto.Message{"id": m.ID, "rid": m.RID, "srv_id": m.SrvID, "name": m.Name, "msg": m.Msg, "time": m.Time}
}

type barrageSvc struct {
	mu     sync.Mutex
	nextID int64
	rooms  map[int64]map[*gateway.Session]bool // type -> sessions that have opened the barrage
	recent map[int64][]barrageMsg              // type -> the newest messages, newest last
	last   map[int64]time.Time                 // role -> time of its last message
}

// RegisterBarrage registers the barrage (12729~12733): 12730 opens the barrage of a type (the client sends it with
// is_push 1, which asks for the recent messages), 12731 closes it (12729 closes the peak champion one), 12732 sends a message.
// Every message goes to all sessions that have the same type open, as 12733 (the sender too: the client has no local echo).
// Only the three types of the client are taken; the messages are only kept in memory, 100 per type. Ours, not captured:
// a message is at most 30 characters and a role may send one every 3 seconds.
func RegisterBarrage(r *gateway.Router) {
	b := &barrageSvc{rooms: map[int64]map[*gateway.Session]bool{}, recent: map[int64][]barrageMsg{}, last: map[int64]time.Time{}}
	known := func(t int64) bool { return t == barrageChampion || t == barrageCrossChampion || t == barragePeak }
	r.Handle(12730, func(s *gateway.Session, req proto.Message) error {
		t := req.Int("type")
		if s.Role() == nil || !known(t) {
			return nil
		}
		b.mu.Lock()
		if b.rooms[t] == nil {
			b.rooms[t] = map[*gateway.Session]bool{}
		}
		b.rooms[t][s] = true
		var list []proto.Message
		for _, m := range b.recent[t] {
			list = append(list, m.message())
		}
		b.mu.Unlock()
		if req.Int("is_push") != 1 || len(list) == 0 {
			return nil
		}
		return s.Send(12733, proto.Message{"type": t, "list": list})
	})
	leave := func(s *gateway.Session, only int64) {
		b.mu.Lock()
		defer b.mu.Unlock()
		for t, room := range b.rooms {
			if only == 0 || only == t {
				delete(room, s)
			}
		}
	}
	r.Handle(12731, func(s *gateway.Session, _ proto.Message) error { leave(s, 0); return nil })
	r.Handle(12729, func(s *gateway.Session, _ proto.Message) error { leave(s, barragePeak); return nil })
	r.Handle(12732, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		t, text := req.Int("type"), req.Str("msg")
		reply := func(ok bool, msg string) error { return s.Send(12732, proto.Message{"code": b2i(ok), "msg": msg}) }
		now := s.Now()
		b.mu.Lock()
		switch {
		case !known(t):
			b.mu.Unlock()
			return reply(false, "弹幕类型不对")
		case !validChatMsg(text) || len([]rune(text)) > barrageMaxRunes:
			b.mu.Unlock()
			return reply(false, "弹幕内容不合法")
		case !b.rooms[t][s]:
			b.mu.Unlock()
			return reply(false, "还没有进入弹幕")
		case now.Sub(b.last[role.RID]) < barrageGap:
			b.mu.Unlock()
			return reply(false, "发送太快了，请稍后再试")
		}
		b.last[role.RID] = now
		b.nextID++
		m := barrageMsg{ID: b.nextID, RID: role.RID, SrvID: role.SrvID, Name: role.Name, Msg: text, Time: now.Unix()}
		b.recent[t] = append(b.recent[t], m)
		if n := len(b.recent[t]); n > barrageKeep {
			b.recent[t] = append([]barrageMsg(nil), b.recent[t][n-barrageKeep:]...)
		}
		var to []*gateway.Session
		for sess := range b.rooms[t] {
			to = append(to, sess)
		}
		b.mu.Unlock()
		push := proto.Message{"type": t, "list": []proto.Message{m.message()}}
		for _, sess := range to {
			if err := sess.Send(12733, push); err != nil { // gone: drop it from the room
				b.mu.Lock()
				delete(b.rooms[t], sess)
				b.mu.Unlock()
			}
		}
		return reply(true, "发送成功")
	})
}
