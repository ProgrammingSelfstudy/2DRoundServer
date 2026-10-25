package game

import (
	"strings"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestRoleSettings(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(10309, proto.Message{"signature": "  出海去  "})
	if r := c.recv(10309); r.Int("code") != 1 || r.Str("signature") != "出海去" {
		t.Fatalf("signature: %v", r)
	}
	c.send(10309, proto.Message{"signature": strings.Repeat("字", signatureMax+1)})
	if r := c.recv(10309); r.Int("code") != 0 || r.Str("msg") == "" {
		t.Fatalf("too long: %v", r)
	}
	c.send(10348, proto.Message{"is_show_vip": 1})
	if r := c.recv(10348); r.Int("code") != 1 || r.Int("is_show_vip") != 1 {
		t.Fatalf("show vip: %v", r)
	}
	c.send(10318, proto.Message{"auto_pk": 2})
	if r := c.recv(10318); r.Int("code") != 0 {
		t.Fatalf("auto_pk 2: %v", r)
	}
	c.send(10318, proto.Message{"auto_pk": 1})
	c.recv(10318)
	cfg, _ := e.st.SettingsOf(role.RID)
	if cfg.Signature != "出海去" || cfg.ShowVIP != 1 || cfg.AutoPK != 1 {
		t.Fatalf("saved: %+v", cfg)
	}
	// the next login carries them in 10301
	c2, _ := e.dialSameRole("u1", role)
	c2.send(10300, proto.Message{})
	if m := c2.recv(10301); m.Str("signature") != "出海去" || m.Int("is_show_vip") != 1 || m.Int("auto_pk") != 1 {
		t.Fatalf("10301: %v", m)
	}
}

func TestRoleFaces(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	if ok, err := e.st.GrantStarterPartners(role.RID, []store.PartnerSeed{{BID: 10401, Star: 4}}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	c.send(10325, proto.Message{})
	var has []int64
	for _, f := range c.recv(10325).Arr("face_list") {
		has = append(has, f.Int("face_id"))
	}
	if len(has) != 1 || has[0] != 10401 {
		t.Fatalf("faces: %v", has)
	}
	use := func(id int64) proto.Message {
		c.send(10327, proto.Message{"face_id": id})
		return c.recv(10327)
	}
	if r := use(10402); r.Int("code") != 0 {
		t.Fatalf("not owned: %v", r)
	}
	if r := use(1001); r.Int("code") != 0 { // VIP 4 needed
		t.Fatalf("vip face: %v", r)
	}
	if err := e.st.SetRoleVIP(role.RID, 4); err != nil {
		t.Fatal(err)
	}
	if r := use(1001); r.Int("code") != 1 {
		t.Fatalf("vip face now: %v", r)
	}
	if got, _ := e.st.RoleByID(role.RID); got.FaceID != 1001 {
		t.Fatalf("face not saved: %d", got.FaceID)
	}
	if err := e.st.GrantLook(role.RID, store.LookFace, 2007, 0); err != nil {
		t.Fatal(err)
	}
	if r := use(2007); r.Int("code") != 1 {
		t.Fatalf("granted face: %v", r)
	}
}

func TestRoleCosmetics(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	// backdrops: the default 0 is always there; an owned one can be used
	c.send(10328, proto.Message{})
	if l := c.recv(10328).Arr("backdrop_list"); len(l) != 1 || l[0].Int("backdrop_id") != 0 {
		t.Fatalf("backdrops: %v", l)
	}
	c.send(10329, proto.Message{"backdrop_id": 4242})
	if r := c.recv(10329); r.Int("code") != 0 {
		t.Fatalf("not owned: %v", r)
	}
	if err := e.st.GrantLook(role.RID, store.LookBackdrop, 4242, 0); err != nil {
		t.Fatal(err)
	}
	c.send(10329, proto.Message{"backdrop_id": 4242})
	if r := c.recv(10329); r.Int("code") != 1 {
		t.Fatalf("use: %v", r)
	}
	if cfg, _ := e.st.SettingsOf(role.RID); cfg.BackdropID != 4242 {
		t.Fatalf("not saved: %+v", cfg)
	}

	// avatar frames: 1000 costs nothing, 2000 costs one 50001
	c.send(21500, proto.Message{})
	if l := c.recv(21500).Arr("avatar_frame"); len(l) != 0 {
		t.Fatalf("no frames yet: %v", l)
	}
	activate := func(id int64) proto.Message {
		c.send(21503, proto.Message{"base_id": id})
		return c.recv(21503)
	}
	c.send(21503, proto.Message{"base_id": 1000})
	c.recv(10302)
	c.recv(10500)
	if l := c.recv(21503).Arr("avatar_frame"); len(l) != 1 || l[0].Int("base_id") != 1000 {
		t.Fatalf("free frame: %v", l)
	}
	if l := activate(2000).Arr("avatar_frame"); len(l) != 1 { // no 50001: nothing changes
		t.Fatalf("unpaid frame: %v", l)
	}
	e.give(c, role, map[int64]int64{50001: 1})
	c.send(21503, proto.Message{"base_id": 2000})
	c.recv(10302)
	c.recv(10500)
	if l := c.recv(21503).Arr("avatar_frame"); len(l) != 2 {
		t.Fatalf("paid frame: %v", l)
	}
	c.send(21501, proto.Message{"base_id": 2000})
	if r := c.recv(21501); r.Int("base_id") != 2000 {
		t.Fatalf("use frame: %v", r)
	}

	// titles: 90003 lasts 5760 minutes once activated; the list also says which one is in use
	if err := e.st.GrantLook(role.RID, store.LookTitle, 90003, 0); err != nil {
		t.Fatal(err)
	}
	c.send(23301, proto.Message{"base_id": 90003})
	if r := c.recv(23301); r.Int("base_id") != 90003 {
		t.Fatalf("use title: %v", r)
	}
	c.send(23300, proto.Message{})
	if r := c.recv(23300); r.Int("base_id") != 90003 || len(r.Arr("honor")) != 1 {
		t.Fatalf("titles: %v", r)
	}
	// bubbles
	c.send(12700, proto.Message{})
	if r := c.recv(12700); r.Int("say_frame_bid") != 0 || len(r.Arr("say_frame")) != 0 {
		t.Fatalf("bubbles: %v", r)
	}
	c2, _ := e.dialSameRole("u1", role)
	c2.send(10300, proto.Message{})
	if m := c2.recv(10301); m.Int("avatar_base_id") != 2000 || m.Int("backdrop_id") != 4242 {
		t.Fatalf("10301: %v", m)
	}
}
