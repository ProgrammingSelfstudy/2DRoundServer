package game

import (
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	h5GuildRules = iota
	h5GuildSkill
	h5GuildBoss
)

func isH5Client(s *gateway.Session) bool {
	account := s.Account()
	return account != nil && account.Platform == store.PlatformH5
}

// Use the same currency IDs that the guild handlers charge and credit.
func guildAmounts(list [][2]int64) [][2]int64 {
	out := make([][2]int64, 0, len(list))
	for _, item := range list {
		out = append(out, [2]int64{guildLabel(item[0]), item[1]})
	}
	return out
}

func (g *GuildService) h5Rules() any {
	donations := make([]any, 0, len(guildCfg.Donate))
	for _, d := range guildCfg.Donate {
		donations = append(donations, map[string]any{
			"id": d.ID, "name": d.Name, "cost": guildAmounts(d.Loss), "rewards": guildAmounts(d.Gain),
		})
	}
	boxes := make([]any, 0, len(guildCfg.Boxes))
	for _, b := range guildCfg.Boxes {
		boxes = append(boxes, map[string]any{"id": b.ID, "threshold": b.Val, "rewards": guildAmounts(b.Rewards)})
	}
	return map[string]any{
		"minimumLevel": g.cfg("guild_min_lev"), "creationVip": g.cfg("maintain_vip_condition"),
		"creationGold": g.cfg("create_gold"), "dayExpMax": g.cfg("day_exp_max"),
		"powerChoices": guildCfg.PowerChoices, "donations": donations, "boxes": boxes,
		"transferHours": g.cfg("transfer_time_condition"), "joinCooldownHours": g.cfg("guild_quit_cd") / 3600,
	}
}

func (g *GuildService) h5Skill(rid int64, gd *store.Guild, career int) (any, error) {
	st, err := g.Store.GuildSkillsOf(rid)
	if err != nil {
		return nil, err
	}
	level := gskillPveLevel(st, career)
	group := gskillCfg.byGroup[gskillCurrentGroup(career, st.PveDone[career])]
	skills := make([]any, 0, len(group))
	active := len(st.PveActive[career])
	for i, skill := range group {
		effect := guildSkillEffects[career][i]
		skills = append(skills, map[string]any{
			"id": skill.ID, "name": effect.name, "level": skill.GuildLev, "attribute": effect.key,
			"increment": effect.value, "cost": guildAmounts(skill.Loss), "active": i < active,
		})
	}
	return map[string]any{
		"guildId": gd.GID, "guildLevel": gd.Lev, "career": career,
		"group": st.PveDone[career] + 1, "groups": gskillHeavens, "skills": skills,
		"resetGold":   gskillResetCost(!st.PveReset, level, false),
		"resetRefund": g.cleanArr(pveRefund(st, career)),
		"level":       level,
	}, nil
}

func (g *GuildService) h5Boss(s *gateway.Session, gd *store.Guild) (any, error) {
	info, err := g.bossInfo(s.Role().RID, gd.GID, s.Now())
	if err != nil {
		return nil, err
	}
	chapter, bought := info.Int("fid"), info.Int("buy_count")
	chap := guildBossCfg.chapter(chapter)
	boss := guildBossCfg.boss[chap.Boss]
	var price, vip int64
	if bought < int64(len(guildBossCfg.Buys)) {
		buy := guildBossCfg.Buys[bought]
		price, vip = buy.Expend[1], buy.VipLev
	}
	return map[string]any{
		"guildId": gd.GID, "bossId": boss.BossID, "name": boss.Name, "chapter": chapter,
		"maxHp": gbossWire(boss.HP, boss.HP), "hpScaled": boss.HP > 1<<32-1,
		"minimumLevel": guildBossCfg.Const["join_lev"], "buyGold": price, "buyVip": vip, "buyIndex": bought,
		"buffGold": guildBossCfg.Const["buff_cost"], "buffItem": guildBossCfg.Const["buff_item"], "buffMax": len(guildBossCfg.Buffs),
		"challengeRewards": guildAmounts(pairsOf(chap.Challenge)), "killRewards": guildAmounts(pairsOf(chap.Kill)),
		"state": info,
	}, nil
}

func (g *GuildService) registerH5Guild(r *gateway.Router) {
	r.Handle(proto.H5GuildView, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if !isH5Client(s) {
			return gcode(s, proto.H5GuildView, false, "当前客户端不支持此公会页面")
		}
		var data any
		view := req.Int("view")
		switch view {
		case h5GuildRules:
			data = g.h5Rules()
		case h5GuildSkill, h5GuildBoss:
			gd, _, err := g.mine(s)
			if errors.Is(err, errNoGuild) {
				return gcode(s, proto.H5GuildView, false, "你还没有加入公会")
			}
			if err != nil {
				return err
			}
			if view == h5GuildSkill {
				career := int(req.Int("career"))
				if gskillBase(career) == 0 {
					return gcode(s, proto.H5GuildView, false, "无效的职业")
				}
				data, err = g.h5Skill(role.RID, gd, career)
			} else {
				data, err = g.h5Boss(s, gd)
			}
			if err != nil {
				return err
			}
		default:
			return gcode(s, proto.H5GuildView, false, "无效的公会页面")
		}
		body, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return s.Send(proto.H5GuildView, proto.Message{"code": 1, "data": string(body)})
	})
}
