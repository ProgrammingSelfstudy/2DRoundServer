package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestH5CityProfileRenameAndAvatar(t *testing.T) {
	e := newDungeonEnv(t)
	c, role := enterDungeonRole(e, store.PlatformH5, "profile", "原名")
	_, fan := enterDungeonRole(e, store.PlatformH5, "profile-fan", "别人的名字")
	if err := e.st.SetRoleLevel(role.RID, 36); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetRoleVIP(role.RID, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateGuild(store.Guild{SrvID: role.SrvID, Name: "草帽团", LeaderRID: role.RID}, time.Now().Unix(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ToggleFollow(fan.RID, role.RID, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	rename := func(name string, sex, code int64) proto.Message {
		t.Helper()
		c.send(10343, proto.Message{"name": name, "sex": sex})
		m := c.waitFor(10343)
		if m.Int("code") != code {
			t.Fatalf("rename %q: %v", name, m)
		}
		return m
	}
	if m := rename("一二三四五六七", 1, 0); m.Int("is_first_rename") != 1 {
		t.Fatalf("invalid name used the free rename: %v", m)
	}
	if m := rename("和之国索隆", 1, 1); m.Str("name") != "和之国索隆" || m.Int("is_first_rename") != 0 {
		t.Fatalf("free rename: %v", m)
	}
	if m := rename("罗罗诺亚", 0, 0); m.Str("msg") != "钻石不足" || m.Str("name") != "和之国索隆" {
		t.Fatalf("paid rename without enough diamonds: %v", m)
	}
	e.grantAssets(c, role, map[int64]int64{3: 250})
	rename(fan.Name, 1, 0)
	if m := rename("罗罗诺亚", 0, 1); m.Str("name") != "罗罗诺亚" || m.Int("sex") != 1 {
		t.Fatalf("paid rename: %v", m)
	}
	if m := c.waitFor(10302); m.Str("gold") != "50" {
		t.Fatalf("paid rename balance: %v", m)
	}
	rename("罗罗诺亚", 1, 0)
	if balances, err := e.st.RoleAssets(role.RID); err != nil || balances[3] != 50 {
		t.Fatalf("rejected renames changed the ledger: %v (%v)", balances, err)
	}
	if _, err := e.st.Summon(role.RID, store.SummonResult{Seeds: []store.PartnerSeed{{BID: starterMale, Star: 4}}}); err != nil {
		t.Fatal(err)
	}
	c.send(10325, nil)
	faces := c.waitFor(10325).Arr("face_list")
	if len(faces) != 2 || faces[0].Int("face_id") != 1001 || faces[1].Int("face_id") != starterH5 {
		t.Fatalf("owned H5 faces: %v", faces)
	}
	c.send(10327, proto.Message{"face_id": starterMale})
	if m := c.waitFor(10327); m.Int("code") != 0 {
		t.Fatalf("four-star face was accepted: %v", m)
	}
	c.send(10327, proto.Message{"face_id": 1001})
	if m := c.waitFor(10327); m.Int("code") != 1 || m.Int("face_id") != 1001 {
		t.Fatalf("face change: %v", m)
	}
	c.c.Close()
	c = loginDungeonRole(e, "profile", role)
	c.send(10300, nil)
	base := c.waitFor(10301)
	if base.Int("rid") != role.RID || base.Str("srv_id") != role.SrvID || base.Str("name") != "罗罗诺亚" ||
		base.Int("lev") != 36 || base.Int("vip_lev") != 4 || base.Int("face_id") != 1001 || base.Str("gname") != "草帽团" || base.Int("fans_num") != 1 {
		t.Fatalf("profile on relogin: %v", base)
	}
	if m := c.waitFor(10306); m.Int("power") <= 0 {
		t.Fatalf("profile power: %v", m)
	}
}
