// Package game holds the command handlers. Only the login sequence is
// implemented, and only with fields whose meaning is confirmed from the
// client code or a device capture; everything else is left unanswered rather
// than invented.
package game

import (
	"errors"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// Reply codes the client understands for 1164 (login_controller.lua
// on1110Rolelist): 1 = ok, 2 = drop the connection and show msg, 4 = drop and
// open the notice (maintenance) view. For 10191/10192, 0 shows msg and returns
// to the enter-game panel, 2 shows msg and returns to the account switch.
const (
	codeFail     = 0
	codeOK       = 1
	codeDrop     = 2
	codeNotice   = 4
	maxNameRunes = 12 // server policy, not a protocol value
)

type Login struct {
	Store *store.Store
	SrvID string
	Hub   *gateway.Hub
	Log   *log.Logger
	// MaxRoles is how many roles one account may create per server (policy).
	MaxRoles int
	// Friends, when set, is told when a role enters the game.
	Friends *FriendService
	// OnlineGift, when set, starts counting a role's online time when it enters the game.
	OnlineGift *OnlineGiftService
	// Chat, when set, hands the role its offline private messages once the main city is up.
	Chat *ChatService
	// NewRoleFlow asks a brand-new role to name itself (10342) once its main city is up.
	NewRoleFlow bool
}

func (l *Login) Register(r *gateway.Router) {
	r.Handle(1164, l.roleList)
	r.Handle(10191, l.createRole)
	r.Handle(10192, func(s *gateway.Session, m proto.Message) error { return l.enter(s, m, 10192) })
	r.Handle(10193, func(s *gateway.Session, m proto.Message) error { return l.enter(s, m, 10193) })
	r.Handle(10300, l.roleDataRequest)
	r.Handle(10343, l.rename)
	r.Handle(10906, l.serverOpenDay)
	r.Handle(1169, heartbeat)
}

func argMap(m proto.Message) map[string]string {
	out := map[string]string{}
	for _, kv := range m.Arr("args") {
		out[kv.Str("key")] = kv.Str("val")
	}
	return out
}

func reply1164(s *gateway.Session, code int, msg string, roles []proto.Message) error {
	if roles == nil {
		roles = []proto.Message{}
	}
	// least_career: meaning not confirmed; 0 is the neutral value.
	return s.Send(1164, proto.Message{"code": code, "msg": msg, "roles": roles, "least_career": 0})
}

func (l *Login) roleList(s *gateway.Session, req proto.Message) error {
	args := argMap(req)
	acc, err := l.Store.AccountByToken(args["token"])
	if err != nil || acc.Account != args["account"] {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return reply1164(s, codeDrop, "登录凭证无效，请重新登录", nil)
	}
	if acc.Status != 1 {
		return reply1164(s, codeDrop, "账号已被封禁", nil)
	}
	srv, err := l.Store.Server(l.SrvID)
	if err != nil {
		return err
	}
	if srv.Status != 1 {
		return reply1164(s, codeNotice, "服务器维护中", nil)
	}
	if want := args["enter_srv_id"]; want != "" && want != l.SrvID {
		l.Log.Printf("1164: client asked for %q, this gateway serves %q", want, l.SrvID)
	}
	s.SetAccount(acc)
	l.Store.TouchAccountLogin(acc.ID)
	roles, err := l.Store.RolesByAccount(acc.ID, l.SrvID)
	if err != nil {
		return err
	}
	out := make([]proto.Message, 0, len(roles))
	for _, r := range roles {
		out = append(out, proto.Message{
			"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "sex": r.Sex,
			"career": r.Career, "face_id": r.FaceID, "is_online": b2i(l.Hub.Online(r.RID) != nil),
		})
	}
	return reply1164(s, codeOK, "", out)
}

func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	if n == 0 || n > maxNameRunes || name != strings.TrimSpace(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func (l *Login) createRole(s *gateway.Session, req proto.Message) error {
	fail := func(msg string) error {
		return s.Send(10191, proto.Message{"code": codeFail, "msg": msg, "rid": 0, "srv_id": l.SrvID, "name": "", "reg_time": 0})
	}
	acc := s.Account()
	if acc == nil {
		return fail("请先登录")
	}
	name := req.Str("name")
	// The real client creates the role with an empty name and asks for a name
	// afterwards (confirmed on device: 10191 arrives with name ""), so an empty
	// name gets a unique placeholder instead of being refused.
	if name != "" && !validName(name) {
		return fail("角色名不合法")
	}
	existing, err := l.Store.RolesByAccount(acc.ID, l.SrvID)
	if err != nil {
		return err
	}
	if l.MaxRoles > 0 && len(existing) >= l.MaxRoles {
		return fail("角色数量已达上限")
	}
	// face_id is not part of the create request, so it starts at 0.
	var role *store.Role
	if name == "" {
		role, err = l.Store.CreateRoleAutoName(acc.ID, l.SrvID, "玩家", int(req.Int("sex")), int(req.Int("career")), 0)
	} else {
		role, err = l.Store.CreateRole(acc.ID, l.SrvID, name, int(req.Int("sex")), int(req.Int("career")), 0)
	}
	if errors.Is(err, store.ErrConflict) {
		return fail("角色名已被使用")
	}
	if err != nil {
		return err
	}
	return s.Send(10191, proto.Message{
		"code": codeOK, "msg": "", "rid": role.RID, "srv_id": role.SrvID, "name": role.Name, "reg_time": role.CreatedAt,
	})
}

// enter implements 10192 (login) and 10193 (relogin); both answer with the same fields.
func (l *Login) enter(s *gateway.Session, req proto.Message, cmd uint16) error {
	fail := func(code int, msg string) error {
		return s.Send(cmd, proto.Message{"code": code, "msg": msg, "timestamp": s.Now().Unix(), "world_lev": 0})
	}
	acc := s.Account()
	if acc == nil {
		return fail(codeDrop, "请先登录")
	}
	role, err := l.Store.RoleByID(req.Int("rid"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && (role.AccountID != acc.ID || role.SrvID != l.SrvID || req.Str("srv_id") != l.SrvID)) {
		return fail(codeFail, "角色不存在")
	}
	if err != nil {
		return err
	}
	if role.Status != 1 {
		return fail(codeFail, "角色已被封禁")
	}
	if prev := l.Hub.Register(role.RID, s); prev != nil {
		// 10310 is the server-initiated "you were disconnected" message.
		prev.Send(10310, proto.Message{"is_show": 1, "msg": "账号在别处登录"})
		prev.Close()
	}
	s.SetRole(role)
	l.Store.TouchRoleLogin(role.RID)
	if l.Friends != nil {
		l.Friends.Entered(role)
	}
	if l.OnlineGift != nil {
		l.OnlineGift.Entered(role.RID, s.Now())
	}
	worldLev, err := l.Store.WorldLevel(l.SrvID)
	if err != nil {
		return err
	}
	return s.Send(cmd, proto.Message{"code": codeOK, "msg": "", "timestamp": s.Now().Unix(), "world_lev": worldLev})
}

// rename implements 10343. The client shows msg as a toast and, when code is 1,
// takes name, sex and is_first_rename from the reply (role_controller.lua
// rgp_hander10343). The first rename is free and also fixes the sex; later ones
// keep the sex. What a later rename should cost is unknown, so it is free here
// (policy, not protocol).
func (l *Login) rename(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		l.Log.Printf("10343 before any role login: ignored")
		return nil
	}
	fresh, err := l.Store.RoleByID(role.RID)
	if err != nil {
		return err
	}
	reply := func(code int, msg string, r *store.Role) error {
		return s.Send(10343, proto.Message{"code": code, "msg": msg, "name": r.Name, "sex": r.Sex, "is_first_rename": 1 - r.Renamed})
	}
	name, sex := req.Str("name"), int(req.Int("sex"))
	switch {
	case !validName(name):
		return reply(codeFail, "角色名不合法", fresh)
	case fresh.Renamed == 0 && sex != 0 && sex != 1:
		return reply(codeFail, "请选择性别", fresh)
	case fresh.Renamed == 1 && name == fresh.Name:
		return reply(codeFail, "新名字与当前名字相同", fresh)
	}
	updated, err := l.Store.RenameRole(fresh.RID, name, sex)
	if errors.Is(err, store.ErrConflict) {
		return reply(codeFail, "角色名已被使用", fresh)
	}
	if err != nil {
		return err
	}
	s.SetRole(updated)
	// the original answers the first (free) rename with an empty msg, so the client
	// shows no toast (capture original-2026-10-22); later renames keep ours
	msg := "改名成功"
	if fresh.Renamed == 0 {
		msg = ""
		// The original hands out the starter hero here, before it answers: the first
		// rename ends the new-role story, and the hero is what the guide then points at.
		if err := l.giveStarter(s, updated); err != nil {
			return err
		}
	}
	return reply(codeOK, msg, updated)
}

// giveStarter grants a role its starter hero when it has none yet and tells the
// client the way the original does at the first rename (capture
// original-2026-10-22, 08:38:50): 11001 with the hero, 11211 with the campaign team,
// 10301 with the role data (it carries the sex just chosen) and 10306 with the power;
// the original follows the rename reply with 11099, which is answered on request here.
// A role that already has heroes is left alone (GrantStarterPartners grants once).
func (l *Login) giveStarter(s *gateway.Session, role *store.Role) error {
	granted, err := l.Store.GrantStarterPartners(role.RID, starterSeeds(role.Sex))
	if err != nil || !granted {
		return err
	}
	heroes, err := l.Store.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	msgs := make([]proto.Message, 0, len(heroes))
	for _, h := range heroes {
		if _, ok := partnerTable[h.BID]; ok {
			msgs = append(msgs, partnerMessage(h))
		}
	}
	if err := s.Send(11001, proto.Message{"partners": msgs}); err != nil {
		return err
	}
	f, err := formationOf(l.Store, role.RID, formDrama)
	if err != nil {
		return err
	}
	if err := s.Send(11211, formationMessage(f)); err != nil {
		return err
	}
	if err := sendRoleBase(l.Store, s, role); err != nil {
		return err
	}
	power := rolePower(heroes)
	return s.Send(10306, proto.Message{"power": power, "max_power": power})
}

func heartbeat(s *gateway.Session, _ proto.Message) error {
	return s.Send(1169, proto.Message{"time": s.Now().Unix()})
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
