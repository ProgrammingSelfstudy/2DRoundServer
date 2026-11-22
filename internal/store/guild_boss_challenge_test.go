package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestGuildBossChallengeRollsBackAndSharesAttemptLimit(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "guild.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	const server = "symlf_1"
	if err := s.EnsureServer(srv(server)); err != nil {
		t.Fatal(err)
	}
	account, err := s.CreateAccount(PlatformH5, "guild-boss", "", "")
	if err != nil {
		t.Fatal(err)
	}
	role, err := s.CreateRole(account.ID, server, "船长", 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := s.CreateGuild(Guild{SrvID: server, Name: "海贼公会", LeaderRID: role.RID}, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := &BossState{GID: gid, Day: 20261121, Fid: 1, HP: 5000}
	if ok, err := s.ResetBossDay(st, 0); err != nil || !ok {
		t.Fatalf("initialize boss: %v, %v", ok, err)
	}
	challenge := GuildBossChallenge{
		State: st, RID: role.RID, BossID: 101, Damage: st.HP, Free: 1, Vitality: 10, Exp: 100, LevelExp: []int64{100, 0},
		RewardAssets: map[int64]int64{10: 10}, KillAssets: map[int64]int64{10: 200},
		RankMail: func(rank, damage int64) *Mail {
			return &Mail{SrvID: server, Subject: "公会排名", Attachments: []Attachment{{Kind: AttachAsset, ItemID: 10, Num: -1}}}
		},
	}
	if _, _, err := s.ApplyGuildBossChallenge(challenge); err == nil {
		t.Fatal("an invalid rank reward should abort settlement")
	}
	if state, _, err := s.BossStateOf(gid); err != nil || state.HP != st.HP || state.MaxID != 0 {
		t.Fatalf("boss changed during failed settlement: %+v, %v", state, err)
	}
	if attempt, err := s.BossRoleOf(role.RID); err != nil || attempt.Used != 0 {
		t.Fatalf("attempt changed during failed settlement: %+v, %v", attempt, err)
	}
	if assets, err := s.RoleAssets(role.RID); err != nil || assets[10] != 0 {
		t.Fatalf("paid a failed settlement: %v, %v", assets, err)
	}
	if vitality, err := s.GuildVitality(gid); err != nil || vitality != 0 {
		t.Fatalf("vitality changed during failed settlement: %v, %v", vitality, err)
	}

	challenge.Damage = 2000
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, _, err := s.ApplyGuildBossChallenge(challenge)
			results <- err
		}()
	}
	var successes int
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Errorf("concurrent challenge: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful challenges with one available attempt: %d", successes)
	}
	if state, _, err := s.BossStateOf(gid); err != nil || state.HP != 3000 {
		t.Fatalf("boss after one valid hit: %+v, %v", state, err)
	}
	if assets, err := s.RoleAssets(role.RID); err != nil || assets[10] != 10 {
		t.Fatalf("reward after one valid hit: %v, %v", assets, err)
	}
}
