package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// guild_secret.json: the guild secret area rules of the client (tools/extract_guild_secret.py).
//
//go:embed data/guild_secret.json
var guildSecretJSON []byte

const combatGuildSecret = 34 // BattleConst.Fight_Type.GuildSecretArea

type gsecretBoss struct {
	ID         int64
	PassID     int `json:"pass_id"`
	Name       string
	OpenLev    int64 `json:"open_lev"`
	Difficulty string
	Time       int64
	Active     int64
	HP         [2]int64
	Challenge  [][]int64
}

type gsecretConf struct {
	Free, Buy     int64
	Init          []int64
	ResetInterval int64 `json:"reset_interval"`
	BossInterval  int64 `json:"boss_interval"`
	Cost          [][]int64
	Bosses        []gsecretBoss
	Boxes         map[string][]struct {
		Number, Progress int64
		Reward           [][]int64
	}
	Ranks map[string][]struct {
		Min, Max int64
		Reward   [][]int64
	}

	boss map[int64]gsecretBoss
}

var guildSecretCfg = func() *gsecretConf {
	c := &gsecretConf{}
	if err := json.Unmarshal(guildSecretJSON, c); err != nil || len(c.Bosses) == 0 || len(c.Init) == 0 || len(c.Cost) == 0 {
		panic("game: bad guild_secret.json")
	}
	c.boss = map[int64]gsecretBoss{}
	for _, b := range c.Bosses {
		c.boss[b.ID] = b
	}
	return c
}()

// The vitality a guild earns (our rule; the client only says that opening a crusade costs it).
const (
	vitalityDonate = 20
	vitalityBoss   = 10
	vitalitySecret = 5
)

// secretLines is the boss each of the five lines is at.
func secretLines(st *store.SecretState) map[int]int64 {
	lines := map[int]int64{}
	var saved map[string]int64
	_ = json.Unmarshal([]byte(st.Lines), &saved)
	for i, id := range guildSecretCfg.Init {
		lines[i+1] = id
	}
	for k, v := range saved {
		if n, err := strconv.Atoi(k); err == nil && guildSecretCfg.boss[v].ID != 0 {
			lines[n] = v
		}
	}
	return lines
}

func saveLines(st *store.SecretState, lines map[int]int64) {
	m := map[string]int64{}
	for k, v := range lines {
		m[strconv.Itoa(k)] = v
	}
	raw, _ := json.Marshal(m)
	st.Lines = string(raw)
}

// secretStand is the monster a secret boss fights as: the client keeps the bosses' units on the server, so
// the first guild boss's monster stands in under the secret boss's name.
func secretStand(b gsecretBoss) *monsterDef {
	base := guildBossCfg.boss[guildBossCfg.Chapters[0].Boss].Monster
	if base == nil {
		return nil
	}
	m := *base
	m.Name = b.Name
	return &m
}

func rewardOf(list [][]int64) [][2]int64 { return pairsOf(list) }

// rankReward is the damage ranking reward of a place for a boss.
func secretRankReward(boss int64, rank int64) [][2]int64 {
	for _, r := range guildSecretCfg.Ranks[strconv.FormatInt(boss, 10)] {
		if rank >= r.Min && rank <= r.Max {
			return rewardOf(r.Reward)
		}
	}
	return nil
}

// secretState brings the guild's secret area up to date: a crusade past its end is settled (the boss's line goes up
// or down a difficulty and the members get their ranking rewards), and once the cooldown is over the guild may open
// another one.
func (g *GuildService) secretState(gd *store.Guild, now time.Time) (*store.SecretState, error) {
	st, err := g.Store.SecretStateOf(gd.GID)
	if err != nil {
		return nil, err
	}
	changed := false
	if st.Bid != 0 && !st.Settled && now.Unix() >= st.EndTime {
		if err := g.secretSettle(gd, st); err != nil {
			return nil, err
		}
		changed = true
	}
	if st.Bid != 0 && now.Unix() >= st.StartTime+guildSecretCfg.BossInterval {
		st.Bid, st.HP, st.MaxHP = 0, 0, 0
		changed = true
	}
	if changed {
		return st, g.Store.SaveSecretState(st)
	}
	return st, nil
}

func (g *GuildService) secretSettle(gd *store.Guild, st *store.SecretState) error {
	boss := guildSecretCfg.boss[st.Bid]
	st.Settled, st.Success = true, st.HP == 0
	lines := secretLines(st)
	if next, ok := guildSecretCfg.boss[st.Bid+1]; st.Success && ok && next.PassID == boss.PassID {
		lines[boss.PassID] = next.ID
	} else if prev, ok := guildSecretCfg.boss[st.Bid-1]; !st.Success && ok && prev.PassID == boss.PassID {
		lines[boss.PassID] = prev.ID
	}
	saveLines(st, lines)
	list, err := g.Store.SecretDamages(gd.GID, st.Seq)
	if err != nil {
		return err
	}
	for i, d := range list {
		reward := secretRankReward(st.Bid, int64(i+1))
		if len(reward) == 0 {
			continue
		}
		m := store.Mail{SrvID: gd.SrvID, Subject: "公会秘境排名奖励", Content: "你在公会秘境讨伐" + boss.Name + "的伤害排名第" + itoa64(int64(i+1)) + "，这是排名奖励。",
			FromName: "公会秘境", CreatedBy: "guildsecret", Attachments: attachmentsOf(reward)}
		if _, err := g.Store.CreateMail(m, []int64{d.RID}); err != nil {
			return err
		}
	}
	return nil
}

// secretCount brings a member's challenges up to date and says how many are left: a new crusade gives a fresh
// set of free challenges, but not within the reset cool-down of the last one.
func (g *GuildService) secretCount(rid int64, st *store.SecretState, now time.Time) (*store.SecretRole, int64, error) {
	r, err := g.Store.SecretRoleOf(rid)
	if err != nil {
		return nil, 0, err
	}
	if r.Seq != st.Seq {
		r.Seq = st.Seq
		if r.ResetAt == 0 || now.Unix() >= r.ResetAt+guildSecretCfg.ResetInterval {
			r.Used, r.Bought, r.ResetAt = 0, 0, now.Unix()
		}
		if err := g.Store.SaveSecretRole(r); err != nil {
			return nil, 0, err
		}
	}
	return r, max(0, guildSecretCfg.Free+r.Bought-r.Used), nil
}

func (g *GuildService) secretInfo(gd *store.Guild, m store.GuildMember, now time.Time) (proto.Message, error) {
	st, err := g.secretState(gd, now)
	if err != nil {
		return nil, err
	}
	r, count, err := g.secretCount(m.RID, st, now)
	if err != nil {
		return nil, err
	}
	lines := secretLines(st)
	list := []proto.Message{}
	for i := 1; i <= len(guildSecretCfg.Init); i++ {
		list = append(list, proto.Message{"boss_id": lines[i]})
	}
	claimed, err := g.Store.SecretClaimed(gd.GID, st.Seq, m.RID)
	if err != nil {
		return nil, err
	}
	done := []proto.Message{}
	for _, n := range claimed {
		done = append(done, proto.Message{"order": n})
	}
	dmg, err := g.Store.SecretDamages(gd.GID, st.Seq)
	if err != nil {
		return nil, err
	}
	var mine int64
	for _, d := range dmg {
		if d.RID == m.RID {
			mine = d.Last
		}
	}
	return proto.Message{"count": count, "last_buy_time": r.LastBuy, "loss_list": []proto.Message{{"loss_id": guildSecretCfg.Cost[0][0], "num": guildSecretCfg.Cost[0][1]}},
		"bid": st.Bid, "hp": st.HP, "max_hp": st.MaxHP, "end_time": st.EndTime, "count_time": r.ResetAt + guildSecretCfg.ResetInterval, "boss_list": list,
		"progress_reward": done, "dps": min(mine, 1<<32-1), "button": b2i(m.Post <= store.PostAssistant), "is_reward": b2i(mine > 0 && st.Bid != 0)}, nil
}

func (g *GuildService) pushSecretInfo(gd *store.Guild, now time.Time) {
	rows, err := g.Store.MembersOf(gd.GID)
	if err != nil {
		return
	}
	for _, m := range rows {
		if sess := g.Hub.Online(m.RID); sess != nil {
			if msg, err := g.secretInfo(gd, m.GuildMember, now); err == nil {
				sess.Send(26800, msg)
			}
		}
	}
}

// secretStrike puts a challenge's damage on the boss and pays its reward; the member gets the result (26809).
func (g *GuildService) secretStrike(s *gateway.Session, gd *store.Guild, st *store.SecretState, damage int64) error {
	role := s.Role()
	boss := guildSecretCfg.boss[st.Bid]
	if _, _, _, err := g.Store.SecretHit(gd.GID, st.Seq, role.RID, damage, s.Now().Unix()); err != nil {
		return err
	}
	reward := rewardOf(boss.Challenge)
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(reward), 1, ga, gi)
	if err := g.Store.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
		return err
	}
	if err := g.Store.AddGuildVitality(gd.GID, vitalitySecret); err != nil {
		return err
	}
	if err := sendAssets(s, g.Store, role.RID); err != nil {
		return err
	}
	stats, _, _, _, err := damageTable(g.Store, role)
	if err != nil {
		return err
	}
	awards := []proto.Message{}
	for _, r := range reward {
		awards = append(awards, proto.Message{"bid": r[0], "num": r[1]})
	}
	if err := s.Send(26809, proto.Message{"result": 1, "reward": awards, "hurt_statistics": []proto.Message{{"type": 1, "partner_hurts": stats}}}); err != nil {
		return err
	}
	g.pushSecretInfo(gd, s.Now())
	return nil
}

// registerSecret registers the guild secret area: 26800 the state, 26801 open a crusade, 26802 challenge,
// 26803 buy challenges, 26804 a stage chest, 26805 mop-up / pursuit, 26806 damage ranking, 26807 the start
// notice, 26808 the end report, 26809 the result of a challenge, 26810 the guild's vitality.
func (g *GuildService) registerSecret(r *gateway.Router) {
	fights := fightServiceOf(g.Store)
	rngOf := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	// mine loads the member's guild and place
	mine := func(s *gateway.Session) (*store.Guild, *store.GuildMember, bool, error) {
		if s.Role() == nil {
			return nil, nil, false, nil
		}
		gd, m, err := g.mine(s)
		if errors.Is(err, errNoGuild) {
			return nil, nil, false, nil
		}
		return gd, m, err == nil, err
	}
	r.Handle(26800, func(s *gateway.Session, _ proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		msg, err := g.secretInfo(gd, *m, s.Now())
		if err != nil {
			return err
		}
		return s.Send(26800, msg)
	})
	r.Handle(26810, func(s *gateway.Session, _ proto.Message) error {
		gd, _, ok, err := mine(s)
		if !ok {
			return err
		}
		v, err := g.Store.GuildVitality(gd.GID)
		if err != nil {
			return err
		}
		return s.Send(26810, proto.Message{"vitality": v})
	})
	r.Handle(26801, func(s *gateway.Session, req proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		refuse := func(msg string) error { return s.Send(26801, proto.Message{"flag": 0, "msg": msg}) }
		if m.Post > store.PostAssistant {
			return refuse("需要会长或者副会长消耗公会活跃开启")
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		lines := secretLines(st)
		boss, valid := guildSecretCfg.boss[req.Int("boss_id")]
		switch {
		case !valid || lines[boss.PassID] != boss.ID:
			return refuse("该领主当前不可讨伐")
		case st.Bid != 0:
			return refuse("讨伐的冷却时间还没有结束")
		case gd.Lev < 1:
			return refuse("公会等级不足")
		}
		vit, err := g.Store.GuildVitality(gd.GID)
		if err != nil {
			return err
		}
		if vit < boss.Active {
			return refuse("所需公会活跃不足")
		}
		st.Vitality = vit - boss.Active
		st.Seq++
		st.Bid = boss.ID
		span := boss.HP[1] - boss.HP[0]
		st.MaxHP = boss.HP[0]
		if span > 0 {
			st.MaxHP += rngOf().Int63n(span + 1)
		}
		st.HP = st.MaxHP
		st.StartTime, st.EndTime, st.KillTime = s.Now().Unix(), s.Now().Unix()+boss.Time, 0
		st.Settled, st.Success = false, false
		if err := g.Store.SaveSecretState(st); err != nil {
			return err
		}
		if err := s.Send(26801, proto.Message{"flag": 1, "msg": "开始讨伐"}); err != nil {
			return err
		}
		g.pushLog(gd.GID, guildLogGeneral, s.Role().RID, s.Role().Name, "开启了公会秘境讨伐"+boss.Name, s.Now().Unix())
		g.pushSecretInfo(gd, s.Now())
		return nil
	})
	r.Handle(26807, func(s *gateway.Session, _ proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		role, _, err := g.secretCount(m.RID, st, s.Now())
		if err != nil {
			return err
		}
		flag := int64(1)
		if st.Bid != 0 && !st.Settled && role.SeenSeq != st.Seq {
			flag = 0
			role.SeenSeq = st.Seq
			if err := g.Store.SaveSecretRole(role); err != nil {
				return err
			}
		}
		return s.Send(26807, proto.Message{"flag": flag, "end_time": st.EndTime})
	})
	r.Handle(26808, func(s *gateway.Session, _ proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		body := proto.Message{"flag": b2i(st.Success), "time": 0, "market_reward": []proto.Message{}, "boss_reward": []proto.Message{}}
		if st.Settled && st.Bid != 0 {
			if st.Success && st.KillTime > st.StartTime {
				body["time"] = st.KillTime - st.StartTime
			}
			list, err := g.Store.SecretDamages(gd.GID, st.Seq)
			if err != nil {
				return err
			}
			for i, d := range list {
				if d.RID == m.RID {
					rew := []proto.Message{}
					for _, e := range secretRankReward(st.Bid, int64(i+1)) {
						rew = append(rew, proto.Message{"bid": e[0], "num": e[1]})
					}
					body["boss_reward"] = rew
				}
			}
		}
		return s.Send(26808, body)
	})
	r.Handle(26803, func(s *gateway.Session, req proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		refuse := func(msg string) error { return s.Send(26803, proto.Message{"flag": 0, "msg": msg}) }
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		if st.Bid == 0 || st.Settled || req.Int("boss_id") != st.Bid {
			return refuse("当前没有可以购买次数的讨伐")
		}
		role, count, err := g.secretCount(m.RID, st, s.Now())
		if err != nil {
			return err
		}
		if count > 0 {
			return refuse("还有挑战次数")
		}
		if role.Bought >= guildSecretCfg.Buy {
			return refuse("购买次数已达上限")
		}
		spendA, spendI := g.payFor([][2]int64{{guildSecretCfg.Cost[0][0], guildSecretCfg.Cost[0][1]}})
		if err := g.Store.Exchange(m.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("钻石不足")
		} else if err != nil {
			return err
		}
		role.Bought++
		role.LastBuy = s.Now().Unix()
		if err := g.Store.SaveSecretRole(role); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, m.RID); err != nil {
			return err
		}
		return s.Send(26803, proto.Message{"flag": 1, "msg": "购买成功", "count": guildSecretCfg.Free + role.Bought - role.Used, "last_buy_time": role.LastBuy})
	})
	r.Handle(26804, func(s *gateway.Session, req proto.Message) error {
		gd, m, ok, err := mine(s)
		if !ok {
			return err
		}
		refuse := func(msg string) error {
			return s.Send(26804, proto.Message{"flag": 0, "msg": msg, "number": req.Int("number")})
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		if st.Bid == 0 || req.Int("boss_id") != st.Bid {
			return refuse("没有可以领取的奖励")
		}
		dmg, err := g.Store.SecretDamages(gd.GID, st.Seq)
		if err != nil {
			return err
		}
		var took bool
		for _, d := range dmg {
			took = took || d.RID == m.RID
		}
		if !took {
			return refuse("参与过讨伐才能领取")
		}
		for _, b := range guildSecretCfg.Boxes[strconv.FormatInt(st.Bid, 10)] {
			if b.Number != req.Int("number") {
				continue
			}
			if st.MaxHP == 0 || st.HP*1000/st.MaxHP > b.Progress {
				return refuse("领主的生命值还没有削减到这一阶段")
			}
			ga, gi := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(rewardOf(b.Reward)), 1, ga, gi)
			if err := g.Store.ClaimSecretChest(gd.GID, st.Seq, m.RID, b.Number, ga, gi); errors.Is(err, store.ErrConflict) {
				return refuse("已经领取过了")
			} else if err != nil {
				return err
			}
			if err := sendAssets(s, g.Store, m.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, g.Store); err != nil {
				return err
			}
			if err := s.Send(26804, proto.Message{"flag": 1, "msg": "领取成功", "number": b.Number}); err != nil {
				return err
			}
			g.pushSecretInfo(gd, s.Now())
			return nil
		}
		return refuse("奖励不存在")
	})
	r.Handle(26806, func(s *gateway.Session, req proto.Message) error {
		gd, _, ok, err := mine(s)
		if !ok {
			return err
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return err
		}
		dmg, err := g.Store.SecretDamages(gd.GID, st.Seq)
		if err != nil {
			return err
		}
		num := int(req.Int("num"))
		if num < 1 || num > 50 {
			num = 50
		}
		list := []proto.Message{}
		for i, d := range dmg {
			if i >= num {
				break
			}
			mem, found := g.memberRow(gd.GID, d.RID)
			if !found {
				continue
			}
			face := int64(mem.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			list = append(list, proto.Message{"rid": d.RID, "srv_id": mem.SrvID, "lev": mem.Lev, "face_id": face, "avatar_bid": 0, "name": mem.Name, "dps": min(d.Last, 1<<32-1),
				"rank": i + 1, "max_dps": min(d.Total, 1<<32-1), "power": g.guildPower(d.RID), "face_update_time": 0, "face_file": ""})
		}
		return s.Send(26806, proto.Message{"boss_id": req.Int("boss_id"), "dps_list": list})
	})
	// the common checks of a challenge: a crusade is running (a pursuit after the kill counts), the boss is the
	// running one, and the member has a challenge left (taken here)
	prepare := func(s *gateway.Session, cmd uint16, req proto.Message, refuse func(string) error) (*store.Guild, *store.SecretState, *store.SecretRole, bool, error) {
		gd, m, ok, err := mine(s)
		if !ok {
			return nil, nil, nil, false, err
		}
		if fresh, err := g.Store.RoleByID(m.RID); err != nil {
			return nil, nil, nil, false, err
		} else if int64(fresh.Lev) < guildBossCfg.Const["join_lev"] {
			return nil, nil, nil, false, refuse("等级不足，需要达到10级才能参与")
		}
		st, err := g.secretState(gd, s.Now())
		if err != nil {
			return nil, nil, nil, false, err
		}
		if st.Bid == 0 || st.Settled || s.Now().Unix() >= st.EndTime {
			return nil, nil, nil, false, refuse("当前没有进行中的讨伐")
		}
		if req.Int("boss_id") != st.Bid {
			return nil, nil, nil, false, refuse("讨伐其他boss中")
		}
		role, count, err := g.secretCount(m.RID, st, s.Now())
		if err != nil {
			return nil, nil, nil, false, err
		}
		if count <= 0 {
			return nil, nil, nil, false, refuse("挑战次数不足")
		}
		return gd, st, role, true, nil
	}
	r.Handle(26802, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(26802, proto.Message{"flag": 0, "msg": msg, "combat_type": combatGuildSecret})
		}
		gd, st, rrole, ok, err := prepare(s, 26802, req, refuse)
		if !ok {
			return err
		}
		boss := guildSecretCfg.boss[st.Bid]
		stand := secretStand(boss)
		if stand == nil {
			return refuse("Boss数据缺失")
		}
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
		unit, obj := monsterOf(*stand, 0)
		fightHP := max(st.MaxHP, 1)
		unit.hp, unit.maxHP = fightHP, fightHP
		obj["hp"], obj["hp_max"] = fightHP, fightHP
		rounds, _, sim := playFightOpts(team, []fightUnit{unit}, rngOf(), int(guildBossCfg.Const["action_time"]), true)
		damage := max(0, fightHP-sim.units[len(team)].hp)
		rrole.Used++
		if err := g.Store.SaveSecretRole(rrole); err != nil {
			return err
		}
		intro := proto.Message{
			"combat_type": combatGuildSecret, "formation": []proto.Message{{"group": 1, "formation_type": formType, "formation_lev": 1}, {"group": 2, "formation_type": 1, "formation_lev": 1}},
			"objects": append(objects, obj), "is_auto": 0, "current_wave": 1, "total_wave": 1, "play_speed": 1,
			"extra_args": []proto.Message{}, "string_ext_args": []proto.Message{}, "a_object_num": len(team), "actor_role_name": role.Name, "target_role_name": boss.Name,
			"suppress": 0, "halo_list": []proto.Message{}, "hallows_list": []proto.Message{{"group": 1, "max": 3, "val": 0}, {"group": 2, "max": 3, "val": 0}},
		}
		if err := s.Send(26802, proto.Message{"flag": 1, "msg": "", "combat_type": combatGuildSecret}); err != nil {
			return err
		}
		return fights.begin(s, role, combatGuildSecret, intro, rounds, damage >= fightHP, func(s *gateway.Session, _ bool) error {
			if err := questEvent(s, g.Store, role.RID, evtGuildSecret, 1); err != nil {
				return err
			}
			return g.secretStrike(s, gd, st, damage)
		})
	})
	r.Handle(26805, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(26805, proto.Message{"flag": 0, "msg": msg}) }
		gd, st, rrole, ok, err := prepare(s, 26805, req, refuse)
		if !ok {
			return err
		}
		dmg, err := g.Store.SecretDamages(gd.GID, st.Seq)
		if err != nil {
			return err
		}
		var last int64
		for _, d := range dmg {
			if d.RID == role.RID {
				last = d.Last
			}
		}
		if last <= 0 {
			return refuse("还没有挑战记录，不能扫荡")
		}
		rrole.Used++
		if err := g.Store.SaveSecretRole(rrole); err != nil {
			return err
		}
		if err := s.Send(26805, proto.Message{"flag": 1, "msg": "扫荡成功"}); err != nil {
			return err
		}
		return g.secretStrike(s, gd, st, last)
	})
}
