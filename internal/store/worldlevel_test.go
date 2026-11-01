package store

import (
	"bytes"
	"testing"
	"time"
)

func TestWorldLevelOverrideUsesClockAndKeepsExpiredStateReadOnly(t *testing.T) {
	e := newMailEnv(t)
	s := e.s
	now := *e.clock
	if err := s.SetRoleLevel(e.a.RID, 30); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoleLevel(e.b.RID, 50); err != nil {
		t.Fatal(err)
	}
	if lev, err := s.WorldLevel("symlf_1"); err != nil || lev != 40 {
		t.Fatalf("original computation: %d %v", lev, err)
	}
	if err := s.SaveWorldLevelOverride("symlf_1", WorldLevelOverride{Override: 88, Until: now.Add(time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.GlobalModule("symlf_1", WorldLevelModule)
	for _, tc := range []struct {
		at   time.Time
		want int
	}{{now, 88}, {now.Add(59 * time.Second), 88}, {now.Add(time.Minute), 40}, {now.Add(2 * time.Minute), 40}} {
		lev, err := s.WorldLevelAt("symlf_1", tc.at)
		if err != nil || lev != tc.want {
			t.Fatalf("clock %s: %d %v", tc.at, lev, err)
		}
	}
	after, _, _ := s.GlobalModule("symlf_1", WorldLevelModule)
	if !bytes.Equal(before, after) {
		t.Fatal("expiry read rewrote configuration")
	}
	*e.clock = now.Add(time.Minute)
	if lev, err := s.WorldLevel("symlf_1"); err != nil || lev != 40 {
		t.Fatal("WorldLevel ignored store clock expiry")
	}
	if err := s.SaveWorldLevelOverride("symlf_1", WorldLevelOverride{Override: 99}); err != nil {
		t.Fatal(err)
	}
	if lev, _ := s.WorldLevelAt("symlf_1", now.AddDate(5, 0, 0)); lev != 99 {
		t.Fatal("until=0 must be persistent")
	}
	if err := s.SaveWorldLevelOverride("symlf_1", WorldLevelOverride{Override: 0, Until: now.Add(time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if lev, _ := s.WorldLevel("symlf_1"); lev != 40 {
		t.Fatal("zero did not disable override")
	}
	info, err := s.WorldLevelInfoAt("symlf_1", now)
	if err != nil || info.Computed != 40 || info.Effective != 40 || info.Active {
		t.Fatalf("info: %+v %v", info, err)
	}
	if lev, err := s.WorldLevel("symlf_2"); err != nil || lev == 99 {
		t.Fatal("override leaked to another server")
	}
}
