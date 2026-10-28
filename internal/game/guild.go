package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// guild.json: the guild rules of the client (tools/extract_guild.py).
//
//go:embed data/guild.json
var guildJSON []byte

var guildCfg = func() struct {
	Const      map[string]int64
	Levels     []struct{ Lev, Exp, Num int64 }
	Assistants map[string]int
	Donate     []struct {
		ID         int
		Name       string
		Loss, Gain [][2]int64
	}
	Boxes []struct {
		ID      int
		Val     int64
		Rewards [][2]int64
	}
	PowerChoices []int64 `json:"power_choices"`
} {
	var c struct {
		Const      map[string]int64
		Levels     []struct{ Lev, Exp, Num int64 }
		Assistants map[string]int
		Donate     []struct {
			ID         int
			Name       string
			Loss, Gain [][2]int64
		}
		Boxes []struct {
			ID      int
			Val     int64
			Rewards [][2]int64
		}
		PowerChoices []int64 `json:"power_choices"`
	}
	if err := json.Unmarshal(guildJSON, &c); err != nil || len(c.Levels) == 0 || len(c.Donate) == 0 || len(c.Boxes) == 0 || len(c.Const) == 0 {
		panic("game: bad guild.json")
	}
	return c
}()

// guildLabel maps the client's currency ids in the guild tables to the ones we keep: the guild tables
// still name the deprecated silver coin (2) and blue / red diamond (15) of the item table, which are
// coin (1) and gold (3) here.
func guildLabel(id int64) int64 {
	switch id {
	case 2:
		return 1
	case 15:
		return 3
	}
	return id
}

func guildGains(list [][2]int64) []sellGain {
	out := make([]sellGain, 0, len(list))
	for _, e := range list {
		out = append(out, sellGain{guildLabel(e[0]), e[1]})
	}
	return out
}

const (
	guildLogGeneral = 0 // GuildConst.show_type.all: shown under "all"
	guildLogDonate  = 2 // GuildConst.show_type.guild_donate
	guildNameMin    = 2
	guildNameMax    = 6
	guildSignMax    = 60
	guildPageSize   = 20
	guildPageMax    = 50
)

func guildCapacity(lev int64) int {
	if lev < 1 {
		lev = 1
	}
	if int(lev) > len(guildCfg.Levels) {
		lev = int64(len(guildCfg.Levels))
	}
	return int(guildCfg.Levels[lev-1].Num)
}

func guildLevelExp() []int64 {
	out := make([]int64, len(guildCfg.Levels))
	for i, l := range guildCfg.Levels {
		out[i] = l.Exp
	}
	return out
}

func validGuildName(n string) bool {
	if c := utf8.RuneCountInString(n); c < guildNameMin || c > guildNameMax {
		return false
	}
	for _, r := range n {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func validGuildSign(v string) bool {
	if utf8.RuneCountInString(v) > guildSignMax {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// GuildService runs the guilds of one server.
type GuildService struct {
	Store *store.Store
	SrvID string
	Hub   *gateway.Hub
	Log   *log.Logger
}

// guildPower is a role's power the way the lists show it.
func (g *GuildService) guildPower(rid int64) int64 {
	heroes, err := g.Store.PartnersOf(rid)
	if err != nil {
		return 0
	}
	return rolePower(heroes)
}

func (g *GuildService) cfg(name string) int64 { return guildCfg.Const[name] }

// quitCD is how long after leaving (or being kicked) a role cannot join or create; a leader that
// disbanded waits longer to create again (the client's guild_quit_cd and the two create cool-downs).
func (g *GuildService) cooldownLeft(rid, now int64, creating bool) (int64, error) {
	quit, wasLeader, err := g.Store.CooldownOf(rid)
	if err != nil || quit == 0 {
		return 0, err
	}
	cd := g.cfg("guild_quit_cd")
	if creating {
		cd = g.cfg("guild_mem_create_guild_cd") * 3600
		if wasLeader {
			cd = g.cfg("guild_leader_create_guild_cd") * 3600
		}
	}
	if left := quit + cd - now; left > 0 {
		return left, nil
	}
	return 0, nil
}

// infoMessage is the 13518 answer: the guild as its members see it.
func (g *GuildService) infoMessage(gd *store.Guild, day int64) (proto.Message, error) {
	n, err := g.Store.MemberCount(gd.GID)
	if err != nil {
		return nil, err
	}
	leader, err := g.Store.RoleByID(gd.LeaderRID)
	if err != nil {
		return nil, err
	}
	vitality, err := g.Store.GuildVitality(gd.GID)
	if err != nil {
		return nil, err
	}
	dayExp, recruit := gd.DayExp, gd.RecruitNum
	if gd.DayExpDay != day {
		dayExp = 0
	}
	if int64(gd.RecruitDay) != day {
		recruit = 0
	}
	return proto.Message{
		"gid": gd.GID, "gsrv_id": gd.SrvID, "name": gd.Name, "lev": gd.Lev, "members_num": n, "members_max": guildCapacity(gd.Lev),
		"leader_name": leader.Name, "rid": leader.RID, "srv_id": leader.SrvID, "sign": gd.Sign, "exp": gd.Exp, "day_exp": dayExp,
		"apply_type": gd.ApplyType, "apply_lev": gd.ApplyLev, "recruit_num": recruit, "vitality": vitality, "apply_power": gd.ApplyPower, "rank_idx": -1,
	}, nil
}

func (g *GuildService) memberMessage(m store.MemberRow) proto.Message {
	online := g.Hub.Online(m.RID) != nil
	login := m.LastLoginAt
	if !online && m.LastLogoutAt > 0 {
		login = m.LastLogoutAt
	}
	face := m.FaceID
	if face == 0 {
		face = store.DefaultFaceID
	}
	return proto.Message{
		"rid": m.RID, "srv_id": m.SrvID, "name": m.Name, "lev": m.Lev, "face": face, "post": m.Post, "online": b2i(online),
		"vip_lev": m.VIPLev, "power": g.guildPower(m.RID), "join_time": m.JoinTime, "login_time": login, "donate": m.Donate,
		"day_donate": m.DayDonate, "avatar_bid": 0, "sex": m.Sex, "active_lev": 0, "day_dun_time": 0, "day_war_time": 0,
		"face_update_time": 0, "face_file": "",
	}
}

// pushRoleBase sends an online role its 10301 again; the client takes the new guild fields from it.
func (g *GuildService) pushRoleBase(rid int64) {
	sess := g.Hub.Online(rid)
	if sess == nil {
		return
	}
	fresh, err := g.Store.RoleByID(rid)
	if err != nil {
		return
	}
	if err := sendRoleBase(g.Store, sess, fresh); err != nil {
		g.Log.Printf("guild: 10301 push to %d: %v", rid, err)
	}
}

// pushInfo sends every online member the guild's 13518.
func (g *GuildService) pushInfo(gid int64, day int64) {
	gd, err := g.Store.GuildByID(gid)
	if err != nil {
		return
	}
	msg, err := g.infoMessage(gd, day)
	if err != nil {
		return
	}
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return
	}
	for _, m := range rows {
		if sess := g.Hub.Online(m.RID); sess != nil {
			sess.Send(13518, msg)
		}
	}
}

// pushMembers tells the online members about changed members: typ 1 they were added or changed, 2 they left.
func (g *GuildService) pushMembers(gid int64, typ int, changed []store.MemberRow, gone []int64) {
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return
	}
	list := []proto.Message{}
	if typ == 2 {
		for _, rid := range gone {
			if r, err := g.Store.RoleByID(rid); err == nil {
				list = append(list, proto.Message{"rid": r.RID, "srv_id": r.SrvID})
			}
		}
	} else {
		for _, m := range changed {
			list = append(list, g.memberMessage(m))
		}
	}
	for _, m := range rows {
		if sess := g.Hub.Online(m.RID); sess != nil {
			sess.Send(13542, proto.Message{"type": typ, "members": list})
		}
	}
}

func (g *GuildService) memberRow(gid, rid int64) (store.MemberRow, bool) {
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return store.MemberRow{}, false
	}
	for _, m := range rows {
		if m.RID == rid {
			return m, true
		}
	}
	return store.MemberRow{}, false
}

// pushApplyRed lights the application red dot of the online leader and assistants.
func (g *GuildService) pushApplyRed(gid int64) {
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return
	}
	for _, m := range rows {
		if m.Post <= store.PostAssistant {
			if sess := g.Hub.Online(m.RID); sess != nil {
				sess.Send(13573, proto.Message{"code": 1})
			}
		}
	}
}

// pushLog stores a line of the guild's log and shows it to the online members.
func (g *GuildService) pushLog(gid int64, kind int, rid int64, name, msg string, now int64) {
	l := store.GuildLog{GID: gid, Type: kind, RID: rid, RoleName: name, Msg: msg, Time: now}
	if err := g.Store.AddGuildLog(l); err != nil {
		g.Log.Printf("guild: log: %v", err)
		return
	}
	logs, err := g.Store.GuildLogs(gid, 1)
	if err != nil || len(logs) == 0 {
		return
	}
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return
	}
	srv := ""
	if gd, err := g.Store.GuildByID(gid); err == nil {
		srv = gd.SrvID
	}
	entry := proto.Message{"id": logs[0].ID % 256, "gid": gid, "gsrv_id": srv, "type": kind, "time": now, "rid": rid, "srv_id": g.SrvID, "role_name": name, "msg": msg}
	for _, m := range rows {
		if sess := g.Hub.Online(m.RID); sess != nil {
			sess.Send(13578, proto.Message{"guild_log_info_list": []proto.Message{entry}})
		}
	}
}

// Register registers the guild commands.
func (g *GuildService) Register(r *gateway.Router) {
	g.registerBasics(r)
	g.registerMembers(r)
	g.registerDonate(r)
	g.registerSkills(r)
	g.registerBoss(r)
	g.registerSecret(r)
	g.registerRedbag(r)
}

var errNoGuild = errors.New("no guild")

// mine reads the guild and place of the session's role.
func (g *GuildService) mine(s *gateway.Session) (*store.Guild, *store.GuildMember, error) {
	gd, m, err := g.Store.GuildOfRole(s.Role().RID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, errNoGuild
	}
	return gd, m, err
}

func gcode(s *gateway.Session, cmd uint16, ok bool, msg string) error {
	return s.Send(cmd, proto.Message{"code": b2i(ok), "msg": msg})
}

func (g *GuildService) payFor(cost [][2]int64) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(cost), 1, assets, items)
	return
}

func (g *GuildService) gemCost(name string) (assets, items map[int64]int64) {
	return g.payFor([][2]int64{{3, g.cfg(name)}})
}

// ---- joining, creating, leaving ----

func (g *GuildService) registerBasics(r *gateway.Router) {
	// 13500 create
	r.Handle(13500, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		fresh, err := g.Store.RoleByID(role.RID)
		if err != nil {
			return err
		}
		name, sign := strings.TrimSpace(req.Str("name")), strings.TrimSpace(req.Str("sign"))
		applyType, applyLev, applyPower := int(req.Int("apply_type")), int(req.Int("apply_lev")), req.Int("apply_power")
		switch {
		case !validGuildName(name):
			return gcode(s, 13500, false, "公会名称需为2-6个汉字、字母或数字")
		case !validGuildSign(sign):
			return gcode(s, 13500, false, "公会宣言过长或含有非法字符")
		case applyType < 0 || applyType > 2:
			return gcode(s, 13500, false, "参数错误")
		case int64(fresh.Lev) < g.cfg("guild_min_lev"):
			return gcode(s, 13500, false, fmt.Sprintf("等级达到%d级才能创建公会", g.cfg("guild_min_lev")))
		case int64(fresh.VIPLev) < g.cfg("maintain_vip_condition"):
			return gcode(s, 13500, false, fmt.Sprintf("需达VIP%d才可创建公会", g.cfg("maintain_vip_condition")))
		}
		if applyLev < 1 {
			applyLev = 1
		}
		if _, _, err := g.Store.GuildOfRole(fresh.RID); err == nil {
			return gcode(s, 13500, false, "你已经加入了公会")
		}
		now := s.Now().Unix()
		if left, err := g.cooldownLeft(fresh.RID, now, true); err != nil {
			return err
		} else if left > 0 {
			return gcode(s, 13500, false, "退出公会后需要等待一段时间才能创建公会")
		}
		assets, items := g.gemCost("create_gold")
		gid, err := g.Store.CreateGuild(store.Guild{SrvID: g.SrvID, Name: name, Sign: sign, LeaderRID: fresh.RID, ApplyType: applyType, ApplyLev: applyLev, ApplyPower: applyPower}, now, assets, items)
		switch {
		case errors.Is(err, store.ErrNameTaken):
			return gcode(s, 13500, false, "公会名称已被使用")
		case errors.Is(err, store.ErrConflict):
			return gcode(s, 13500, false, "钻石不足")
		case err != nil:
			return err
		}
		g.pushLog(gid, guildLogGeneral, fresh.RID, fresh.Name, "创建了公会", now)
		growGuild(g.Store, fresh.RID, name, fresh.RID, true, now)
		if err := sendAssets(s, g.Store, fresh.RID); err != nil {
			return err
		}
		if err := gcode(s, 13500, true, "创建成功"); err != nil {
			return err
		}
		g.pushRoleBase(fresh.RID)
		g.pushInfo(gid, dayKey(s.Now()))
		return nil
	})

	// 13501 list
	r.Handle(13501, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		page, num, flag := int(req.Int("page")), int(req.Int("num")), int(req.Int("flag"))
		name := strings.TrimSpace(req.Str("name"))
		if flag != 2 {
			name = ""
		}
		if page < 1 {
			page = 1
		}
		if num < 1 || num > guildPageMax {
			num = guildPageSize
		}
		rows, total, err := g.Store.GuildsOf(g.SrvID, name, page, num)
		if err != nil {
			return err
		}
		applied, err := g.Store.AppliedGuilds(role.RID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, x := range rows {
			list = append(list, proto.Message{
				"gid": x.GID, "gsrv_id": x.SrvID, "name": x.Name, "lev": x.Lev, "members_num": x.Members, "members_max": guildCapacity(x.Lev),
				"leader_name": x.LeaderName, "apply_type": x.ApplyType, "apply_lev": x.ApplyLev, "apply_power": x.ApplyPower, "is_apply": b2i(applied[x.GID]),
			})
		}
		pages := (total + num - 1) / num
		return s.Send(13501, proto.Message{"page": page, "flag": flag, "num": num, "page_total": pages, "all_count": total, "name": name, "guilds": list})
	})

	// 13503 ask to join (type 1) or take the ask back (type 2)
	r.Handle(13503, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gid, kind := req.Int("gid"), int(req.Int("type"))
		reply := func(ok bool, msg string, applied bool) error {
			return s.Send(13503, proto.Message{"code": b2i(ok), "msg": msg, "gid": gid, "gsrv_id": req.Str("gsrv_id"), "is_apply": b2i(applied)})
		}
		gd, err := g.Store.GuildByID(gid)
		if errors.Is(err, store.ErrNotFound) || (err == nil && gd.SrvID != g.SrvID) {
			return reply(false, "公会不存在", false)
		}
		if err != nil {
			return err
		}
		if kind == 2 {
			if _, err := g.Store.DeleteApply(gid, role.RID); err != nil {
				return err
			}
			return reply(true, "已取消申请", false)
		}
		fresh, err := g.Store.RoleByID(role.RID)
		if err != nil {
			return err
		}
		now := s.Now().Unix()
		if _, _, err := g.Store.GuildOfRole(fresh.RID); err == nil {
			return reply(false, "你已经加入了公会", false)
		}
		if left, err := g.cooldownLeft(fresh.RID, now, false); err != nil {
			return err
		} else if left > 0 {
			return reply(false, "退出公会后需要等待一段时间才能加入其他公会", false)
		}
		switch {
		case int64(fresh.Lev) < g.cfg("guild_min_lev"):
			return reply(false, fmt.Sprintf("等级达到%d级才能加入公会", g.cfg("guild_min_lev")), false)
		case gd.ApplyType == 2:
			return reply(false, "公会不允许加入", false)
		case fresh.Lev < gd.ApplyLev:
			return reply(false, "等级不满足公会的入会要求", false)
		case gd.ApplyPower > 0 && g.guildPower(fresh.RID) < gd.ApplyPower:
			return reply(false, "战力不满足公会的入会要求", false)
		}
		if gd.ApplyType == 0 {
			err := g.Store.JoinGuild(gid, fresh.RID, now, guildCapacity(gd.Lev))
			if errors.Is(err, store.ErrConflict) {
				return reply(false, "公会人数已满", false)
			}
			if err != nil {
				return err
			}
			g.pushLog(gid, guildLogGeneral, fresh.RID, fresh.Name, "加入了公会", now)
			growGuild(g.Store, fresh.RID, gd.Name, gd.LeaderRID, false, now)
			if err := reply(true, "加入公会成功", false); err != nil {
				return err
			}
			g.pushRoleBase(fresh.RID)
			g.pushInfo(gid, dayKey(s.Now()))
			if m, ok := g.memberRow(gid, fresh.RID); ok {
				g.pushMembers(gid, 1, []store.MemberRow{m}, nil)
			}
			return nil
		}
		if n, err := g.Store.MemberCount(gid); err != nil {
			return err
		} else if n >= guildCapacity(gd.Lev) {
			return reply(false, "公会人数已满", false)
		}
		if _, err := g.Store.AddApply(gid, fresh.RID, now); err != nil {
			return err
		}
		if err := reply(true, "申请已发送，请等待公会管理员审批", true); err != nil {
			return err
		}
		g.pushApplyRed(gid)
		return nil
	})

	// 13518 my guild
	r.Handle(13518, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return nil
		}
		if err != nil {
			return err
		}
		msg, err := g.infoMessage(gd, dayKey(s.Now()))
		if err != nil {
			return err
		}
		return s.Send(13518, msg)
	})

	// 13514 leave (members), 13516 disband (the leader)
	r.Handle(13514, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return gcode(s, 13514, false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		if m.Post == store.PostLeader {
			return gcode(s, 13514, false, "会长不能退出公会，请先转让会长或解散公会")
		}
		role := s.Role()
		now := s.Now().Unix()
		if err := g.Store.LeaveGuild(role.RID, now); err != nil {
			return err
		}
		g.pushLog(gd.GID, guildLogGeneral, role.RID, role.Name, "退出了公会", now)
		if err := gcode(s, 13514, true, "已退出公会"); err != nil {
			return err
		}
		g.pushRoleBase(role.RID)
		g.pushInfo(gd.GID, dayKey(s.Now()))
		g.pushMembers(gd.GID, 2, nil, []int64{role.RID})
		return nil
	})
	r.Handle(13516, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return gcode(s, 13516, false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		if m.Post != store.PostLeader {
			return gcode(s, 13516, false, "只有会长才能解散公会")
		}
		rows, err := g.Store.MembersOf(gd.GID)
		if err != nil {
			return err
		}
		if err := g.Store.DisbandGuild(gd.GID, s.Now().Unix()); err != nil {
			return err
		}
		if err := gcode(s, 13516, true, "公会已解散"); err != nil {
			return err
		}
		for _, x := range rows {
			g.pushRoleBase(x.RID)
		}
		return nil
	})
}

// sortedMembers puts the members in the order the list shows.
func sortedMembers(rows []store.MemberRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Post != rows[j].Post {
			return rows[i].Post < rows[j].Post
		}
		return rows[i].Donate > rows[j].Donate
	})
}

var _ = strconv.Itoa
