package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func featList(c *client) map[int64]proto.Message {
	c.send(16400, proto.Message{})
	out := map[int64]proto.Message{}
	for _, f := range c.recv(16400).Arr("feat_list") {
		out[f.Int("id")] = f
	}
	return out
}

func TestFeats(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	fs := featList(c)
	if len(fs) != 199 {
		t.Fatalf("feats: %d", len(fs))
	}
	if fs[10010].Int("finish") != 0 || fs[10010].Arr("progress")[0].Int("value") != 1 || fs[10010].Arr("progress")[0].Int("target_val") != 10 {
		t.Fatalf("level 10 feat at level 1: %v", fs[10010])
	}
	claim := func(id int64) proto.Message {
		c.send(16402, proto.Message{"id": id})
		return c.recv(16402)
	}
	if r := claim(10010); r.Int("code") != 0 {
		t.Fatalf("not yet: %v", r)
	}
	if r := claim(424242); r.Int("code") != 0 {
		t.Fatalf("unknown: %v", r)
	}
	// reach level 10: 120+360+720+1200+1800+... exp
	need := int64(0)
	for lev := 1; lev < 10; lev++ {
		need += expTable[lev-1].ExpMax
	}
	mailTo(t, e, c, role.RID, store.Attachment{Kind: "asset", ItemID: 5, Num: need})
	if fs := featList(c); fs[10010].Int("finish") != 1 || fs[10020].Int("finish") != 0 {
		t.Fatalf("after level 10: %v %v", fs[10010], fs[10020])
	}
	gems, _ := e.st.RoleAssets(role.RID)
	before := gems[3]
	r := claim(10010)
	if r.Int("code") != 1 || r.Int("id") != 10010 {
		t.Fatalf("claim: %v", r)
	}
	if p := c.recv(16401).Arr("feat_list"); len(p) != 1 || p[0].Int("finish") != 2 {
		t.Fatalf("16401: %v", p)
	}
	c.recv(10302)
	c.recv(10500)
	gems, _ = e.st.RoleAssets(role.RID)
	if gems[3] != before+500 {
		t.Fatalf("reward: %d -> %d", before, gems[3])
	}
	if r := claim(10010); r.Int("code") != 0 {
		t.Fatalf("twice: %v", r)
	}
	if fs := featList(c); fs[10010].Int("finish") != 2 {
		t.Fatalf("claimed: %v", fs[10010])
	}
}

func TestFriendFeats(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 19, 10, 0, 0, 0, dayZone))
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	if fs := featList(ca); fs[12010].Int("finish") != 0 { // "own a first friend"
		t.Fatalf("no friend yet: %v", fs[12010])
	}
	befriend(t, ca, a, cb, b)
	if fs := featList(ca); fs[12010].Int("finish") != 1 {
		t.Fatalf("with a friend: %v", fs[12010])
	}
	giftReq(ca, 0, b)
	ca.recv(10302)
	pushed := ca.recv(16401).Arr("feat_list")
	found := false
	for _, f := range pushed {
		if f.Int("id") == 12030 && f.Arr("progress")[0].Int("value") == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("a gift counts for the gift achievements: %v", pushed)
	}
}
