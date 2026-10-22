package game

import (
	"errors"
	"log"
	"strings"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// NoticeService answers the notice tab of the mailbox (10950 list, 10952 open) and
// pushes admin changes to online roles. Semantics come from the restored client
// (mail_controller.lua, mail_model.lua, mail_cell.lua).
type NoticeService struct {
	Store *store.Store
	Hub   *gateway.Hub
	Log   *log.Logger
}

func (n *NoticeService) Register(r *gateway.Router) {
	r.Handle(10950, n.list)
	r.Handle(10952, n.open)
}

const summaryRunes = 36

// summarize is the one-line text the list shows under the title: the first line of
// the content, cut to what fits.
func summarize(content string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= summaryRunes {
		return line
	}
	return string([]rune(line)[:summaryRunes]) + "…"
}

// board is one entry of a 10950 reply. type is 1 for every notice; flag is 1 once
// the role has opened it (the client greys those out).
func board(v store.NoticeView) proto.Message {
	return proto.Message{
		"id": v.ID, "type": 1, "title": v.Title, "summary": summarize(v.Content), "content": v.Content,
		"start_time": v.StartAt, "end_time": v.EndAt, "flag": b2i(v.Read),
	}
}

func (n *NoticeService) list(s *gateway.Session, _ proto.Message) error {
	role := s.Role()
	if role == nil {
		n.Log.Printf("10950 before a role login: ignored")
		return nil
	}
	views, err := n.Store.NoticesForRole(role)
	if err != nil {
		return err
	}
	out := make([]proto.Message, 0, len(views))
	for _, v := range views {
		out = append(out, board(v))
	}
	return s.Send(10950, proto.Message{"type": 1, "board_list": out})
}

// open is 10952: the client sends it when the player taps a notice and opens the
// detail only on code 1.
func (n *NoticeService) open(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		n.Log.Printf("10952 before a role login: ignored")
		return nil
	}
	id := req.Int("id")
	reply := proto.Message{"code": 0, "msg": "公告不存在", "id": id}
	opened := false
	switch _, err := n.Store.NoticeForRole(role, id); {
	case err == nil:
		if err := n.Store.MarkNoticeRead(role.RID, id); err != nil {
			return err
		}
		reply["code"], reply["msg"] = 1, ""
		opened = true
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	if err := s.Send(10952, reply); err != nil {
		return err
	}
	if !opened {
		return nil
	}
	// The client does not mark a notice read by itself when it opens it: the grey
	// "read" look and the tab's red count only change when the server sends the entry
	// again with flag 1 (10950 merges entries into the list). Seen on a device.
	v, err := n.Store.NoticeForRole(role, id)
	if err != nil {
		return err
	}
	return s.Send(10950, proto.Message{"type": 1, "board_list": []proto.Message{board(*v)}})
}

// PushNotice sends the current state of notice id to every online role: roles that
// may see it get the entry (10950 merges it into their list), the others get 10951
// so a hidden, expired or deleted notice disappears from an open list. It returns
// how many roles were sent the notice itself and how many were told to drop it.
func (n *NoticeService) PushNotice(id int64) (shown, dropped int, err error) {
	var firstErr error
	n.Hub.Each(func(_ int64, s *gateway.Session) {
		role := s.Role()
		if role == nil {
			return
		}
		v, err := n.Store.NoticeForRole(role, id)
		switch {
		case err == nil:
			err = s.Send(10950, proto.Message{"type": 1, "board_list": []proto.Message{board(*v)}})
			shown++
		case errors.Is(err, store.ErrNotFound):
			err = s.Send(10951, proto.Message{"id": id})
			dropped++
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return shown, dropped, firstErr
}

// DropNotice tells every online role that notice id is gone (it was deleted, so
// there is no row left to ask).
func (n *NoticeService) DropNotice(id int64) (dropped int, err error) {
	var firstErr error
	n.Hub.Each(func(_ int64, s *gateway.Session) {
		if s.Role() == nil {
			return
		}
		if err := s.Send(10951, proto.Message{"id": id}); err != nil && firstErr == nil {
			firstErr = err
		}
		dropped++
	})
	return dropped, firstErr
}
