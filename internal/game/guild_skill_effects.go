package game

import (
	"strings"

	"2droundserver/internal/store"
)

type guildSkillEffect struct {
	name, key string
	value     int64
}

// guild_skill_data.lua uses the same six increments on each of the 40 heavens
// of a career. Attributes ending in _per and the combat rates are per mille.
var guildSkillEffects = func() map[int][gskillPerHeaven]guildSkillEffect {
	attack := [gskillPerHeaven]guildSkillEffect{
		{"气血比", "hp_max_per", 5}, {"攻击比", "atk_per", 5},
		{"暴击", "crit_rate", 5}, {"暴伤", "crit_ratio", 10},
		{"速度", "speed", 4}, {"伤害加成", "dam", 5},
	}
	tank, support := attack, attack
	tank[2], tank[3], tank[5] = guildSkillEffect{"控制", "hit_magic", 5}, guildSkillEffect{"抗暴", "tenacity", 5}, guildSkillEffect{"物理免伤", "res_p", 5}
	support[2], support[3], support[5] = guildSkillEffect{"抗控", "dodge_magic", 5}, tank[3], guildSkillEffect{"魔法免伤", "res_s", 5}
	return map[int][gskillPerHeaven]guildSkillEffect{2: attack, 3: attack, 4: tank, 5: support}
}()

// There is one entry per distinct attribute in a heaven.
func guildSkillBonus(p store.Partner, key string) int64 {
	if len(p.GuildSkillLevels) == 0 || !h5HeroEligible(p.BID, p.Star) {
		return 0
	}
	career := partnerTable[p.BID].Type
	level := min(p.GuildSkillLevels[career], gskillHeavens*gskillPerHeaven)
	if level <= 0 {
		return 0
	}
	for i, effect := range guildSkillEffects[career] {
		if effect.key == key {
			steps := level / gskillPerHeaven
			if i < level%gskillPerHeaven {
				steps++
			}
			return int64(steps) * effect.value
		}
	}
	return 0
}

type guildCombatBonus struct {
	damage, tenacity, control, resistControl, resistPhysical, resistMagic int64
}

func guildCombatBonusOf(p store.Partner) guildCombatBonus {
	return guildCombatBonus{
		damage: guildSkillBonus(p, "dam"), tenacity: guildSkillBonus(p, "tenacity"),
		control: guildSkillBonus(p, "hit_magic"), resistControl: guildSkillBonus(p, "dodge_magic"),
		resistPhysical: guildSkillBonus(p, "res_p"), resistMagic: guildSkillBonus(p, "res_s"),
	}
}

func (g guildCombatBonus) resistDamage(rule skillRule) int64 {
	if g.resistMagic != 0 && (strings.Contains(rule.Description, "魔法伤害") || strings.Contains(rule.Description, "法术伤害")) {
		return g.resistMagic
	}
	if g.resistPhysical != 0 && (strings.Contains(rule.Description, "物理伤害") || strings.Contains(rule.Description, "体术伤害")) {
		return g.resistPhysical
	}
	return 0
}
