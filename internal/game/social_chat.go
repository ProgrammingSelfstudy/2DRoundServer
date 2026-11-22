package game

import (
	"errors"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const privateChatUnavailable = "暂时无法与该玩家私聊"

var errChatThrottled = errors.New("chat: rate limit")

func (c *ChatService) pushPrivate(me, to *store.Role, flag int, text string, now time.Time, h5Pair bool) error {
	if h5Pair {
		defer c.Store.LockChatRecipient(to.RID)()
		blocked, err := c.Store.PrivateChatBlocked(me.RID, to.RID)
		if err != nil {
			return err
		}
		if blocked {
			return store.ErrSocialBlocked
		}
	}
	if !c.throttle(me.RID, now) {
		return errChatThrottled
	}
	if sess := c.Hub.Online(to.RID); sess != nil {
		err := sess.Send(12721, privateMsg(2, me, flag, text, now.Unix()))
		if err == nil {
			return nil
		}
		c.Log.Printf("chat: push to role %d: %v", to.RID, err)
		if !h5Pair {
			return nil
		}
	}
	return c.Store.AddOfflineChat(me.RID, to.RID, flag, text, chatOfflineKeep)
}

func (c *ChatService) deliverPrivate(s *gateway.Session, to int64, waiting []store.OfflineChat, h5 bool) error {
	if len(waiting) == 0 {
		return nil
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
		if h5 {
			blocked, err := c.Store.PrivateChatBlocked(rid, to)
			if err != nil {
				return err
			}
			if blocked {
				continue
			}
		}
		from, err := c.Store.RoleByID(rid)
		if err != nil {
			if h5 && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			continue
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

// A blacklist hides a speaker from the listener's channels and their history.
// The speaker can still see the listener's channel posts.
func (c *ChatService) visibleChatHistory(rid int64, history []proto.Message) ([]proto.Message, error) {
	if len(history) == 0 {
		return history, nil
	}
	blocked, err := c.Store.Blacklist(rid)
	if err != nil || len(blocked) == 0 {
		return history, err
	}
	muted := make(map[int64]bool, len(blocked))
	for _, other := range blocked {
		muted[other.RID] = true
	}
	visible := history[:0]
	for _, msg := range history {
		who := msg.Arr("role_list")
		if len(who) == 0 || !muted[who[0].Int("rid")] {
			visible = append(visible, msg)
		}
	}
	return visible, nil
}
