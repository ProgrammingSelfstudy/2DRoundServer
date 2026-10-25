package game

import (
	"errors"
	"fmt"
	"strings"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// impeachGems is the client's impeach_gold, {{15, 300}}: 300 gems.
const impeachGems = 300

func (g *GuildService) manager(s *gateway.Session, cmd uint16) (*store.Guild, *store.GuildMember, bool, error) {
	gd, m, err := g.mine(s)
	if errors.Is(err, errNoGuild) {
		return nil, nil, false, gcode(s, cmd, false, "你还没有加入公会")
	}
	if err != nil {
		return nil, nil, false, err
	}
	if m.Post > store.PostAssistant {
		return nil, nil, false, gcode(s, cmd, false, "你没有权限")
	}
	return gd, m, true, nil
}

func (g *GuildService) registerMembers(r *gateway.Router) {
	// 13519 members
	r.Handle(13519, func(s *gateway.Session, _ proto.Message) error {
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
		rows, err := g.Store.MembersOf(gd.GID)
		if err != nil {
			return err
		}
		list := make([]proto.Message, 0, len(rows))
		for _, m := range rows {
			list = append(list, g.memberMessage(m))
		}
		return s.Send(13519, proto.Message{"members": list})
	})

	// 13507 applicants
	r.Handle(13507, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) || (err == nil && m.Post > store.PostAssistant) {
			return s.Send(13507, proto.Message{"page": 1, "page_total": 1, "num": 0, "guids": []proto.Message{}})
		}
		if err != nil {
			return err
		}
		rows, err := g.Store.AppliesOf(gd.GID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, a := range rows {
			face := a.FaceID
			if face == 0 {
				face = store.DefaultFaceID
			}
			list = append(list, proto.Message{
				"rid": a.RID, "srv_id": a.SrvID, "name": a.Name, "lev": a.Lev, "face": face, "power": g.guildPower(a.RID), "vip_lev": a.VIPLev,
				"is_online": b2i(g.Hub.Online(a.RID) != nil), "face_update_time": 0, "face_file": "",
			})
		}
		return s.Send(13507, proto.Message{"page": 1, "page_total": 1, "num": len(list), "guids": list})
	})
	r.Handle(13573, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) || (err == nil && m.Post > store.PostAssistant) {
			return s.Send(13573, proto.Message{"code": 0})
		}
		if err != nil {
			return err
		}
		n, err := g.Store.ApplyCount(gd.GID)
		if err != nil {
			return err
		}
		return s.Send(13573, proto.Message{"code": b2i(n > 0)})
	})

	// 13505 answer an application: type 1 accept, 2 turn down
	r.Handle(13505, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		kind, rid, srv := int(req.Int("type")), req.Int("rid"), req.Str("srv_id")
		reply := func(ok bool, msg string) error {
			return s.Send(13505, proto.Message{"type": kind, "rid": rid, "srv_id": srv, "code": b2i(ok), "msg": msg})
		}
		if _, me, err := g.mine(s); err != nil || me.Post > store.PostAssistant {
			if err != nil && !errors.Is(err, errNoGuild) {
				return err
			}
			return reply(false, "你没有权限")
		}
		return g.answerApply(s, kind, rid, reply)
	})

	// 13513 kick
	r.Handle(13513, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		rid, srv := req.Int("rid"), req.Str("srv_id")
		reply := func(ok bool, msg string) error {
			return s.Send(13513, proto.Message{"rid": rid, "srv_id": srv, "code": b2i(ok), "msg": msg})
		}
		gd, me, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return reply(false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		target, err := g.Store.MemberOf(rid)
		switch {
		case me.Post > store.PostAssistant:
			return reply(false, "你没有权限")
		case rid == role.RID:
			return reply(false, "不能移除自己")
		case errors.Is(err, store.ErrNotFound) || (err == nil && target.GID != gd.GID):
			return reply(false, "该玩家不在公会中")
		case err != nil:
			return err
		case target.Post <= me.Post:
			return reply(false, "不能移除职位不低于自己的成员")
		}
		now := s.Now().Unix()
		err = g.Store.KickMember(gd.GID, rid, now, dayKey(s.Now()), int(g.cfg("remove_limit")))
		if errors.Is(err, store.ErrConflict) {
			return reply(false, "今日移除成员次数已达上限")
		}
		if err != nil {
			return err
		}
		name := ""
		if t, err := g.Store.RoleByID(rid); err == nil {
			name = t.Name
		}
		g.pushLog(gd.GID, guildLogGeneral, rid, name, "被移出了公会", now)
		if err := reply(true, "已将玩家移出公会"); err != nil {
			return err
		}
		g.pushRoleBase(rid)
		g.pushInfo(gd.GID, dayKey(s.Now()))
		g.pushMembers(gd.GID, 2, nil, []int64{rid})
		return nil
	})

	// 13520 post: 2 assistant, 3 member, 1 hand over the guild
	r.Handle(13520, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		rid, srv, pos := req.Int("rid"), req.Str("srv_id"), int(req.Int("position"))
		reply := func(ok bool, msg string) error {
			return s.Send(13520, proto.Message{"rid": rid, "srv_id": srv, "position": pos, "code": b2i(ok), "msg": msg})
		}
		gd, me, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return reply(false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		target, err := g.Store.MemberOf(rid)
		switch {
		case me.Post != store.PostLeader:
			return reply(false, "只有会长才能任命职位")
		case rid == role.RID:
			return reply(false, "不能对自己操作")
		case errors.Is(err, store.ErrNotFound) || (err == nil && target.GID != gd.GID):
			return reply(false, "该玩家不在公会中")
		case err != nil:
			return err
		case pos < store.PostLeader || pos > store.PostMember:
			return reply(false, "参数错误")
		}
		now := s.Now().Unix()
		tname := ""
		if t, err := g.Store.RoleByID(rid); err == nil {
			tname = t.Name
		}
		switch pos {
		case store.PostLeader:
			if h := now - gd.CreatedAt; h < g.cfg("transfer_time_condition")*3600 {
				return reply(false, fmt.Sprintf("公会创建满%d小时后才能转让会长", g.cfg("transfer_time_condition")))
			}
			if err := g.Store.TransferLeader(gd.GID, role.RID, rid); err != nil {
				return err
			}
			g.pushLog(gd.GID, guildLogGeneral, rid, tname, "成为了新的会长", now)
		case store.PostAssistant:
			if target.Post == store.PostAssistant {
				return reply(false, "该玩家已经是副会长")
			}
			n, err := g.Store.AssistantCount(gd.GID)
			if err != nil {
				return err
			}
			if limit, ok := guildCfg.Assistants[fmt.Sprintf("2_%d", gd.Lev)]; ok && n >= limit {
				return reply(false, "副会长人数已达上限")
			}
			if err := g.Store.SetMemberPost(gd.GID, rid, pos); err != nil {
				return err
			}
			g.pushLog(gd.GID, guildLogGeneral, rid, tname, "被任命为副会长", now)
		default:
			if target.Post != store.PostAssistant {
				return reply(false, "该玩家不是副会长")
			}
			if err := g.Store.SetMemberPost(gd.GID, rid, pos); err != nil {
				return err
			}
			g.pushLog(gd.GID, guildLogGeneral, rid, tname, "被免去了副会长职务", now)
		}
		if err := reply(true, "操作成功"); err != nil {
			return err
		}
		g.pushRoleBase(rid)
		g.pushRoleBase(role.RID)
		g.pushInfo(gd.GID, dayKey(s.Now()))
		var changed []store.MemberRow
		for _, id := range []int64{rid, role.RID} {
			if m, ok := g.memberRow(gd.GID, id); ok {
				changed = append(changed, m)
			}
		}
		g.pushMembers(gd.GID, 1, changed, nil)
		return nil
	})

	// 13521 declaration, 13522 who may join
	r.Handle(13521, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, _, ok, err := g.manager(s, 13521)
		if !ok {
			return err
		}
		sign := strings.TrimSpace(req.Str("sign"))
		if !validGuildSign(sign) {
			return gcode(s, 13521, false, "公会宣言过长或含有非法字符")
		}
		if err := g.Store.SetGuildSign(gd.GID, sign); err != nil {
			return err
		}
		if err := gcode(s, 13521, true, "修改成功"); err != nil {
			return err
		}
		g.pushInfo(gd.GID, dayKey(s.Now()))
		return nil
	})
	r.Handle(13522, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, _, ok, err := g.manager(s, 13522)
		if !ok {
			return err
		}
		t, lev, power := int(req.Int("apply_type")), int(req.Int("apply_lev")), req.Int("apply_power")
		if t < 0 || t > 1 || lev < 1 || lev > 200 || power < 0 {
			return gcode(s, 13522, false, "参数错误")
		}
		if err := g.Store.SetGuildApply(gd.GID, t, lev, power); err != nil {
			return err
		}
		if err := gcode(s, 13522, true, "设置成功"); err != nil {
			return err
		}
		g.pushInfo(gd.GID, dayKey(s.Now()))
		return nil
	})

	// 13568 rename (the leader, for gems, once in a while)
	r.Handle(13568, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, me, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return gcode(s, 13568, false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		name := strings.TrimSpace(req.Str("name"))
		now := s.Now().Unix()
		switch {
		case me.Post != store.PostLeader:
			return gcode(s, 13568, false, "只有会长才能修改公会名称")
		case !validGuildName(name):
			return gcode(s, 13568, false, "公会名称需为2-6个汉字、字母或数字")
		case name == gd.Name:
			return gcode(s, 13568, false, "新名称与原名称相同")
		case gd.RenamedAt > 0 && now-gd.RenamedAt < g.cfg("rename_time")*3600:
			return gcode(s, 13568, false, "公会改名冷却中")
		}
		assets, items := g.gemCost("rename_gold")
		switch err := g.Store.RenameGuild(gd.GID, role.RID, name, now, assets, items); {
		case errors.Is(err, store.ErrNameTaken):
			return gcode(s, 13568, false, "公会名称已被使用")
		case errors.Is(err, store.ErrConflict):
			return gcode(s, 13568, false, "钻石不足")
		case err != nil:
			return err
		}
		g.pushLog(gd.GID, guildLogGeneral, role.RID, role.Name, "将公会改名为"+name, now)
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := gcode(s, 13568, true, "修改成功"); err != nil {
			return err
		}
		rows, _ := g.Store.MembersOf(gd.GID)
		for _, m := range rows {
			g.pushRoleBase(m.RID)
		}
		g.pushInfo(gd.GID, dayKey(s.Now()))
		return nil
	})

	// 13558 recruit: the first announcement of the day is free, the next ones cost gems
	r.Handle(13558, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, ok, err := g.manager(s, 13558)
		if !ok {
			return err
		}
		day := dayKey(s.Now())
		used := gd.RecruitNum
		if int64(gd.RecruitDay) != day {
			used = 0
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		if used > 0 {
			assets, items = g.gemCost("recruit_cost")
		}
		switch _, err := g.Store.CountRecruit(gd.GID, role.RID, day, int(g.cfg("recruit_limit")), assets, items); {
		case errors.Is(err, store.ErrConflict):
			if used >= int(g.cfg("recruit_limit")) {
				return gcode(s, 13558, false, "当天招募次数已用完")
			}
			return gcode(s, 13558, false, "钻石不足")
		case err != nil:
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := gcode(s, 13558, true, "招募公告已发布"); err != nil {
			return err
		}
		g.pushInfo(gd.GID, day)
		return nil
	})

	// 13565 impeach a leader that has stayed away
	r.Handle(13565, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, me, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return gcode(s, 13565, false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		leader, err := g.Store.RoleByID(gd.LeaderRID)
		if err != nil {
			return err
		}
		now := s.Now().Unix()
		days := g.cfg("impeach_offline_day2")
		if me.Post == store.PostAssistant {
			days = g.cfg("impeach_offline_day1")
		}
		switch {
		case me.Post == store.PostLeader:
			return gcode(s, 13565, false, "你就是会长")
		case g.Hub.Online(leader.RID) != nil || leader.LastLogoutAt == 0 || now-leader.LastLogoutAt < days*86400:
			return gcode(s, 13565, false, "会长离线时间不足，无法弹劾")
		}
		assets, items := g.payFor([][2]int64{{3, impeachGems}})
		if err := g.Store.Impeach(gd.GID, leader.RID, role.RID, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return gcode(s, 13565, false, "钻石不足")
			}
			return err
		}
		g.pushLog(gd.GID, guildLogGeneral, role.RID, role.Name, "弹劾会长成功，成为了新的会长", now)
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := gcode(s, 13565, true, "弹劾成功"); err != nil {
			return err
		}
		g.pushRoleBase(role.RID)
		g.pushRoleBase(leader.RID)
		g.pushInfo(gd.GID, dayKey(s.Now()))
		var changed []store.MemberRow
		for _, id := range []int64{role.RID, leader.RID} {
			if m, ok := g.memberRow(gd.GID, id); ok {
				changed = append(changed, m)
			}
		}
		g.pushMembers(gd.GID, 1, changed, nil)
		return nil
	})

	// 13576 welcome a new member, 13577 the log, 13579 remind members, 13580 mail all members
	r.Handle(13576, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		if _, _, err := g.mine(s); err != nil {
			if errors.Is(err, errNoGuild) {
				return gcode(s, 13576, false, "你还没有加入公会")
			}
			return err
		}
		return gcode(s, 13576, true, "欢迎消息已发送")
	})
	r.Handle(13577, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return s.Send(13577, proto.Message{"guild_log_info_list": []proto.Message{}})
		}
		if err != nil {
			return err
		}
		logs, err := g.Store.GuildLogs(gd.GID, 100)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, l := range logs {
			list = append(list, proto.Message{"id": l.ID % 256, "gid": gd.GID, "gsrv_id": gd.SrvID, "type": l.Type, "time": l.Time, "rid": l.RID, "srv_id": g.SrvID, "role_name": l.RoleName, "msg": l.Msg})
		}
		return s.Send(13577, proto.Message{"guild_log_info_list": list})
	})
	r.Handle(13579, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(13579, proto.Message{"msg": "已提醒成员"})
	})
	r.Handle(13580, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return gcode(s, 13580, false, "暂未开放公会邮件")
	})

	// the guild goal (16900-16904) needs its own tables; until then it reads as an empty goal
	r.Handle(16900, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(16900, proto.Message{"lev": 1, "exp": 0, "day_exp": 0, "week_exp": 0})
	})
	r.Handle(16901, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(16901, proto.Message{"list": []proto.Message{}})
	})
	r.Handle(16903, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return gcode(s, 16903, false, "暂未开放")
	})
	r.Handle(16904, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return gcode(s, 16904, false, "暂未开放")
	})
}

// answerApply accepts (1) or turns down (2) one applicant of the session's guild.
func (g *GuildService) answerApply(s *gateway.Session, kind int, rid int64, reply func(bool, string) error) error {
	gd, _, err := g.mine(s)
	if err != nil {
		return reply(false, "你还没有加入公会")
	}
	if kind != 1 && kind != 2 {
		return reply(false, "参数错误")
	}
	had, err := g.Store.DeleteApply(gd.GID, rid)
	if err != nil {
		return err
	}
	if !had {
		return reply(false, "该申请已不存在")
	}
	if kind == 2 {
		return reply(true, "已拒绝申请")
	}
	applicant, err := g.Store.RoleByID(rid)
	if err != nil {
		return err
	}
	now := s.Now().Unix()
	if left, err := g.cooldownLeft(rid, now, false); err != nil {
		return err
	} else if left > 0 {
		return reply(false, "该玩家刚退出公会，暂时不能加入")
	}
	err = g.Store.JoinGuild(gd.GID, rid, now, guildCapacity(gd.Lev))
	switch {
	case errors.Is(err, store.ErrConflict):
		return reply(false, "公会人数已满，或该玩家已加入其他公会")
	case err != nil:
		return err
	}
	g.pushLog(gd.GID, guildLogGeneral, rid, applicant.Name, "加入了公会", now)
	if err := reply(true, "已同意申请"); err != nil {
		return err
	}
	g.pushRoleBase(rid)
	g.pushInfo(gd.GID, dayKey(s.Now()))
	if m, ok := g.memberRow(gd.GID, rid); ok {
		g.pushMembers(gd.GID, 1, []store.MemberRow{m}, nil)
	}
	return nil
}

func (g *GuildService) registerDonate(r *gateway.Router) {
	boxesFor := func(rid, day int64) ([]proto.Message, error) {
		claimed, err := g.Store.ClaimedBoxes(rid, day)
		if err != nil {
			return nil, err
		}
		out := []proto.Message{}
		for _, b := range claimed {
			out = append(out, proto.Message{"box_id": b})
		}
		return out, nil
	}
	// 13523 donation state
	send := func(s *gateway.Session) error {
		role := s.Role()
		gd, m, err := g.mine(s)
		if err != nil {
			return nil
		}
		day := dayKey(s.Now())
		list := []proto.Message{}
		if m.DonateDay == day {
			list = append(list, proto.Message{"type": m.DonateType, "num": 1})
		}
		boxes, err := boxesFor(role.RID, day)
		if err != nil {
			return err
		}
		exp, err := g.Store.GuildDayExp(gd.GID, day)
		if err != nil {
			return err
		}
		return s.Send(13523, proto.Message{"donate_list": list, "boxes": boxes, "donate_exp": exp, "day_send_num": 0, "day_recv_num": 0})
	}
	r.Handle(13523, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return send(s)
	})
	// 13524 donate: once a day, from the three kinds
	r.Handle(13524, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return gcode(s, 13524, false, "你还没有加入公会")
		}
		if err != nil {
			return err
		}
		kind := int(req.Int("type"))
		if kind < 1 || kind > len(guildCfg.Donate) {
			return gcode(s, 13524, false, "参数错误")
		}
		d := guildCfg.Donate[kind-1]
		spendA, spendI := g.payFor(d.Loss)
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		var guildExp int64
		for _, e := range d.Gain {
			if e[0] == 16 {
				guildExp += e[1]
			}
		}
		splitGains(guildGains(d.Gain), 1, gainA, gainI)
		day := dayKey(s.Now())
		res, err := g.Store.Donate(role.RID, day, kind, spendA, spendI, gainA, gainI, guildExp, guildLevelExp())
		switch {
		case errors.Is(err, store.ErrConflict):
			return gcode(s, 13524, false, "今天已经捐献过了，或材料不足")
		case err != nil:
			return err
		}
		g.pushLog(gd.GID, guildLogDonate, role.RID, role.Name, "进行了"+d.Name, s.Now().Unix())
		if err := g.Store.AddGuildVitality(gd.GID, vitalityDonate); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := gcode(s, 13524, true, "捐献成功"); err != nil {
			return err
		}
		if err := send(s); err != nil {
			return err
		}
		g.pushInfo(gd.GID, day)
		rows, _ := g.Store.MembersOf(gd.GID)
		for _, m := range rows {
			if sess := g.Hub.Online(m.RID); sess != nil {
				sess.Send(13575, proto.Message{"donate_exp": res.DayExp})
			}
		}
		return nil
	})
	// 13574 take a donation box
	r.Handle(13574, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := int(req.Int("box_id"))
		reply := func(ok bool, msg string) error {
			return s.Send(13574, proto.Message{"code": b2i(ok), "msg": msg, "box_id": id})
		}
		if _, _, err := g.mine(s); err != nil {
			if errors.Is(err, errNoGuild) {
				return reply(false, "你还没有加入公会")
			}
			return err
		}
		if id < 1 || id > len(guildCfg.Boxes) {
			return reply(false, "参数错误")
		}
		b := guildCfg.Boxes[id-1]
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains(b.Rewards), 1, gainA, gainI)
		switch err := g.Store.ClaimDonateBox(role.RID, dayKey(s.Now()), id, b.Val, gainA, gainI); {
		case errors.Is(err, store.ErrConflict):
			return reply(false, "尚未达到领取条件，或已领取")
		case err != nil:
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, g.Store); err != nil {
			return err
		}
		return reply(true, "领取成功")
	})
}
