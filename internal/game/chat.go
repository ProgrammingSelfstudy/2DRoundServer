package game

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Chat channels are bits (chat_const.lua Channel); the world and the guild channel exist.
const (
	chatWorld = 1
	chatGuild = 4 // Channel.Gang
)

// Policy values that are our choice, not protocol.
const (
	maxChatRunes    = 100 // longest message
	chatHistory     = 50  // world messages kept in memory for 12766
	chatOfflineKeep = 50  // private messages kept per offline recipient
)

// ChatService implements private chat (12720 -> 12721, offline 12722), the
// world channel (12762 -> 12761, history 12766) and the guild channel (the same
// commands on channel 4, only for the members of the speaker's guild). Other channels (scene,
// team, cross-server), voice (12725/12726) and the @ notices are not implemented.
type ChatService struct {
	Store *store.Store
	SrvID string
	Hub   *gateway.Hub
	Log   *log.Logger
	// MinInterval is the shortest time between two messages of one role (0 = no limit).
	MinInterval time.Duration

	mu        sync.Mutex
	lastSpeak map[int64]time.Time
	history   []proto.Message           // newest last
	guildHist map[int64][]proto.Message // the guild channel's messages by guild, newest last
}

func (c *ChatService) Register(r *gateway.Router) {
	r.Handle(12720, c.private)
	r.Handle(12762, c.world)
	r.Handle(12766, c.historyOf)
	// 12723: the client says it has read the offline messages. They were handed
	// over (and deleted) when they were sent, so there is nothing left to do.
	r.Handle(12723, func(*gateway.Session, proto.Message) error { return nil })
}

// validChatMsg checks a message's text: not blank, not too long, no control characters.
func validChatMsg(msg string) bool {
	if strings.TrimSpace(msg) == "" || utf8.RuneCountInString(msg) > maxChatRunes || !utf8.ValidString(msg) {
		return false
	}
	for _, r := range msg {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// throttle reports whether the role may speak now and records the time if so.
func (c *ChatService) throttle(rid int64, now time.Time) bool {
	if c.MinInterval <= 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastSpeak == nil {
		c.lastSpeak = map[int64]time.Time{}
	}
	if last, ok := c.lastSpeak[rid]; ok && now.Sub(last) < c.MinInterval {
		return false
	}
	c.lastSpeak[rid] = now
	return true
}

// speaker is a role as chat shows it; what has no source (vip, bubble, head frame,
// guild, location) is 0 / "".
func speaker(r *store.Role) proto.Message {
	return proto.Message{
		"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "sex": r.Sex, "career": r.Career, "face_id": r.FaceID,
		"ext_list": []proto.Message{},
	}
}

// ---- private chat ----

func (c *ChatService) private(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	reply := func(ok bool, msg string) error { return s.Send(12720, proto.Message{"code": b2i(ok), "msg": msg}) }
	to, flag, text := req.Int("to_rid"), int(req.Int("len")), req.Str("msg")
	if req.Str("to_srv_id") != c.SrvID {
		return reply(false, "暂不支持跨服聊天")
	}
	target, err := c.Store.RoleByID(to)
	switch {
	case err != nil || target.SrvID != c.SrvID || target.Status != 1:
		return reply(false, "玩家不存在")
	case target.RID == me.RID:
		return reply(false, "不能和自己聊天")
	case !validChatMsg(text):
		return reply(false, "消息内容不合法")
	case !c.throttle(me.RID, s.Now()):
		return reply(false, "发言太快了，请稍后再试")
	}
	tick := s.Now().Unix()
	// flag 2: what the recipient gets, showing the sender; flag 1: the echo to the
	// sender, showing the recipient (role_controller reads rid/name as "the other side")
	if sess := c.Hub.Online(target.RID); sess != nil {
		if err := sess.Send(12721, privateMsg(2, me, flag, text, tick)); err != nil {
			c.Log.Printf("chat: push to role %d: %v", target.RID, err)
		}
	} else if err := c.Store.AddOfflineChat(me.RID, target.RID, flag, text, chatOfflineKeep); err != nil {
		return err
	}
	if err := s.Send(12721, privateMsg(1, target, flag, text, tick)); err != nil {
		return err
	}
	return reply(true, "")
}

func privateMsg(kind int, who *store.Role, flag int, text string, tick int64) proto.Message {
	return proto.Message{
		"flag": kind, "rid": who.RID, "srv_id": who.SrvID, "name": who.Name, "lev": who.Lev, "career": who.Career,
		"face_id": who.FaceID, "sex": who.Sex, "len": flag, "msg": text, "tick": tick,
	}
}

// Deliver hands a role its offline private messages (12722). It must run after
// 10301: the client drops the push while it has no role object yet.
func (c *ChatService) Deliver(s *gateway.Session) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	waiting, err := c.Store.TakeOfflineChat(me.RID)
	if err != nil || len(waiting) == 0 {
		return err
	}
	var order []int64
	bySender := map[int64][]proto.Message{}
	for _, m := range waiting {
		if _, seen := bySender[m.FromRID]; !seen {
			order = append(order, m.FromRID)
		}
		bySender[m.FromRID] = append(bySender[m.FromRID], proto.Message{"len": m.Len, "msg": m.Msg, "tick": m.SentAt})
	}
	list := []proto.Message{}
	for _, rid := range order {
		from, err := c.Store.RoleByID(rid)
		if err != nil {
			continue // the sender is gone
		}
		list = append(list, proto.Message{
			"rid": from.RID, "srv_id": from.SrvID, "name": from.Name, "lev": from.Lev, "career": from.Career,
			"face_id": from.FaceID, "sex": from.Sex, "msg_list": bySender[rid],
		})
	}
	if len(list) == 0 {
		return nil
	}
	return s.Send(12722, proto.Message{"offline_list": list})
}

// ---- world channel ----

// chatSign is the client's check value of a channel message: md5 of
// rid, srv_id, channel, len and msg run together, then "____".
func chatSign(rid int64, srvID string, channel, flag int64, msg string) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%d%s%d%d%s____", rid, srvID, channel, flag, msg)))
	return hex.EncodeToString(sum[:])
}

func (c *ChatService) world(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	reply := func(ok bool, msg string) error { return s.Send(12762, proto.Message{"code": b2i(ok), "msg": msg}) }
	channel, flag, text := req.Int("channel"), req.Int("len"), req.Str("msg")
	var gid int64
	if channel == chatGuild {
		gd, _, err := c.Store.GuildOfRole(me.RID)
		if err != nil {
			return reply(false, "你还没有加入公会")
		}
		gid = gd.GID
	}
	switch {
	case channel != chatWorld && channel != chatGuild:
		return reply(false, "该频道暂未开放")
	case !validChatMsg(text):
		return reply(false, "消息内容不合法")
	case !c.throttle(me.RID, s.Now()):
		return reply(false, "发言太快了，请稍后再试")
	}
	// like the 1197 signature, the check value is a client artefact: a mismatch is logged, not refused
	if req.Str("sign") != chatSign(me.RID, me.SrvID, channel, flag, text) {
		c.Log.Printf("chat: role %d: channel message sign mismatch", me.RID)
	}
	who := speaker(me)
	if channel == chatGuild {
		if gd, _, err := c.Store.GuildOfRole(me.RID); err == nil {
			who["gid"], who["gsrv_id"], who["g_name"] = gd.GID, gd.SrvID, gd.Name
		}
	}
	msg := proto.Message{"role_list": []proto.Message{who}, "channel": channel, "len": flag, "msg": text, "tick": s.Now().Unix()}
	c.mu.Lock()
	if channel == chatGuild {
		if c.guildHist == nil {
			c.guildHist = map[int64][]proto.Message{}
		}
		h := append(c.guildHist[gid], msg)
		if len(h) > chatHistory {
			h = h[len(h)-chatHistory:]
		}
		c.guildHist[gid] = h
	} else {
		c.history = append(c.history, msg)
		if len(c.history) > chatHistory {
			c.history = c.history[len(c.history)-chatHistory:]
		}
	}
	c.mu.Unlock()
	push := func(rid int64, sess *gateway.Session) {
		if err := sess.Send(12761, msg); err != nil {
			c.Log.Printf("chat: push to role %d: %v", rid, err)
		}
	}
	if channel == chatGuild {
		members, err := c.Store.MembersOf(gid)
		if err != nil {
			return err
		}
		for _, m := range members {
			if sess := c.Hub.Online(m.RID); sess != nil {
				push(m.RID, sess)
			}
		}
	} else {
		c.Hub.Each(push)
	}
	return reply(true, "")
}

func (c *ChatService) historyOf(s *gateway.Session, req proto.Message) error {
	if s.Role() == nil {
		return nil
	}
	list := []proto.Message{}
	switch req.Int("channel") {
	case chatWorld:
		c.mu.Lock()
		list = append(list, c.history...)
		c.mu.Unlock()
	case chatGuild:
		if gd, _, err := c.Store.GuildOfRole(s.Role().RID); err == nil {
			c.mu.Lock()
			list = append(list, c.guildHist[gd.GID]...)
			c.mu.Unlock()
		}
	}
	return s.Send(12766, proto.Message{"channel": req.Int("channel"), "msg_list": list})
}

// chatNoticeTop is the channel bit of the scrolling banner at the top of the screen
// (chat_const.lua Channel.NoticeTop): the client shows the message as it is, without a speaker.
const chatNoticeTop = 32

// Broadcast scrolls msg across the top of every online player's screen (12761 on the
// NoticeTop channel) and returns how many were sent it. It does not go into the chat history.
func (m *MailService) Broadcast(msg string) int {
	push := proto.Message{"role_list": []proto.Message{}, "channel": chatNoticeTop, "len": 0, "msg": msg, "tick": time.Now().Unix()}
	n := 0
	m.Hub.Each(func(rid int64, sess *gateway.Session) {
		if err := sess.Send(12761, push); err != nil {
			m.Log.Printf("broadcast: push to role %d: %v", rid, err)
			return
		}
		n++
	})
	return n
}
