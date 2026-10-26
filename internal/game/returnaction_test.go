package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func retEnv(t *testing.T) (*env, *client, *store.Role, int64) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("ret", "归来人")
	if err := e.st.SetRoleLevel(role.RID, 40); err != nil {
		t.Fatal(err)
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: retCamp, BID: 101, Status: 1, StartAt: start, EndAt: start + 30*86400, Spec: actSpecJSON(t, false)})
	return e, c, role, start
}

func (e *env) retBack(t *testing.T, role *store.Role, since int64) {
	t.Helper()
	d := &retRole{}
	if _, err := e.st.LoadState(role.RID, retModule, d); err != nil {
		t.Fatal(err)
	}
	*d = retRole{Start: dayStart(actNow).Unix(), Since: since}
	if err := e.st.SaveState(role.RID, retModule, d); err != nil {
		t.Fatal(err)
	}
}

func (e *env) retState(t *testing.T, role *store.Role) *retRole {
	d := &retRole{}
	if _, err := e.st.LoadState(role.RID, retModule, d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestReturnActionWhoComesBack(t *testing.T) {
	e, c, role, _ := retEnv(t)
	c.send(27902, proto.Message{})
	if r := c.waitFor(27902); r.Int("is_open") != 0 {
		t.Fatalf("not away: %v", r)
	}
	away := *role
	away.Lev, away.LastLogoutAt = 40, actNow.Unix()-20*86400
	ReturnEntered(e.st, &away, actNow)
	c.send(27902, proto.Message{})
	if r := c.waitFor(27902); r.Int("is_open") != 1 || r.Int("open_day") != 1 || r.Int("period") != 1 {
		t.Fatalf("back: %v", r)
	}
	// the days go on from the day of coming back and end after 14
	e.setNow(actNow.AddDate(0, 0, 2))
	c.send(27902, proto.Message{})
	if r := c.waitFor(27902); r.Int("open_day") != 3 {
		t.Fatalf("day 3: %v", r)
	}
	e.setNow(actNow.AddDate(0, 0, 14))
	c.send(27902, proto.Message{})
	if r := c.waitFor(27902); r.Int("is_open") != 0 {
		t.Fatalf("over: %v", r)
	}
	// too short away, too low a level
	short := *role
	short.Lev, short.LastLogoutAt = 50, actNow.Unix()-10*86400
	low := *role
	low.Lev, low.LastLogoutAt = 39, actNow.Unix()-40*86400
	d := &retRole{}
	e.st.SaveState(role.RID, retModule, d)
	ReturnEntered(e.st, &short, actNow)
	ReturnEntered(e.st, &low, actNow)
	if got := e.retState(t, role); got.Since != 0 {
		t.Fatalf("must not come back: %+v", got)
	}
}

func TestReturnActionGiftAndSignin(t *testing.T) {
	e, c, role, _ := retEnv(t)
	c.send(27901, proto.Message{})
	if r := c.waitFor(27901); r.Int("code") != 0 {
		t.Fatalf("gift for one who did not come back: %v", r)
	}
	e.retBack(t, role, actNow.Unix())
	c.send(27900, proto.Message{})
	if r := c.waitFor(27900); r.Int("flag") != 0 || r.Int("endtime") != dayStart(actNow).AddDate(0, 0, 14).Unix() {
		t.Fatalf("27900: %v", r)
	}
	c.send(27901, proto.Message{})
	if r := c.waitFor(27901); r.Int("code") != 1 {
		t.Fatalf("gift: %v", r)
	}
	if r := c.waitFor(27900); r.Int("flag") != 1 {
		t.Fatalf("27900 after: %v", r)
	}
	c.send(27901, proto.Message{})
	if r := c.waitFor(27901); r.Int("code") != 0 {
		t.Fatalf("gift twice: %v", r)
	}
	if items, _ := e.st.RoleItems(role.RID); items[10416] < 500 {
		t.Fatalf("gift items: %v", items)
	}
	c.send(27908, proto.Message{})
	r := c.waitFor(27908)
	list := r.Arr("status_list")
	if len(list) != 7 || list[0].Int("status") != 1 || list[1].Int("status") != 0 {
		t.Fatalf("27908: %v", r)
	}
	c.send(27909, proto.Message{"day": 2})
	if r := c.waitFor(27909); r.Int("code") != 0 {
		t.Fatalf("a day to come: %v", r)
	}
	c.send(27909, proto.Message{"day": 1})
	if r := c.waitFor(27909); r.Int("code") != 1 || r.Int("day") != 1 {
		t.Fatalf("sign: %v", r)
	}
	c.send(27909, proto.Message{"day": 1})
	if r := c.waitFor(27909); r.Int("code") != 0 {
		t.Fatalf("sign twice: %v", r)
	}
	e.setNow(actNow.AddDate(0, 0, 1))
	c.send(27908, proto.Message{})
	if list := c.waitFor(27908).Arr("status_list"); list[0].Int("status") != 2 || list[1].Int("status") != 1 || list[2].Int("status") != 0 {
		t.Fatalf("27908 next day: %v", list)
	}
}

func TestReturnActionSummon(t *testing.T) {
	e, c, role, _ := retEnv(t)
	e.retBack(t, role, actNow.Unix())
	c.send(27903, proto.Message{})
	p := c.waitFor(27903)
	if p.Int("draw_time") != 0 || p.Int("limit_draw_time") != 100 || p.Int("free_time") != 1 || len(p.Arr("award_list")) != 10 || p.Int("starttime") != actNow.Unix() {
		t.Fatalf("27903: %v", p)
	}
	c.send(27904, proto.Message{"type": 1})
	if r := c.waitFor(27904); r.Int("code") != 1 {
		t.Fatalf("free draw: %v", r)
	}
	if p := c.waitFor(27903); p.Int("draw_time") != 1 || p.Int("free_time") != 0 {
		t.Fatalf("after: %v", p)
	}
	c.send(27904, proto.Message{"type": 1})
	if r := c.waitFor(27904); r.Int("code") != 0 {
		t.Fatalf("single without tickets: %v", r)
	}
	c.send(27904, proto.Message{"type": 3})
	if r := c.waitFor(27904); r.Int("code") != 0 {
		t.Fatalf("ten without diamonds: %v", r)
	}
	// tickets for 9 draws and the diamonds for the tenth
	grantItems(t, e, c, role, map[int64]int64{3: 100}, map[int64]int64{10416: 900})
	c.send(27904, proto.Message{"type": 3})
	if r := c.waitFor(27904); r.Int("code") != 1 {
		t.Fatalf("ten: %v", r)
	}
	c.settle()
	if d := e.retState(t, role); len(d.Drawn) != 11 {
		t.Fatalf("drawn: %v", d.Drawn)
	}
	if assets, _ := e.st.RoleAssets(role.RID); assets[3] != 0 {
		t.Fatalf("diamonds: %v", assets)
	}
	// 100 draws take everything, each once; the must / must_not entries are where the table puts them
	d := e.retState(t, role)
	d.Drawn = nil
	for i := 0; i < 100; i++ {
		before := len(d.Drawn)
		e := retDraw(d.Drawn)
		if has(d.Drawn, e.ID) {
			t.Fatalf("draw %d takes %d again", i+1, e.ID)
		}
		if e.Must != 0 && e.Must != int64(i+1) && e.MustNot > int64(i+1) {
			t.Fatalf("entry %+v at draw %d", e, i+1)
		}
		if e.MustNot > int64(i+1) && e.Must == 0 {
			t.Fatalf("draw %d takes %d too early (must_not %d)", i+1, e.ID, e.MustNot)
		}
		d.Drawn = append(d.Drawn, e.ID)
		if len(d.Drawn) != before+1 {
			t.Fatal("no progress")
		}
	}
}

func TestReturnActionTasks(t *testing.T) {
	e, c, role, _ := retEnv(t)
	e.retBack(t, role, actNow.Unix())
	c.send(27905, proto.Message{})
	r := c.waitFor(27905)
	if len(r.Arr("quest_list")) != 7 || r.Arr("quest_list")[0].Int("finish") != 0 {
		t.Fatalf("27905: %v", r)
	}
	c.send(27907, proto.Message{"id": 103})
	if r := c.waitFor(27907); r.Int("code") != 0 {
		t.Fatalf("claim before: %v", r)
	}
	friend, frole := e.enter("retf", "朋友")
	friend.send(13303, rid(role))
	friend.waitFor(13303)
	c.waitFor(13304)
	c.send(13305, proto.Message{"rid": frole.RID, "srv_id": "symlf_1", "agreed": 1})
	c.waitFor(13305)
	u := c.waitFor(27906)
	if u.Int("id") != 103 || u.Int("finish") != 1 || u.Int("value") != 1 {
		t.Fatalf("27906: %v", u)
	}
	c.send(27907, proto.Message{"id": 103})
	if r := c.waitFor(27907); r.Int("code") != 1 || r.Int("id") != 103 {
		t.Fatalf("claim: %v", r)
	}
	c.send(27907, proto.Message{"id": 103})
	if r := c.waitFor(27907); r.Int("code") != 0 {
		t.Fatalf("claim twice: %v", r)
	}
	// the day goes by: the tasks start again
	e.setNow(actNow.AddDate(0, 0, 1))
	c.send(27905, proto.Message{})
	if r := c.waitFor(27905); r.Arr("quest_list")[2].Int("finish") != 0 || r.Arr("quest_list")[2].Int("value") != 0 {
		t.Fatalf("next day: %v", r)
	}
}

func TestReturnActionRedPackets(t *testing.T) {
	e, c, role, _ := retEnv(t)
	grantItems(t, e, c, role, nil, map[int64]int64{10417: 2})
	other, orole := e.enter("reto", "路过人")
	if err := e.st.SetRoleLevel(orole.RID, 20); err != nil {
		t.Fatal(err)
	}
	c.send(10515, proto.Message{"id": 10417, "quantity": 1})
	if r := c.waitFor(10515); r.Int("flag") != 1 {
		t.Fatalf("use: %v", r)
	}
	c.waitFor(10500)
	if r := c.waitFor(27912); r.Int("code") != 2 || r.Int("max_num") != 5 {
		t.Fatalf("27912: %v", r)
	}
	other.send(27910, proto.Message{})
	r := other.waitFor(27910)
	list := r.Arr("red_packet_list")
	if len(list) != 1 || list[0].Int("status") != 1 || list[0].Str("name") != "归来人" || r.Int("get_num") != 0 {
		t.Fatalf("27910: %v", r)
	}
	id := list[0].Int("red_packet_id")
	other.send(27911, proto.Message{"red_packet_id": id})
	g := other.waitFor(27911)
	got := g.Arr("get_red_packet_list")
	if g.Int("code") != 1 || len(got) != 1 || g.Int("get_num") != 1 || g.Int("max_num") != 8 || g.Str("send_name") != "归来人" || got[0].Arr("item")[0].Int("num") <= 0 {
		t.Fatalf("27911: %v", g)
	}
	other.waitFor(27912)
	if assets, _ := e.st.RoleAssets(orole.RID); assets[1] < got[0].Arr("item")[0].Int("num") {
		t.Fatalf("coins: %v", assets)
	}
	// the same packet again only shows it
	other.send(27911, proto.Message{"red_packet_id": id})
	if g := other.waitFor(27911); g.Int("code") != 1 || len(g.Arr("get_red_packet_list")) != 1 {
		t.Fatalf("again: %v", g)
	}
	other.send(27913, proto.Message{})
	if m := other.waitFor(27913); len(m.Arr("get_red_packet_list")) != 1 {
		t.Fatalf("27913: %v", m)
	}
	// a packet is over after 24 hours
	e.setNow(actNow.Add(25 * time.Hour))
	other.send(27911, proto.Message{"red_packet_id": id})
	if g := other.waitFor(27911); g.Int("code") != 1 && g.Int("code") != 0 {
		t.Fatalf("expired: %v", g)
	}
	c.send(27910, proto.Message{})
	if r := c.waitFor(27910); len(r.Arr("red_packet_list")) != 0 {
		t.Fatalf("expired packets are not listed: %v", r)
	}
}

func TestReturnActionShop(t *testing.T) {
	e, c, role, _ := retEnv(t)
	c.send(27914, proto.Message{})
	r := c.waitFor(27914)
	if r.Int("limit_count") != 100 || len(r.Arr("buy_info")) != 7 {
		t.Fatalf("27914: %v", r)
	}
	c.send(27915, proto.Message{"id": 2})
	if r := c.waitFor(27915); r.Int("code") != 0 {
		t.Fatalf("buy without bottles: %v", r)
	}
	grantItems(t, e, c, role, nil, map[int64]int64{10418: 1000})
	for i := 0; i < 3; i++ {
		c.send(27915, proto.Message{"id": 2})
		if r := c.waitFor(27915); r.Int("code") != 1 {
			t.Fatalf("buy %d: %v", i, r)
		}
		c.waitFor(27914)
	}
	c.send(27915, proto.Message{"id": 2})
	if r := c.waitFor(27915); r.Int("code") != 0 {
		t.Fatalf("over the limit of 3: %v", r)
	}
	c.send(27914, proto.Message{})
	for _, b := range c.waitFor(27914).Arr("buy_info") {
		if b.Int("id") == 2 && b.Int("all_num") != 3 {
			t.Fatalf("buy_info: %v", b)
		}
	}
	if items, _ := e.st.RoleItems(role.RID); items[10418] != 1000-3*60 || items[10403] < 3 {
		t.Fatalf("items: %v", items)
	}
}
