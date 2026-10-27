package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestCrossArena(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone)) // a Tuesday
	ca, a := e.enter("a", "甲")
	cb, b := e.enter("b", "乙")
	for _, r := range []*store.Role{a, b} {
		if err := e.st.SetRoleLevel(r.RID, int(caMinLev)); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.GrantStarterPartners(r.RID, []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10502, Star: 5}, {BID: 10503, Star: 5}, {BID: 10504, Star: 5}}); err != nil {
			t.Fatal(err)
		}
	}
	cb.send(25600, proto.Message{})
	cb.waitFor(25600)

	ca.send(25610, proto.Message{})
	if r := ca.waitFor(25610); r.Int("code") != 1 {
		t.Fatalf("open on a Tuesday: %v", r)
	}
	// worshipping the honoured pays the like reward once a day
	before, _ := e.st.RoleAssets(a.RID)
	ca.send(10316, proto.Message{"type": worshipCrossArena, "rid": a.RID, "srv_id": a.SrvID, "idx": 1})
	if r := ca.waitFor(10316); r.Int("code") != 1 {
		t.Fatalf("worship: %v", r)
	}
	after, _ := e.st.RoleAssets(a.RID)
	if like := caCfg.pair("like_reward"); after[like[0]]-before[like[0]] != like[1] {
		t.Fatalf("like reward: %v -> %v", before, after)
	}
	ca.send(10316, proto.Message{"type": worshipCrossArena, "rid": a.RID, "srv_id": a.SrvID, "idx": 1})
	if r := ca.waitFor(10316); r.Int("code") != 0 {
		t.Fatalf("worship twice: %v", r)
	}
	ca.send(25600, proto.Message{})
	if r := ca.waitFor(25600); r.Int("score") != caInitScore || r.Int("rank") != 0 {
		t.Fatalf("25600: %v", r)
	}
	ca.send(25601, proto.Message{})
	list := ca.waitFor(25601).Arr("f_list")
	if len(list) != caOpps || list[0].Int("rid") != b.RID {
		t.Fatalf("opponents: %v", list)
	}
	if list[1].Int("rid") < caRobotRID || list[1].Int("power") == 0 {
		t.Fatalf("a robot fills the list: %v", list[1])
	}
	ca.send(25602, proto.Message{"rid": list[0].Int("rid"), "srv_id": b.SrvID})
	if v := ca.waitFor(25602); len(v.Arr("order_list")) != caTeams || len(v.Arr("order_list")[0].Arr("p_list")) == 0 {
		t.Fatalf("25602: %v", v)
	}

	// the attacking teams: two with heroes
	heroes, _ := e.st.PartnersOf(a.RID)
	team := func(order int64, ids ...int64) proto.Message {
		pos := []proto.Message{}
		for i, id := range ids {
			pos = append(pos, proto.Message{"pos": i + 1, "id": id})
		}
		return proto.Message{"order": order, "formation_type": 1, "pos_info": pos, "hallows_id": 0, "is_hidden": 0}
	}
	ca.send(25604, proto.Message{"type": 1, "formations": []proto.Message{team(1, heroes[0].PartnerID, heroes[1].PartnerID)}})
	if r := ca.waitFor(25604); r.Int("code") != 0 {
		t.Fatalf("one team only: %v", r)
	}
	ca.send(25604, proto.Message{"type": 1, "formations": []proto.Message{team(1, heroes[0].PartnerID, heroes[1].PartnerID), team(2, heroes[2].PartnerID, heroes[3].PartnerID)}})
	if r := ca.waitFor(25604); r.Int("code") != 1 {
		t.Fatalf("save: %v", r)
	}
	ca.send(25605, proto.Message{"type": 1})
	if f := ca.waitFor(25605).Arr("formations"); len(f) != caTeams || len(f[1].Arr("pos_info")) != 2 {
		t.Fatalf("25605: %v", f)
	}

	// no tickets, no fight
	ca.send(25606, proto.Message{"rid": list[0].Int("rid"), "srv_id": b.SrvID})
	if r := ca.waitFor(25606); r.Int("code") != 0 {
		t.Fatalf("without tickets: %v", r)
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: []store.Attachment{{Kind: store.AttachItem, ItemID: 10103, Num: 9}}}, a.RID)
	ca.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	ca.waitFor(10801)
	ca.waitFor(10500)

	// a robot, then the real role
	ca.send(25606, proto.Message{"rid": list[1].Int("rid"), "srv_id": a.SrvID})
	if r := ca.waitFor(25606); r.Int("code") != 1 {
		t.Fatalf("fight: %v", r)
	}
	ca.waitFor(20000)
	if intro := ca.waitFor(20027); intro.Int("combat_type") != caCombat {
		t.Fatalf("intro: %v", intro)
	}
	ca.send(20062, proto.Message{})
	ca.waitFor(20062)
	res := ca.waitFor(25611)
	if res.Int("result") != 1 && res.Int("result") != 2 {
		t.Fatalf("result: %v", res)
	}
	won := res.Int("result") == 1
	if won && (res.Int("get_score") <= 0 || res.Int("score") != caInitScore+res.Int("get_score")) {
		t.Fatalf("score of a win: %v", res)
	}
	if !won && res.Int("score") != caInitScore {
		t.Fatalf("a loss moved the score: %v", res)
	}
	ca.send(25607, proto.Message{})
	if r := ca.waitFor(25607); r.Int("had_combat_num") != 1 {
		t.Fatalf("25607: %v", r)
	}
	ca.send(25616, proto.Message{"type": 1})
	logs := ca.waitFor(25616).Arr("arena_cluster_log")
	if len(logs) != 1 || logs[0].Int("combat_type") != 1 || logs[0].Int("ret") != res.Int("result") {
		t.Fatalf("25616: %v", logs)
	}
	ca.send(25617, proto.Message{"type": 1, "id": logs[0].Int("id")})
	if rep := ca.waitFor(25617).Arr("arena_replay_infos"); len(rep) == 0 || rep[0].Int("id") == 0 {
		t.Fatalf("25617: %v", rep)
	}
	// the cards of a win
	ca.send(25612, proto.Message{})
	cards := ca.waitFor(25612).Arr("card_info")
	if won {
		if len(cards) != 3 || cards[0].Int("status") != 0 {
			t.Fatalf("cards of a win: %v", cards)
		}
		ca.send(25613, proto.Message{"pos": 2})
		if r := ca.waitFor(25613); r.Int("code") != 1 {
			t.Fatalf("flip: %v", r)
		}
		after := ca.waitFor(25612).Arr("card_info")
		if after[1].Int("status") != 2 || after[0].Int("status") != 1 || len(after[0].Arr("val")) != 1 {
			t.Fatalf("after the first card: %v", after)
		}
	} else if len(cards) != 0 {
		t.Fatalf("cards after a loss: %v", cards)
	}
	// the refresh waits a few seconds
	ca.send(25609, proto.Message{})
	if r := ca.waitFor(25609); r.Int("code") != 1 {
		t.Fatalf("refresh: %v", r)
	}
	ca.waitFor(25601)
	ca.send(25609, proto.Message{})
	if r := ca.waitFor(25609); r.Int("code") != 0 {
		t.Fatalf("refresh twice: %v", r)
	}
	// the rank list holds who fought
	ca.send(25615, proto.Message{})
	if r := ca.waitFor(25615); won && (len(r.Arr("arena_cluster_role")) != 1 || r.Int("rank") != 1) {
		t.Fatalf("25615: %v", r)
	}
	// Wednesday: closed
	e.setNow(time.Date(2026, 10, 28, 12, 0, 0, 0, dayZone))
	ca.send(25610, proto.Message{})
	if r := ca.waitFor(25610); r.Int("code") != 0 {
		t.Fatalf("closed on a Wednesday: %v", r)
	}
}

func TestCrossArenaSeasonTurn(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone))
	ca, a := e.enter("a", "甲")
	if err := e.st.SetRoleLevel(a.RID, int(caMinLev)); err != nil {
		t.Fatal(err)
	}
	ca.send(25600, proto.Message{})
	ca.waitFor(25600)
	ca.send(25610, proto.Message{}) // a second command: the board of the first is saved by now
	ca.waitFor(25610)
	svc := &caSvc{st: e.st}
	b, err := svc.board(a.SrvID, time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone))
	if err != nil {
		t.Fatal(err)
	}
	r := b.Roles[a.RID]
	r.Fought, r.Score = true, 1300
	if err := e.st.SaveGlobal(a.SrvID, caModule, b); err != nil {
		t.Fatal(err)
	}
	// next Monday: the season turns, the first place is honoured and paid by mail
	e.setNow(time.Date(2026, 11, 2, 12, 0, 0, 0, dayZone))
	ca.send(25614, proto.Message{})
	h := ca.waitFor(25614).Arr("rank_list")
	if len(h) != 1 || h[0].Int("rid") != a.RID || h[0].Int("rank") != 1 {
		t.Fatalf("honour: %v", h)
	}
	// worshipping the honoured pays the like reward once a day
	before, _ := e.st.RoleAssets(a.RID)
	ca.send(10316, proto.Message{"type": worshipCrossArena, "rid": a.RID, "srv_id": a.SrvID, "idx": 1})
	if r := ca.waitFor(10316); r.Int("code") != 1 {
		t.Fatalf("worship: %v", r)
	}
	after, _ := e.st.RoleAssets(a.RID)
	if like := caCfg.pair("like_reward"); after[like[0]]-before[like[0]] != like[1] {
		t.Fatalf("like reward: %v -> %v", before, after)
	}
	ca.send(10316, proto.Message{"type": worshipCrossArena, "rid": a.RID, "srv_id": a.SrvID, "idx": 1})
	if r := ca.waitFor(10316); r.Int("code") != 0 {
		t.Fatalf("worship twice: %v", r)
	}
	ca.send(25600, proto.Message{})
	if r := ca.waitFor(25600); r.Int("score") != caInitScore || r.Int("rank") != 0 {
		t.Fatalf("a new season starts over: %v", r)
	}
	role, _ := e.st.RoleByID(a.RID)
	mails, err := e.st.MailsForRole(role)
	if err != nil || len(mails) == 0 || mails[0].Subject != "跨服竞技场排名奖励" {
		t.Fatalf("rank mail: %v %v", mails, err)
	}
}

func TestCrossArenaBelowTheLevel(t *testing.T) {
	e := newEnv(t, time.Minute)
	e.setNow(time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone))
	c, _ := e.enter("a", "甲")
	c.send(25610, proto.Message{})
	if r := c.waitFor(25610); r.Int("code") != 0 {
		t.Fatalf("25610: %v", r)
	}
	c.send(25607, proto.Message{})
	if r := c.waitFor(25607); r.Int("had_combat_num") != 0 {
		t.Fatalf("25607: %v", r)
	}
	c.send(25618, proto.Message{})
	if r := c.waitFor(25618); r.Int("combat_code") != 0 || r.Int("reward_code") != 0 {
		t.Fatalf("25618: %v", r)
	}
	c.send(25605, proto.Message{"type": 2})
	if f := c.waitFor(25605).Arr("formations"); len(f) != caTeams {
		t.Fatalf("25605: %v", f)
	}
	c.send(25606, proto.Message{"rid": 1, "srv_id": "symlf_1"})
	if r := c.waitFor(25606); r.Int("code") != 0 {
		t.Fatalf("25606: %v", r)
	}
	c.send(25604, proto.Message{"type": 1})
	if r := c.waitFor(25604); r.Int("code") != 0 {
		t.Fatalf("25604: %v", r)
	}
	// nothing of it is kept: the role is not on the board
	b, err := (&caSvc{st: e.st}).board("symlf_1", time.Date(2026, 10, 27, 12, 0, 0, 0, dayZone))
	if err != nil || len(b.Roles) != 0 {
		t.Fatalf("board: %v %v", b, err)
	}
}
