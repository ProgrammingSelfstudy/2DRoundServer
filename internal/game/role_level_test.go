package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestExperienceLevelsTheRoleUp(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if expTable[0].ExpMax != 120 || expTable[1].ExpMax != 360 {
		t.Fatalf("level table: %+v", expTable[:2])
	}
	// 500 exp: 120 to leave level 1, 360 to leave level 2, 20 left over
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: "asset", ItemID: 5, Num: 500}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	as := c.recv(10302)
	up := c.recv(10344)
	feats := c.recv(16401).Arr("feat_list") // the level achievements move with the level
	if len(feats) == 0 {
		t.Fatal("no 16401 after the level-up")
	}
	for _, f := range feats {
		if v := f.Arr("progress")[0]; v.Int("value") != 3 {
			t.Fatalf("achievement %d shows %d, want level 3", f.Int("id"), v.Int("value"))
		}
	}
	c.recv(10500)
	if as.Int("lev") != 3 || as.Int("exp") != 20 {
		t.Fatalf("10302: %v", as)
	}
	if up.Int("old_lev") != 1 || up.Int("lev") != 3 {
		t.Fatalf("10344: %v", up)
	}
	got, _ := e.st.RoleAssets(role.RID)
	if got[5] != 20 || got[3] != 10+20 { // the gems of levels 2 and 3
		t.Fatalf("ledger: %v", got)
	}
	if r, _ := e.st.RoleByID(role.RID); r.Lev != 3 {
		t.Fatalf("stored level: %d", r.Lev)
	}
	// the next award starts from the new level and the session knows the new level
	m = e.sendMail(store.Mail{Subject: "g2", Attachments: []store.Attachment{{Kind: "asset", ItemID: 5, Num: 100}}}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.recv(10801)
	if as := c.recv(10302); as.Int("lev") != 3 || as.Int("exp") != 120 {
		t.Fatalf("no level yet: %v", as)
	}
	c.recv(10500)
	if got := c.recvOrNone(10344); got != nil {
		t.Fatalf("unexpected 10344: %v", got)
	}
}

func TestLevelStopsAtTheTableEnd(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("u1", "甲")
	if _, err := e.st.Summon(role.RID, store.SummonResult{GainAssets: map[int64]int64{5: 5_000_000_000}}); err != nil {
		t.Fatal(err)
	}
	r, _ := e.st.RoleByID(role.RID)
	got, _ := e.st.RoleAssets(role.RID)
	if r.Lev != MaxRoleLevel() || got[5] <= 0 {
		t.Fatalf("level %d exp %d", r.Lev, got[5])
	}
}
