package game

import (
	"encoding/json"
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const combatMonopolyBoss = 105 // BattleConst.Fight_Type.MonopolyBoss

func monoBossHP(mapID, bossID int64) int64 { return 2_000_000 * mapID * bossID }

// chase is the boss that can still be fought after it is killed, for its drops only.
func monoChase(bossID int64) bool {
	var ids []int64
	_ = json.Unmarshal(monoCfg.Const["monopoly_follow_combat_id"], &ids)
	for _, id := range ids {
		if id == bossID {
			return true
		}
	}
	return false
}

func (mg *monoGuild) bossOf(mapID int64, b monoBossCfg) *monoBoss {
	key := monoKey(mapID) + ":" + monoKey(b.BossID)
	if mg.Boss[key] == nil {
		mg.Boss[key] = &monoBoss{HP: monoBossHP(mapID, b.BossID)}
	}
	return mg.Boss[key]
}

// status is 1 locked by the explore value, 2 open, 3 killed, 4 killed but still fought for the drops.
func (mg *monoGuild) status(mapID int64, b monoBossCfg) int64 {
	switch bs := mg.bossOf(mapID, b); {
	case bs.Killed && monoChase(b.BossID):
		return 4
	case bs.Killed:
		return 3
	case mg.Dev[mapID] < b.Develop:
		return 1
	}
	return 2
}

func (mg *monoGuild) killed() (kill, all int64) {
	for i, bosses := range monoCfg.Bosses {
		for _, b := range bosses {
			all++
			if mg.bossOf(int64(i+1), b).Killed {
				kill++
			}
		}
	}
	return
}

func (mg *monoGuild) damageTotal(mapID int64) int64 {
	var sum int64
	for _, d := range mg.Dmg[monoKey(mapID)] {
		sum += d
	}
	return sum
}

func (m *monoSvc) buffValues(mg *monoGuild) (atk, hp int64) {
	var caps [][]json.RawMessage
	_ = json.Unmarshal(monoCfg.Const["monopoly_buff_max"], &caps)
	atk, hp = mg.Atk*monoAtkUnit, mg.HP*monoHPUnit
	for _, c := range caps {
		var name string
		var limit int64
		if len(c) == 2 && json.Unmarshal(c[0], &name) == nil && json.Unmarshal(c[1], &limit) == nil {
			switch name {
			case "attack":
				atk = min(atk, limit)
			case "hp_max":
				hp = min(hp, limit)
			}
		}
	}
	return
}

func (m *monoSvc) pushGuildBuffs(s *gateway.Session, mg *monoGuild) error {
	atk, hp := m.buffValues(mg)
	return s.Send(27504, proto.Message{"buffs": []proto.Message{{"buff_id": 1, "val": atk}, {"buff_id": 2, "val": hp}}})
}

func (m *monoSvc) bossList(mg *monoGuild, mapID int64) []proto.Message {
	list := []proto.Message{}
	for _, b := range monoCfg.Bosses[mapID-1] {
		bs := mg.bossOf(mapID, b)
		list = append(list, proto.Message{"boss_id": b.BossID, "status": mg.status(mapID, b), "hp": bs.HP, "max_hp": monoBossHP(mapID, b.BossID)})
	}
	return list
}

func (m *monoSvc) pushBoss(s *gateway.Session, mg *monoGuild, mapID int64) error {
	if err := s.Send(27500, proto.Message{"flag": 1, "end_time": monoEnd(s.Now()), "id": mapID, "develop": mg.Dev[mapID], "boss_list": m.bossList(mg, mapID)}); err != nil {
		return err
	}
	kill, all := mg.killed()
	return s.Send(27505, proto.Message{"kill_num": kill, "all_num": all})
}

func (m *monoSvc) registerBoss(r *gateway.Router) {
	r.Handle(27500, func(s *gateway.Session, req proto.Message) error {
		id := req.Int("id")
		role := s.Role()
		if role == nil || id < 1 || id > monoMaps {
			return nil
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		return m.pushBoss(s, mg, id)
	})
	r.Handle(27501, m.fightBoss)
	r.Handle(27502, m.personalRank)
	r.Handle(27503, m.guildRank)
	r.Handle(27504, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		return m.pushGuildBuffs(s, mg)
	})
	r.Handle(27505, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		g, err := m.guildOf(role.RID)
		if err != nil {
			return err
		}
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		kill, all := mg.killed()
		return s.Send(27505, proto.Message{"kill_num": kill, "all_num": all})
	})
}

// fightBoss (27501): a guard boss of a map, fought for damage against the guild's shared hp. The ticket pays each
// challenge; the explore value of the guild opens the boss.
func (m *monoSvc) fightBoss(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	refuse := func(msg string) error { return s.Send(27501, proto.Message{"flag": 0, "msg": msg}) }
	mapID, bossID := req.Int("id"), req.Int("boss_id")
	if mapID < 1 || mapID > monoMaps {
		return refuse("地图不存在")
	}
	var cfg *monoBossCfg
	for i := range monoCfg.Bosses[mapID-1] {
		if monoCfg.Bosses[mapID-1][i].BossID == bossID {
			cfg = &monoCfg.Bosses[mapID-1][i]
		}
	}
	if cfg == nil {
		return refuse("Boss不存在")
	}
	g, err := m.guildOf(role.RID)
	if err != nil {
		return err
	}
	if g == nil {
		return refuse("你还没有加入公会")
	}
	mg, err := m.loadGuild(g)
	if err != nil {
		return err
	}
	if !mg.unlocked(mapID) {
		return refuse("该地图尚未解锁")
	}
	status := mg.status(mapID, *cfg)
	if status == 1 {
		return refuse("探索值不足，Boss尚未出现")
	}
	if status == 3 {
		return refuse("Boss已被击败")
	}
	bs := mg.bossOf(mapID, *cfg)
	var slots []store.FormationSlot
	seen := map[int64]bool{}
	for _, p := range req.Arr("pos_info") {
		if id := p.Int("id"); id != 0 && !seen[id] && p.Int("pos") >= 1 {
			seen[id] = true
			slots = append(slots, store.FormationSlot{Pos: p.Int("pos"), PartnerID: id})
		}
	}
	if len(slots) == 0 {
		return refuse("至少需要上阵一个英雄")
	}
	formType := max(1, req.Int("formation_type"))
	team, objects, err := m.fights.teamFor(role, formType, slots)
	if err != nil {
		return err
	}
	if len(team) == 0 {
		return refuse("没有可以出战的英雄")
	}
	if err := m.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{monoTicket: 1}, map[int64]int64{}, map[int64]int64{}); err != nil {
		return refuse("火祭挑战券不足")
	}
	atkBuff, hpBuff := m.buffValues(mg)
	for i := range team {
		team[i].atk += atkBuff
		team[i].hp += hpBuff
		team[i].maxHP += hpBuff
	}
	chase := status == 4
	fightHP := bs.HP
	if chase {
		fightHP = monoBossHP(mapID, bossID)
	}
	if guildBossCfg.Bosses[0].Monster == nil {
		return refuse("Boss数据缺失")
	}
	unit, obj := monsterOf(*guildBossCfg.Bosses[0].Monster, 0)
	unit.hp, unit.maxHP = fightHP, fightHP
	obj["hp"], obj["hp_max"] = fightHP, fightHP
	rounds, _, sim := playFightOpts(team, []fightUnit{unit}, newRng(), maxFightRounds, true)
	damage := max(0, fightHP-sim.units[len(team)].hp)
	intro := proto.Message{
		"combat_type": combatMonopolyBoss, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
		"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
		"extra_args": []proto.Message{{"param": mapID}}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": cfg.Name,
		"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
	}
	if err := s.Send(27501, proto.Message{"flag": 1, "msg": ""}); err != nil {
		return err
	}
	return m.fights.begin(s, role, combatMonopolyBoss, intro, rounds, damage >= fightHP, func(s *gateway.Session, _ bool) error {
		return m.settleBoss(s, role, g, mapID, *cfg, slots, damage, chase)
	})
}

func (m *monoSvc) settleBoss(s *gateway.Session, role *store.Role, g *store.Guild, mapID int64, cfg monoBossCfg, slots []store.FormationSlot, damage int64, chase bool) error {
	mg, err := m.loadGuild(g)
	if err != nil {
		return err
	}
	bs := mg.bossOf(mapID, cfg)
	prize := pairsOf(cfg.Drop)
	if !chase && !bs.Killed {
		damage = min(damage, bs.HP)
		bs.HP -= damage
		dm := mg.Dmg[monoKey(mapID)]
		if dm == nil {
			dm = map[string]int64{}
			mg.Dmg[monoKey(mapID)] = dm
		}
		dm[monoKey(role.RID)] += damage
		if bs.HP <= 0 {
			bs.Killed = true
			prize = append(prize, pairsOf(cfg.Reward)...)
		}
	}
	if err := m.saveGuild(g, mg); err != nil {
		return err
	}
	if err := m.gain(s, role, prize); err != nil {
		return err
	}
	all, err := m.st.PartnersOf(role.RID)
	if err != nil {
		return err
	}
	byID := map[int64]store.Partner{}
	for _, h := range all {
		byID[h.PartnerID] = h
	}
	var hurts []proto.Message
	var best, bestPower int64
	for _, sl := range slots {
		h, ok := byID[sl.PartnerID]
		if !ok {
			continue
		}
		pw := partnerMessage(h).Int("power")
		hurts = append(hurts, proto.Message{"rid": role.RID, "srvid": role.SrvID, "id": h.PartnerID, "bid": h.BID, "star": h.Star, "lev": h.Lev, "camp_type": partnerTable[h.BID].Camp,
			"dps": damage / int64(len(slots)), "cure": 0, "ext_data": []proto.Message{}, "be_hurt": 0})
		if pw > bestPower {
			best, bestPower = h.BID, pw
		}
	}
	if err := s.Send(27506, proto.Message{"id": mapID, "boss_id": cfg.BossID, "all_dps": damage, "best_partner": best, "award_list": awardList(prize), "target_role_name": cfg.Name,
		"hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": hurts}}}); err != nil {
		return err
	}
	return m.pushBoss(s, mg, mapID)
}

func awardList(items [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, it := range items {
		out = append(out, proto.Message{"bid": it[0], "num": it[1]})
	}
	return out
}

// personalRank (27502): the guild's members by their damage on the map.
func (m *monoSvc) personalRank(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	id := req.Int("id")
	if role == nil || id < 1 || id > monoMaps {
		return nil
	}
	g, err := m.guildOf(role.RID)
	if err != nil {
		return err
	}
	list := []proto.Message{}
	if g != nil {
		mg, err := m.loadGuild(g)
		if err != nil {
			return err
		}
		type row struct{ rid, dmg int64 }
		var rows []row
		for k, d := range mg.Dmg[monoKey(id)] {
			var rid int64
			for _, c := range k {
				rid = rid*10 + int64(c-'0')
			}
			rows = append(rows, row{rid, d})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].dmg > rows[j].dmg || (rows[i].dmg == rows[j].dmg && rows[i].rid < rows[j].rid)
		})
		for i, r := range rows {
			who, err := m.st.RoleByID(r.rid)
			if err != nil {
				continue
			}
			face := int64(who.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			list = append(list, proto.Message{"rid": who.RID, "srv_id": who.SrvID, "name": who.Name, "dps": r.dmg, "lev": who.Lev, "face_id": face, "avatar_bid": 0,
				"power": 0, "rank": i + 1, "worship_num": 0, "worship_status": 0})
		}
	}
	return s.Send(27502, proto.Message{"id": id, "rank_list": list})
}

// guildRank (27503): the guilds of the server by the damage they did on the map.
func (m *monoSvc) guildRank(s *gateway.Session, req proto.Message) error {
	role := s.Role()
	id := req.Int("id")
	if role == nil || id < 1 || id > monoMaps {
		return nil
	}
	guilds, _, err := m.st.GuildsOf(role.SrvID, "", 1, 200)
	if err != nil {
		return err
	}
	type row struct {
		g   store.GuildRow
		dmg int64
	}
	var rows []row
	for _, g := range guilds {
		mg, err := m.loadGuild(&g.Guild)
		if err != nil {
			return err
		}
		if d := mg.damageTotal(id); d > 0 {
			rows = append(rows, row{g, d})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].dmg > rows[j].dmg })
	mine, _ := m.guildOf(role.RID)
	list := []proto.Message{}
	body := proto.Message{"id": id}
	for i, r := range rows {
		entry := proto.Message{"gid": r.g.GID, "srv_id": r.g.SrvID, "name": r.g.Name, "dps": r.dmg, "rank": i + 1, "leader_name": r.g.LeaderName, "member": r.g.Members, "member_max": 50, "lev": r.g.Lev}
		list = append(list, entry)
		if mine != nil && mine.GID == r.g.GID {
			body["r_gid"], body["r_srv_id"], body["r_name"], body["r_dps"], body["r_rank"] = r.g.GID, r.g.SrvID, r.g.Name, r.dmg, i+1
			body["r_leader_name"], body["r_member"], body["r_member_max"], body["r_lev"] = r.g.LeaderName, r.g.Members, 50, r.g.Lev
		}
	}
	body["guild_stage_rank"] = list
	return s.Send(27503, body)
}
