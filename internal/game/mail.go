package game

import (
	"errors"
	"log"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// MailService answers the client's mail commands and pushes new mail to roles
// that are online. Wire format and semantics come from the restored client
// (mail_controller.lua, mail_model.lua, mail_vo.lua).
type MailService struct {
	Store *store.Store
	SrvID string
	Hub   *gateway.Hub
	Log   *log.Logger
	// Notices, when set, also gets the notice push endpoints on AdminHandler.
	Notices *NoticeService
}

func (m *MailService) Register(r *gateway.Router) {
	r.Handle(10800, m.list)
	r.Handle(10805, m.read)
	r.Handle(10806, m.detail)
	r.Handle(10801, m.claim)
	r.Handle(10802, m.claimAll)
	r.Handle(10804, m.remove)
}

// brief is the list entry (10800 / 10803). The client keys a mail by id plus
// srv_id, so srv_id is always the role's server, also for all-server broadcasts.
func brief(role *store.Role, rm store.RoleMail) proto.Message {
	return proto.Message{
		"id": rm.ID, "srv_id": role.SrvID, "type": rm.Type, "from_name": rm.FromName, "subject": rm.Subject,
		"send_time": rm.CreatedAt, "read_time": rm.ReadAt, "time_out": rm.ExpireAt,
		"status": rm.Status(), "has_items": b2i(rm.HasItems()),
	}
}

// detail is 10806. Attachments are shown only while they can still be claimed.
func detail(role *store.Role, rm store.RoleMail) proto.Message {
	msg := brief(role, rm)
	msg["content"] = rm.Content
	assets, items := []proto.Message{}, []proto.Message{}
	if rm.HasItems() {
		for _, a := range rm.Attachments {
			if a.Kind == store.AttachAsset {
				assets = append(assets, proto.Message{"label": a.ItemID, "val": a.Num})
			} else {
				items = append(items, proto.Message{"base_id": a.ItemID, "quantity": a.Num})
			}
		}
	}
	msg["assets"], msg["items"] = assets, items
	return msg
}

// roleOf returns the logged-in role; a request for another server's mail never matches.
func (m *MailService) roleOf(s *gateway.Session, srvID string) (*store.Role, bool) {
	role := s.Role()
	if role == nil {
		m.Log.Printf("mail request before a role login: ignored")
		return nil, false
	}
	return role, srvID == "" || srvID == role.SrvID
}

func (m *MailService) list(s *gateway.Session, _ proto.Message) error {
	role, _ := m.roleOf(s, "")
	if role == nil {
		return nil
	}
	mails, err := m.Store.MailsForRole(role)
	if err != nil {
		return err
	}
	out := make([]proto.Message, 0, len(mails))
	for _, rm := range mails {
		out = append(out, brief(role, rm))
	}
	return s.Send(10800, proto.Message{"mail": out})
}

func (m *MailService) read(s *gateway.Session, req proto.Message) error {
	role, ok := m.roleOf(s, req.Str("srv_id"))
	if role == nil {
		return nil
	}
	reply := proto.Message{"id": req.Int("id"), "srv_id": req.Str("srv_id"), "code": 0, "msg": "邮件不存在", "read_time": 0, "is_delete": 0}
	if ok {
		at, err := m.Store.MarkMailRead(role.RID, req.Int("id"))
		switch {
		case err == nil:
			reply["code"], reply["msg"], reply["read_time"] = 1, "", at
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
	}
	return s.Send(10805, reply)
}

func (m *MailService) detail(s *gateway.Session, req proto.Message) error {
	role, ok := m.roleOf(s, req.Str("srv_id"))
	if role == nil || !ok {
		return nil
	}
	rm, err := m.Store.MailForRole(role.RID, req.Int("id"))
	if errors.Is(err, store.ErrNotFound) {
		m.Log.Printf("10806: role %d asked for mail %d it does not have", role.RID, req.Int("id"))
		return nil // the client has no failure form of this reply
	}
	if err != nil {
		return err
	}
	return s.Send(10806, detail(role, *rm))
}

func (m *MailService) claim(s *gateway.Session, req proto.Message) error {
	role, ok := m.roleOf(s, req.Str("srv_id"))
	if role == nil {
		return nil
	}
	reply := proto.Message{"id": req.Int("id"), "srv_id": req.Str("srv_id"), "code": 0, "msg": "邮件不存在"}
	if ok {
		_, _, err := m.Store.ClaimMail(role.RID, req.Int("id"))
		switch {
		case err == nil:
			reply["code"], reply["msg"] = 1, "领取成功"
		case errors.Is(err, store.ErrNoAttachments):
			reply["msg"] = "没有可领取的附件"
		case !errors.Is(err, store.ErrNotFound):
			return err
		}
	}
	if err := s.Send(10801, reply); err != nil {
		return err
	}
	if reply["code"] == 1 {
		return m.refreshAssets(s, role)
	}
	return nil
}

func (m *MailService) claimAll(s *gateway.Session, _ proto.Message) error {
	role, _ := m.roleOf(s, "")
	if role == nil {
		return nil
	}
	claimed, err := m.Store.ClaimAllMails(role)
	if err != nil {
		return err
	}
	ids, msg := make([]proto.Message, 0, len(claimed)), "没有可领取的附件"
	for _, c := range claimed {
		ids = append(ids, proto.Message{"id": c.ID, "srv_id": role.SrvID, "read_time": c.ClaimedAt})
	}
	if len(ids) > 0 {
		msg = "领取成功"
	}
	if err := s.Send(10802, proto.Message{"ids": ids, "msg": msg}); err != nil {
		return err
	}
	if len(ids) > 0 {
		return m.refreshAssets(s, role)
	}
	return nil
}

func (m *MailService) remove(s *gateway.Session, req proto.Message) error {
	role, _ := m.roleOf(s, "")
	if role == nil {
		return nil
	}
	var want []int64
	for _, e := range req.Arr("ids") {
		if e.Str("srv_id") == role.SrvID {
			want = append(want, e.Int("id"))
		}
	}
	done, err := m.Store.DeleteMails(role.RID, want)
	if err != nil {
		return err
	}
	ids, msg := make([]proto.Message, 0, len(done)), "没有可删除的邮件"
	for _, id := range done {
		ids = append(ids, proto.Message{"id": id, "srv_id": role.SrvID})
	}
	if len(ids) > 0 {
		msg = "删除成功"
	}
	return s.Send(10804, proto.Message{"ids": ids, "msg": msg})
}

// refreshAssets re-sends 10302 so the resource bar shows what was just claimed,
// then the backpack, which may have gained items.
func (m *MailService) refreshAssets(s *gateway.Session, role *store.Role) error {
	if err := sendAssets(s, m.Store, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, m.Store)
}

// PushMail tells the online roles who are due the mail with 10803 and returns
// how many were told. Offline roles get it on their next 10800. A broadcast is
// delivered to an online role first, so listing it later does not add a second copy.
func (m *MailService) PushMail(mailID int64) (int, error) {
	mail, err := m.Store.MailByID(mailID)
	if err != nil {
		return 0, err
	}
	var direct map[int64]bool
	if mail.Scope == store.MailDirect {
		rids, err := m.Store.DirectRecipients(mailID)
		if err != nil {
			return 0, err
		}
		direct = map[int64]bool{}
		for _, r := range rids {
			direct[r] = true
		}
	}
	pushed := 0
	var firstErr error
	m.Hub.Each(func(rid int64, s *gateway.Session) {
		role := s.Role()
		if role == nil {
			return
		}
		switch {
		case direct != nil && !direct[rid]:
			return
		case direct == nil && ((mail.SrvID != "" && mail.SrvID != role.SrvID) || mail.CreatedAt < role.CreatedAt):
			return
		}
		if direct == nil {
			if err := m.Store.DeliverBroadcasts(role); err != nil {
				firstErr = err
				return
			}
		}
		rm, err := m.Store.MailForRole(rid, mailID)
		if err != nil {
			return // e.g. expired broadcast: not delivered
		}
		if err := s.Send(10803, proto.Message{"mail": []proto.Message{brief(role, *rm)}}); err != nil {
			firstErr = err
			return
		}
		pushed++
	})
	return pushed, firstErr
}
