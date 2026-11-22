package game

import (
	"time"

	"2droundserver/internal/store"
)

func (g *GuildService) bossSettle(role *store.Role, gd *store.Guild, st *store.BossState, boss gbossRow, damage int64, now time.Time, attemptSpent bool) (bossOutcome, error) {
	chap := guildBossCfg.chapter(st.Fid)
	rewards, kill := pairsOf(chap.Challenge), pairsOf(chap.Kill)
	c := store.GuildBossChallenge{
		State: st, RID: role.RID, BossID: boss.BossID, Damage: damage, AttemptSpent: attemptSpent,
		Free: guildBossCfg.Const["free_time"], Exp: chap.GuildExp, LevelExp: guildLevelExp(), Vitality: vitalityBoss,
		RewardAssets: map[int64]int64{}, RewardItems: map[int64]int64{},
		KillAssets: map[int64]int64{}, KillItems: map[int64]int64{},
		RankMail: func(rank, total int64) *store.Mail {
			return guildBossRankMail(gd.SrvID, boss, rank, total)
		},
	}
	splitGains(guildGains(rewards), 1, c.RewardAssets, c.RewardItems)
	splitGains(guildGains(kill), 1, c.KillAssets, c.KillItems)
	killed, firstKill, err := g.Store.ApplyGuildBossChallenge(c)
	if err != nil {
		return bossOutcome{}, err
	}
	if firstKill {
		rewards = append(rewards, kill...)
	}
	if killed {
		g.pushLog(gd.GID, guildLogGeneral, role.RID, role.Name, "击败了公会副本的"+boss.Name, now.Unix())
		g.pushInfo(gd.GID, dayKey(now))
	}
	return bossOutcome{damage: damage, rewards: rewards}, nil
}

func guildBossRankMail(srvID string, boss gbossRow, rank, total int64) *store.Mail {
	if total < guildBossCfg.Const["rank_dam"] {
		return nil
	}
	for _, row := range guildBossCfg.ranks[boss.BossID] {
		if rank >= row.Rank1 && rank <= row.Rank2 {
			return &store.Mail{
				SrvID: srvID, Subject: "公会副本排名奖励", FromName: "公会副本", CreatedBy: "guildboss",
				Content:     "你在公会副本" + boss.Name + "的伤害排名第" + itoa64(rank) + "，这是排名奖励。",
				Attachments: attachmentsOf(pairsOf(row.Award)),
			}
		}
	}
	return nil
}
