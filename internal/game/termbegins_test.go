package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func tbEnv(t *testing.T) (*env, *client, *store.Role, int64, int64) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, role := e.enter("tb", "开学人")
	c.send(11000, proto.Message{})
	c.waitFor(11000)
	heroes, err := e.st.PartnersOf(role.RID)
	if err != nil || len(heroes) == 0 {
		t.Fatalf("no hero: %v", err)
	}
	for _, kind := range []int64{tbFormChapter, tbFormBoss} {
		c.send(11212, proto.Message{"type": kind, "formation_type": 1, "pos_info": []proto.Message{{"pos": 1, "id": heroes[0].PartnerID}}, "hallows_id": 0})
		if r := c.waitFor(11212); r.Int("code") != 1 {
			t.Fatalf("11212: %v", r)
		}
	}
	start := dayStart(actNow).Unix()
	e.addActivity(t, store.Activity{SrvID: role.SrvID, CampID: tbCamp, BID: 97008, Status: 1, StartAt: start, EndAt: start + 20*86400, Spec: actSpecJSON(t, false)})
	return e, c, role, start, heroes[0].PartnerID
}

func (e *env) tbRole(t *testing.T, role *store.Role) *tbRole {
	r := &tbRole{}
	if _, err := e.st.LoadState(role.RID, tbModule, r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTermBeginsPageAndBuy(t *testing.T) {
	e, c, role, start, _ := tbEnv(t)
	c.send(26700, proto.Message{})
	p := c.waitFor(26700)
	if p.Int("round") != 1 || p.Int("difficulty") != 1 || p.Int("order") != 1 || p.Int("cha_count") != 2 || p.Int("buy_count") != 6 || p.Int("hp_per") != 1000 ||
		p.Int("boss_flag") != 0 || p.Int("boss_time") != start+5*86400 || p.Int("end_time") != start+7*86400 || p.Int("boss_hp_per") != 1000 {
		t.Fatalf("26700: %v", p)
	}
	c.send(26701, proto.Message{})
	if r := c.waitFor(26701); r.Int("code") != 0 {
		t.Fatalf("buy without diamonds: %v", r)
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 1000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 6; i++ {
		c.send(26701, proto.Message{})
		if r := c.waitFor(26701); r.Int("code") != 1 || r.Int("count") != 2+int64(i) || r.Int("buy_count") != 6-int64(i) {
			t.Fatalf("buy %d: %v", i, r)
		}
	}
	c.send(26701, proto.Message{})
	if r := c.waitFor(26701); r.Int("code") != 0 {
		t.Fatalf("buy over the limit: %v", r)
	}
}

func TestTermBeginsClosedWithoutActivity(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	c, _ := e.enter("tb0", "无活动")
	c.send(26702, proto.Message{})
	if r := c.waitFor(26702); r.Int("code") != 0 {
		t.Fatalf("level without the activity: %v", r)
	}
	c.send(26707, proto.Message{})
	if r := c.waitFor(26707); r.Int("code") != 0 {
		t.Fatalf("boss without the activity: %v", r)
	}
	c.send(26704, proto.Message{"count": 1})
	if r := c.waitFor(26704); r.Int("code") != 0 {
		t.Fatalf("papers without the activity: %v", r)
	}
}

func TestTermBeginsLevelCarriesHPAndClearPaysPapers(t *testing.T) {
	e, c, role, _, _ := tbEnv(t)
	c.send(26702, proto.Message{})
	if r := c.waitFor(26702); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatTBLevel {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(26703)
	info := c.waitFor(26700)
	c.settle()
	r := e.tbRole(t, role)
	if r.Count != 1 {
		t.Fatalf("state: %+v", r)
	}
	if res.Int("result") != 1 && (r.Left <= 0 || info.Int("hp_per") >= 1000) {
		t.Fatalf("hp must carry over: %+v %v", r, info)
	}
	// one hp left: any hit clears the level
	r.Order, r.Left = 1, 1
	if err := e.st.SaveState(role.RID, tbModule, r); err != nil {
		t.Fatal(err)
	}
	c.send(26702, proto.Message{})
	c.waitFor(26702)
	c.waitFor(20027)
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res = c.waitFor(26703)
	info = c.waitFor(26700)
	c.settle()
	r = e.tbRole(t, role)
	items, _ := e.st.RoleItems(role.RID)
	if res.Int("result") != 1 || r.Order != 2 || info.Int("order") != 2 || len(res.Arr("award_list")) != 3 || items[80232] < 10 || items[80233] < 1 {
		t.Fatalf("clear: %v %+v %v %v", res, r, info, items)
	}
	c.send(26702, proto.Message{})
	if r := c.waitFor(26702); r.Int("code") != 0 {
		t.Fatalf("fight without attempts: %v", r)
	}
}

func TestTermBeginsPapersAndRewards(t *testing.T) {
	e, c, role, _, _ := tbEnv(t)
	grantItems(t, e, c, role, nil, map[int64]int64{80232: 400})
	c.send(26705, proto.Message{})
	if r := c.waitFor(26705); r.Int("num") != 0 || len(r.Arr("collect_schedule")) != 3 || r.Arr("collect_schedule")[0].Int("staus") != 0 {
		t.Fatalf("26705: %v", r)
	}
	c.send(26706, proto.Message{"id": 1})
	if r := c.waitFor(26706); r.Int("code") != 0 {
		t.Fatalf("claim before the papers: %v", r)
	}
	c.send(26704, proto.Message{"count": 500})
	if r := c.waitFor(26704); r.Int("code") != 0 {
		t.Fatalf("more papers than owned: %v", r)
	}
	c.send(26704, proto.Message{"count": 200})
	c.waitFor(10500)
	if r := c.waitFor(26704); r.Int("code") != 1 || r.Int("paper_sum") != 200 {
		t.Fatalf("hand in: %v", r)
	}
	c.send(26705, proto.Message{})
	r := c.waitFor(26705)
	if r.Int("num") != 200 || r.Arr("collect_schedule")[0].Int("staus") != 1 || r.Arr("collect_schedule")[1].Int("staus") != 0 {
		t.Fatalf("after: %v", r)
	}
	c.send(26706, proto.Message{"id": 1})
	if r := c.waitFor(26706); r.Int("code") != 1 || r.Int("id") != 1 {
		t.Fatalf("claim: %v", r)
	}
	c.send(26706, proto.Message{"id": 1})
	if r := c.waitFor(26706); r.Int("code") != 0 {
		t.Fatalf("claim twice: %v", r)
	}
	c.send(26711, proto.Message{})
	if r := c.waitFor(26711); r.Int("count") != 200 || r.Int("rank") != 100 || r.Int("round") != 1 {
		t.Fatalf("26711: %v", r)
	}
	c.send(26700, proto.Message{})
	if r := c.waitFor(26700); r.Int("paper_sum") != 200 || r.Int("buff_per") != 0 {
		t.Fatalf("26700: %v", r)
	}
}

func TestTermBeginsBossFightSweepRankAndMail(t *testing.T) {
	e, c, role, start, _ := tbEnv(t)
	c.send(26707, proto.Message{})
	if r := c.waitFor(26707); r.Int("code") != 0 {
		t.Fatalf("boss before it opens: %v", r)
	}
	e.setNow(time.Unix(start+5*86400+3600, 0))
	c.send(26707, proto.Message{})
	if r := c.waitFor(26707); r.Int("code") != 0 {
		t.Fatalf("boss without a ticket: %v", r)
	}
	c.send(26712, proto.Message{"num": 1, "flag": 0})
	if r := c.waitFor(26712); r.Int("code") != 0 {
		t.Fatalf("buy a ticket without diamonds: %v", r)
	}
	grantItems(t, e, c, role, map[int64]int64{3: 1000}, map[int64]int64{80233: 1})
	c.send(26700, proto.Message{})
	if p := c.waitFor(26700); p.Int("boss_flag") != 1 || p.Int("dps") != 0 {
		t.Fatalf("26700: %v", p)
	}
	c.send(26709, proto.Message{"boss_id": 1})
	if r := c.waitFor(26709); r.Int("code") != 0 {
		t.Fatalf("sweep before a fight: %v", r)
	}
	c.send(26707, proto.Message{})
	if r := c.waitFor(26707); r.Int("code") != 1 {
		t.Fatalf("boss fight: %v", r)
	}
	if m := c.waitFor(20027); m.Int("combat_type") != combatTBBoss {
		t.Fatalf("intro: %v", m)
	}
	c.send(20062, proto.Message{})
	c.waitFor(20062)
	res := c.waitFor(26708)
	c.waitFor(26700)
	c.settle()
	r := e.tbRole(t, role)
	if res.Int("all_dps") != r.Last || r.Last <= 0 || r.Total != r.Last || len(res.Arr("award_list")) != 1 {
		t.Fatalf("result: %v %+v", res, r)
	}
	last := r.Last
	// the ticket is spent: buy one with diamonds, then mop up
	c.send(26712, proto.Message{"num": 1, "flag": 1})
	if b := c.waitFor(26712); b.Int("code") != 1 || b.Int("flag") != 1 {
		t.Fatalf("buy a ticket: %v", b)
	}
	c.send(26709, proto.Message{"boss_id": 1})
	if b := c.waitFor(26709); b.Int("code") != 1 {
		t.Fatalf("sweep: %v", b)
	}
	if b := c.waitFor(26708); b.Int("all_dps") != last {
		t.Fatalf("sweep result: %v", b)
	}
	c.waitFor(26700)
	c.send(26710, proto.Message{"boss_id": 1, "start_num": 1, "end_num": 100})
	rk := c.waitFor(26710)
	if rk.Int("rank") != 1 || rk.Int("mydps") != 2*last || rk.Int("rank_per") != 1 || len(rk.Arr("rank_list")) != 1 || rk.Arr("rank_list")[0].Str("name") != "开学人" {
		t.Fatalf("26710: %v", rk)
	}
	// the round is over: the next one starts clean and the first place gets the top reward by mail
	e.setNow(time.Unix(start+7*86400+3600, 0))
	c.send(26700, proto.Message{})
	p := c.waitFor(26700)
	if p.Int("boss_flag") != 0 || p.Int("order") != 1 || p.Int("dps") != 0 || p.Int("round") != 2 || p.Int("boss_hp_per") != 1000 {
		t.Fatalf("next round: %v", p)
	}
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mails {
		if m.Subject == "开学季世界Boss排名奖励" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no rank mail: %+v", mails)
	}
}
