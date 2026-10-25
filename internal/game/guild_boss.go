package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// guild_boss.json: the guild boss rules of the client (tools/extract_guild_boss.py).
//
//go:embed data/guild_boss.json
var guildBossJSON []byte

const combatGuildDun = 9 // BattleConst.Fight_Type.GuildDun

type gbossRow struct {
	BossID   int64 `json:"boss_id"`
	ID       int64
	CombatID int64 `json:"combat_id"`
	HP       int64
	Name     string
	Monster  *monsterDef
}

type gbossChapter struct {
	ID        int64
	Boss      int64
	GuildExp  int64 `json:"guild_exp"`
	Challenge [][]int64
	Kill      [][]int64
}

type gbossRank struct {
	BossID int64 `json:"boss_id"`
	Rank1  int64
	Rank2  int64
	Award  [][]int64
}

type gbossConf struct {
	Const       map[string]int64
	Bosses      []gbossRow
	Chapters    []gbossChapter
	RankRewards []gbossRank `json:"rank_rewards"`
	Buys        []struct {
		Count, VipLev int64
		Expend        []int64
	}
	Buffs []struct{ Lev, AtkPer int64 }

	boss  map[int64]gbossRow
	ranks map[int64][]gbossRank
}

var guildBossCfg = func() *gbossConf {
	c := &gbossConf{}
	if err := json.Unmarshal(guildBossJSON, c); err != nil || len(c.Chapters) == 0 || len(c.Bosses) == 0 || len(c.Const) == 0 {
		panic("game: bad guild_boss.json")
	}
	c.boss, c.ranks = map[int64]gbossRow{}, map[int64][]gbossRank{}
	for _, b := range c.Bosses {
		c.boss[b.BossID] = b
	}
	for _, r := range c.RankRewards {
		c.ranks[r.BossID] = append(c.ranks[r.BossID], r)
	}
	return c
}()

func (c *gbossConf) chapter(fid int64) gbossChapter {
	fid = min(max(fid, 1), int64(len(c.Chapters)))
	return c.Chapters[fid-1]
}

// the hp fields of the protocol are 32 bit: a boss with more hp than that is shown scaled down
func gbossWire(hp, full int64) int64 {
	const top = 1<<32 - 1
	if full <= top {
		return hp
	}
	return hp * top / full
}

func pairsOf(list [][]int64) [][2]int64 {
	out := make([][2]int64, 0, len(list))
	for _, e := range list {
		if len(e) >= 2 {
			out = append(out, [2]int64{e[0], e[1]})
		}
	}
	return out
}

func attachmentsOf(list [][2]int64) []store.Attachment {
	var out []store.Attachment
	for _, e := range guildGains(list) {
		kind := store.AttachItem
		if t, ok := itemTable[e.ID]; ok && t.Type == 7 {
			kind = store.AttachAsset
		}
		out = append(out, store.Attachment{Kind: kind, ItemID: e.ID, Num: e.Amount})
	}
	return out
}

// bossState is the guild's boss as of now: a new day puts it back to full hp on the chapter the reset picks
// (the next one after the highest killed, or with the leader's rollback the highest itself).
func (g *GuildService) bossState(gid int64, now time.Time) (*store.BossState, error) {
	st, found, err := g.Store.BossStateOf(gid)
	if err != nil {
		return nil, err
	}
	day := dayKey(now)
	if found && st.Day == day {
		return st, nil
	}
	switch {
	case !found:
		st.Fid = 1
	case st.ResetType == 1:
		st.Fid = max(1, st.MaxID)
	default:
		st.Fid = min(st.MaxID+1, int64(len(guildBossCfg.Chapters)))
	}
	st.ResetType, st.Day = 0, day
	st.HP = guildBossCfg.boss[guildBossCfg.chapter(st.Fid).Boss].HP
	if st.BuffEnd <= now.Unix() {
		st.BuffLev, st.BuffEnd = 0, 0
	}
	return st, g.Store.SaveBossState(st)
}

// bossCount is how many challenges a role has left today.
func (g *GuildService) bossCount(rid int64, now time.Time) (*store.BossRole, int64, error) {
	r, err := g.Store.BossRoleOf(rid)
	if err != nil {
		return nil, 0, err
	}
	if day := dayKey(now); r.Day != day {
		r.Day, r.Used, r.Bought = day, 0, 0
	}
	return r, max(0, guildBossCfg.Const["free_time"]+r.Bought-r.Used), nil
}

func nextMidnight(now time.Time) int64 { return dayStart(now).AddDate(0, 0, 1).Unix() }

// bossInfo is the 21300 answer for one member.
func (g *GuildService) bossInfo(rid, gid int64, now time.Time) (proto.Message, error) {
	st, err := g.bossState(gid, now)
	if err != nil {
		return nil, err
	}
	role, count, err := g.bossCount(rid, now)
	if err != nil {
		return nil, err
	}
	chap := guildBossCfg.chapter(st.Fid)
	boss := guildBossCfg.boss[chap.Boss]
	last, err := g.Store.BossLastDamage(gid, st.Day, boss.BossID, rid)
	if err != nil {
		return nil, err
	}
	buffLev, buffEnd := st.BuffLev, st.BuffEnd
	if buffEnd <= now.Unix() {
		buffLev, buffEnd = 0, 0
	}
	return proto.Message{"fid": st.Fid, "max_id": st.MaxID, "count": count, "buy_count": role.Bought,
		"info":        []proto.Message{{"boss_id": boss.BossID, "hp": gbossWire(st.HP, boss.HP)}},
		"combat_info": []proto.Message{{"boss_id": boss.BossID, "dps": min(last, 1<<32-1)}},
		"buff_lev":    buffLev, "buff_end_time": buffEnd, "ref_time": nextMidnight(now), "worship": 0, "coldtime": st.MusterAt}, nil
}

func (g *GuildService) pushBossInfo(gid int64, now time.Time) {
	rows, err := g.Store.MembersOf(gid)
	if err != nil {
		return
	}
	for _, m := range rows {
		if sess := g.Hub.Online(m.RID); sess != nil {
			if msg, err := g.bossInfo(m.RID, gid, now); err == nil {
				sess.Send(21300, msg)
			}
		}
	}
}

// bossOutcome is what one challenge (a fight or a mop-up) came to.
type bossOutcome struct {
	damage, applied int64
	killed          bool
	rewards         [][2]int64
}

// bossSettle puts a challenge's damage on the boss and pays: the challenge reward to the member, and when it
// kills the boss, the kill reward (once per member and boss), the guild's exp and the damage ranking by mail.
func (g *GuildService) bossSettle(role *store.Role, gd *store.Guild, st *store.BossState, boss gbossRow, damage int64, now time.Time) (bossOutcome, error) {
	chap := guildBossCfg.chapter(st.Fid)
	applied, _, killed, err := g.Store.BossHit(gd.GID, st.Day, boss.BossID, role.RID, damage)
	if err != nil {
		return bossOutcome{}, err
	}
	out := bossOutcome{damage: damage, applied: applied, killed: killed, rewards: pairsOf(chap.Challenge)}
	if err := g.Store.AddGuildVitality(gd.GID, vitalityBoss); err != nil {
		return out, err
	}
	if killed {
		first, err := g.Store.ClaimBossKill(role.RID, boss.BossID)
		if err != nil {
			return out, err
		}
		if first {
			out.rewards = append(out.rewards, pairsOf(chap.Kill)...)
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(out.rewards), 1, ga, gi)
	if err := g.Store.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return out, err
	}
	if killed {
		if chap.GuildExp > 0 {
			if err := g.Store.AddGuildExp(gd.GID, chap.GuildExp, guildLevelExp()); err != nil {
				return out, err
			}
		}
		if err := g.bossRankMails(gd, st, boss, now); err != nil {
			return out, err
		}
		g.pushLog(gd.GID, guildLogGeneral, role.RID, role.Name, "击败了公会副本的"+boss.Name, now.Unix())
		g.pushInfo(gd.GID, dayKey(now))
	}
	return out, nil
}

// bossRankMails mails the damage ranking rewards of a killed boss to the members who made the list.
func (g *GuildService) bossRankMails(gd *store.Guild, st *store.BossState, boss gbossRow, now time.Time) error {
	list, err := g.Store.BossDamages(gd.GID, st.Day, boss.BossID)
	if err != nil {
		return err
	}
	rows := guildBossCfg.ranks[boss.BossID]
	for i, d := range list {
		if d.Total < guildBossCfg.Const["rank_dam"] {
			continue
		}
		rank := int64(i + 1)
		for _, r := range rows {
			if rank >= r.Rank1 && rank <= r.Rank2 {
				m := store.Mail{SrvID: gd.SrvID, Subject: "公会副本排名奖励", Content: "你在公会副本" + boss.Name + "的伤害排名第" + itoa64(rank) + "，这是排名奖励。",
					FromName: "公会副本", CreatedBy: "guildboss", Attachments: attachmentsOf(pairsOf(r.Award))}
				if _, err := g.Store.CreateMail(m, []int64{d.RID}); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func itoa64(n int64) string { return string(appendInt(nil, n)) }

func appendInt(b []byte, n int64) []byte {
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

// bossResult is the 21309 / 21317 body for a challenge.
func bossResult(st *store.Store, role *store.Role, boss gbossRow, o bossOutcome) (proto.Message, error) {
	stats, bid, _, _, err := damageTable(st, role)
	if err != nil {
		return nil, err
	}
	awards := []proto.Message{}
	for _, r := range o.rewards {
		awards = append(awards, proto.Message{"bid": r[0], "num": r[1]})
	}
	return proto.Message{"all_dps": min(o.damage, 1<<32-1), "best_partner": bid, "award_list": awards, "target_role_name": boss.Name,
		"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}}, nil
}

// registerBoss registers the guild boss: 21300 the boss of the day, 21305 the attack buff, 21308 challenge, 21312 buy challenges, 21317 mop-up, 21318/21319 the rankings, 21323 muster.
func (g *GuildService) registerBoss(r *gateway.Router) {
	fights := fightServiceOf(g.Store)
	rngOf := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	r.Handle(21300, func(s *gateway.Session, _ proto.Message) error {
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
		msg, err := g.bossInfo(s.Role().RID, gd.GID, s.Now())
		if err != nil {
			return err
		}
		return s.Send(21300, msg)
	})
	r.Handle(21303, func(s *gateway.Session, _ proto.Message) error { // the day boxes: the client never asks for them
		if s.Role() == nil {
			return nil
		}
		return s.Send(21303, proto.Message{"box_list": []proto.Message{}})
	})
	r.Handle(21312, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		typ := req.Int("type")
		refuse := func(msg string) error { return s.Send(21312, proto.Message{"code": 0, "msg": msg, "type": typ}) }
		if _, _, err := g.mine(s); errors.Is(err, errNoGuild) {
			return refuse("你还没有加入公会")
		}
		br, count, err := g.bossCount(role.RID, s.Now())
		if err != nil {
			return err
		}
		if count > 0 {
			return refuse("挑战次数为0时可购买，请努力挑战Boss！")
		}
		if int(br.Bought) >= len(guildBossCfg.Buys) {
			return refuse("当前购买次数已到达本日上限")
		}
		price := guildBossCfg.Buys[br.Bought]
		if fresh, err := g.Store.RoleByID(role.RID); err != nil {
			return err
		} else if int64(fresh.VIPLev) < price.VipLev {
			return refuse("VIP等级不足")
		}
		spendA, spendI := g.payFor([][2]int64{{price.Expend[0], price.Expend[1]}})
		if err := g.Store.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足")
		} else if err != nil {
			return err
		}
		br.Bought++
		if err := g.Store.SaveBossRole(br); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		return s.Send(21312, proto.Message{"code": 1, "msg": "购买成功", "count": guildBossCfg.Const["free_time"] + br.Bought - br.Used, "buy_count": br.Bought, "type": typ})
	})
	r.Handle(21305, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(21305, proto.Message{"code": 0, "msg": msg}) }
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return refuse("你还没有加入公会")
		}
		if err != nil {
			return err
		}
		st, err := g.bossState(gd.GID, s.Now())
		if err != nil {
			return err
		}
		lev := st.BuffLev
		if st.BuffEnd <= s.Now().Unix() {
			lev = 0
		}
		if int(lev) >= len(guildBossCfg.Buffs) {
			return refuse("增幅已达最高等级")
		}
		// the booster item first, else gems
		spendA, spendI := map[int64]int64{}, map[int64]int64{guildBossCfg.Const["buff_item"]: 1}
		if err := g.Store.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			spendA, spendI = g.payFor([][2]int64{{3, guildBossCfg.Const["buff_cost"]}})
			if err := g.Store.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
				return refuse("钻石不足")
			} else if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		st.BuffLev, st.BuffEnd = lev+1, nextMidnight(s.Now())
		if err := g.Store.SaveBossState(st); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, g.Store); err != nil {
			return err
		}
		if err := s.Send(21305, proto.Message{"code": 1, "msg": "激活成功"}); err != nil {
			return err
		}
		g.pushBossInfo(gd.GID, s.Now())
		return nil
	})
	r.Handle(21323, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return s.Send(21323, proto.Message{"code": 0, "msg": "你还没有加入公会"})
		}
		if err != nil {
			return err
		}
		if m.Post > store.PostAssistant {
			return s.Send(21323, proto.Message{"code": 0, "msg": "只有会长、副会长可发出集结"})
		}
		st, err := g.bossState(gd.GID, s.Now())
		if err != nil {
			return err
		}
		if st.MusterAt > s.Now().Unix() {
			return s.Send(21323, proto.Message{"code": 0, "msg": "集结冷却中"})
		}
		st.MusterAt = s.Now().Unix() + guildBossCfg.Const["concentrate_cd"]
		if err := g.Store.SaveBossState(st); err != nil {
			return err
		}
		if err := s.Send(21323, proto.Message{"code": 1, "msg": "", "coldtime": st.MusterAt}); err != nil {
			return err
		}
		g.pushBossInfo(gd.GID, s.Now())
		return nil
	})
	// 21308 the fight: the chosen heroes against the boss for at most 15 rounds; the damage is what it loses
	r.Handle(21308, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(21308, proto.Message{"code": 0, "msg": msg}) }
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return refuse("你还没有加入公会")
		}
		if err != nil {
			return err
		}
		if fresh, err := g.Store.RoleByID(role.RID); err != nil {
			return err
		} else if int64(fresh.Lev) < guildBossCfg.Const["join_lev"] {
			return refuse("等级不足，需要达到10级才能参与公会副本")
		}
		st, err := g.bossState(gd.GID, s.Now())
		if err != nil {
			return err
		}
		chap := guildBossCfg.chapter(st.Fid)
		boss := guildBossCfg.boss[chap.Boss]
		switch {
		case req.Int("boss_id") != boss.BossID:
			return refuse("Boss已经变化，请刷新")
		case st.HP <= 0:
			return refuse("Boss已被击败")
		case boss.Monster == nil:
			return refuse("Boss数据缺失")
		}
		br, count, err := g.bossCount(role.RID, s.Now())
		if err != nil {
			return err
		}
		if count <= 0 {
			return refuse("挑战次数不足")
		}
		// the heroes: the places the client sent, else the campaign line-up
		formType := req.Int("formation_type")
		var slots []store.FormationSlot
		seen := map[int64]bool{}
		for _, p := range req.Arr("pos_info") {
			if id := p.Int("id"); id != 0 && !seen[id] && p.Int("pos") >= 1 {
				seen[id] = true
				slots = append(slots, store.FormationSlot{Pos: p.Int("pos"), PartnerID: id})
			}
		}
		var team []fightUnit
		var objects []proto.Message
		if len(slots) > 0 {
			if formType < 1 {
				formType = 1
			}
			team, objects, err = fights.teamFor(role, formType, slots)
		} else {
			var form store.Formation
			team, objects, form, err = fights.teamSide(role)
			formType = form.FormationType
		}
		if err != nil {
			return err
		}
		if len(team) == 0 {
			return refuse("没有可以出战的英雄")
		}
		boost := int64(1000)
		if st.BuffEnd > s.Now().Unix() && st.BuffLev > 0 {
			boost += guildBossCfg.Buffs[st.BuffLev-1].AtkPer
		}
		for i := range team {
			team[i].atk = team[i].atk * boost / 1000
		}
		unit, obj := monsterOf(*boss.Monster, 0)
		fightHP := min(st.HP, 1<<32-1)
		unit.hp, unit.maxHP = fightHP, fightHP
		obj["hp"], obj["hp_max"] = fightHP, fightHP
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, rngOf(), int(guildBossCfg.Const["action_time"]), true)
		damage := max(0, fightHP-sim.units[len(team)].hp)
		br.Used++
		if err := g.Store.SaveBossRole(br); err != nil {
			return err
		}
		intro := proto.Message{
			"combat_type": combatGuildDun, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": boss.Name,
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(21308, proto.Message{"code": 1, "msg": ""}); err != nil {
			return err
		}
		return fights.begin(s, role, combatGuildDun, intro, rounds, damage >= fightHP, func(s *gateway.Session, _ bool) error {
			out, err := g.bossSettle(role, gd, st, boss, damage, s.Now())
			if err != nil {
				return err
			}
			if err := sendAssets(s, g.Store, role.RID); err != nil {
				return err
			}
			body, err := bossResult(g.Store, role, boss, out)
			if err != nil {
				return err
			}
			body["bossid"] = boss.BossID
			if err := s.Send(21309, body); err != nil {
				return err
			}
			g.pushBossInfo(gd.GID, s.Now())
			return nil
		})
	})
	// 21317 mop-up: repeat the damage of the last fight against this boss, for one challenge
	r.Handle(21317, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(21317, proto.Message{"code": 0, "msg": msg}) }
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return refuse("你还没有加入公会")
		}
		if err != nil {
			return err
		}
		st, err := g.bossState(gd.GID, s.Now())
		if err != nil {
			return err
		}
		boss := guildBossCfg.boss[guildBossCfg.chapter(st.Fid).Boss]
		if req.Int("boss_id") != boss.BossID || st.HP <= 0 {
			return refuse("Boss已被击败")
		}
		last, err := g.Store.BossLastDamage(gd.GID, st.Day, boss.BossID, role.RID)
		if err != nil {
			return err
		}
		if last <= 0 {
			return refuse("还没有挑战记录，不能扫荡")
		}
		br, count, err := g.bossCount(role.RID, s.Now())
		if err != nil {
			return err
		}
		if count <= 0 {
			return refuse("挑战次数不足")
		}
		br.Used++
		if err := g.Store.SaveBossRole(br); err != nil {
			return err
		}
		out, err := g.bossSettle(role, gd, st, boss, last, s.Now())
		if err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		body, err := bossResult(g.Store, role, boss, out)
		if err != nil {
			return err
		}
		body["code"], body["msg"], body["do_count"] = 1, "", 1
		if err := s.Send(21317, body); err != nil {
			return err
		}
		g.pushBossInfo(gd.GID, s.Now())
		return nil
	})
	// 21319 the damage ranking of a boss inside the guild
	r.Handle(21319, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return nil
		}
		if err != nil {
			return err
		}
		st, err := g.bossState(gd.GID, s.Now())
		if err != nil {
			return err
		}
		list, err := g.Store.BossDamages(gd.GID, st.Day, req.Int("boss_id"))
		if err != nil {
			return err
		}
		var rank, mine int64
		out := []proto.Message{}
		for i, d := range list {
			if d.Total < guildBossCfg.Const["rank_dam"] {
				continue
			}
			if d.RID == role.RID {
				rank, mine = int64(i+1), d.Total
			}
			n := int64(i + 1)
			if n < req.Int("start_num") || n > req.Int("end_num") {
				continue
			}
			m, ok := g.memberRow(gd.GID, d.RID)
			if !ok {
				continue
			}
			face := int64(m.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			out = append(out, proto.Message{"rank": n, "r_rid": d.RID, "r_srvid": m.SrvID, "name": m.Name, "family_name": gd.Name, "all_dps": min(d.Total, 1<<32-1),
				"face_id": face, "lev": m.Lev, "power": g.guildPower(d.RID), "avatar_bid": 0, "look_id": 0, "face_update_time": 0, "face_file": "", "worship": 0, "worship_status": 0})
		}
		return s.Send(21319, proto.Message{"rank": rank, "mydps": min(mine, 1<<32-1), "worship": 0, "day_worship": 0, "rank_list": out})
	})
	// 21318 the guilds of the server by the highest chapter killed
	r.Handle(21318, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		rows, err := g.Store.BossGuilds(role.SrvID, dayKey(s.Now()))
		if err != nil {
			return err
		}
		mine, _, merr := g.mine(s)
		body := proto.Message{"my_name": "", "my_rank": 0, "my_max_id": 0, "my_dps": 0, "r_rid": 0, "r_srvid": "", "r_name": "", "face_id": 0, "lev": 0, "power": 0, "avatar_bid": 0,
			"rank_guild": []proto.Message{}}
		list := []proto.Message{}
		for i, r := range rows {
			leader, err := g.Store.RoleByID(r.LeaderRID)
			if err != nil {
				continue
			}
			if i == 0 {
				face := int64(leader.FaceID)
				if face == 0 {
					face = store.DefaultFaceID
				}
				body["r_rid"], body["r_srvid"], body["r_name"], body["face_id"], body["lev"], body["power"] = leader.RID, leader.SrvID, leader.Name, face, leader.Lev, g.guildPower(leader.RID)
			}
			if merr == nil && r.GID == mine.GID {
				body["my_name"], body["my_rank"], body["my_max_id"], body["my_dps"] = r.Name, i+1, r.MaxID, min(r.DayTotal, 1<<32-1)
			}
			if i < 20 {
				list = append(list, proto.Message{"name": r.Name, "rank": i + 1, "r_name": leader.Name, "max_id": r.MaxID, "day_dps": min(r.DayTotal, 1<<32-1)})
			}
		}
		body["rank_guild"] = list
		return s.Send(21318, body)
	})
}
