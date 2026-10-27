package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestRankSelfIsLive(t *testing.T) {
	e := newEnv(t, time.Hour)
	e.setNow(actNow)
	ca, a := e.enter("la", "甲")
	_, b := e.enter("lb", "乙")
	atDungeon(t, e, b.RID, 10050, false)
	ask := func() proto.Message {
		ca.send(12900, proto.Message{"type": rankDrama, "start": 1, "num": 100})
		return ca.recv(12900)
	}
	first := ask() // builds the snapshot
	if first.Int("my_idx") != 2 {
		t.Fatalf("second in the snapshot: %v", first)
	}
	atDungeon(t, e, a.RID, 10100, false)
	// the list is the snapshot, but my own place counts my progress at once
	r := ask()
	if r.Int("time") != first.Int("time") || r.Arr("rank_list")[0].Int("rid") != b.RID {
		t.Fatalf("the list should still be the snapshot: %v", r)
	}
	if r.Int("my_idx") != 1 || r.Int("my_val1") != 10100 {
		t.Fatalf("my place after my progress: %v", r)
	}
}

func TestSevenDayRanking(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(actNow)
	if err := e.st.UpsertServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1,
		OpenTime: actNow.Add(-2 * 24 * time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	ca, a := e.enter("da", "甲")
	cb, b := e.enter("db", "乙")
	start := dayStart(actNow.Add(-2 * 24 * time.Hour)).Unix()
	end := start + 12*86400 - 1
	ca.send(22700, proto.Message{"is_cluster": 0})
	r := ca.waitFor(22700)
	if l := r.Arr("rank_list"); len(l) != 7 || l[0].Int("end_time") != end {
		t.Fatalf("22700: %v", r)
	}
	ca.send(22700, proto.Message{"is_cluster": 1})
	if r := ca.waitFor(22700); len(r.Arr("rank_list")) != 0 {
		t.Fatalf("cross server: %v", r)
	}
	atDungeon(t, e, a.RID, 10050, false)
	cb.send(22701, proto.Message{"id": 1})
	r = cb.waitFor(22701)
	if l := r.Arr("rank_list"); len(l) != 2 || l[0].Int("rid") != a.RID || r.Int("rank") != 2 || r.Int("end_time") != end {
		t.Fatalf("22701: %v", r)
	}
	// the quest of the campaign: not yet
	ca.send(22703, proto.Message{})
	qs := ca.waitFor(22703).Arr("feat_list")
	if len(qs) != 14 {
		t.Fatalf("22703: %d quests", len(qs))
	}
	find := func(list []proto.Message, id int64) proto.Message {
		for _, q := range list {
			if q.Int("id") == id {
				return q
			}
		}
		return nil
	}
	if q := find(qs, 1001); q.Int("finish") != questOpen {
		t.Fatalf("1001: %v", q)
	}
	ca.send(22704, proto.Message{"quest_id": 1001})
	if r := ca.waitFor(22704); r.Int("result") != 0 {
		t.Fatalf("an unfinished quest: %v", r)
	}
	atDungeon(t, e, a.RID, 10300, true)
	ca.send(22703, proto.Message{})
	if q := find(ca.waitFor(22703).Arr("feat_list"), 1001); q.Int("finish") != questDone {
		t.Fatalf("1001 done: %v", q)
	}
	ca.send(22704, proto.Message{"quest_id": 1001})
	if r := ca.waitFor(22704); r.Int("result") != 1 {
		t.Fatalf("take: %v", r)
	}
	ca.send(22704, proto.Message{"quest_id": 1001})
	if r := ca.waitFor(22704); r.Int("result") != 0 {
		t.Fatalf("twice: %v", r)
	}
	// the end: the rankings freeze and the places are paid by mail
	e.setNow(time.Unix(end+1, 0))
	ca.send(22700, proto.Message{"is_cluster": 0})
	ca.waitFor(22700)
	mails, err := e.st.MailsForRole(&store.Role{RID: a.RID, SrvID: a.SrvID, Name: a.Name, CreatedAt: a.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	var got int
	for _, m := range mails {
		if m.Subject == "开服冲榜·剧情冲刺奖励" {
			got++
		}
	}
	// the first place of the campaign needs dungeon 10420: a's progress is short of it, so a is paid for no first place
	if got != 0 {
		t.Fatalf("the first place under the limit was paid: %d", got)
	}
	var bGot int
	if mails, err = e.st.MailsForRole(&store.Role{RID: b.RID, SrvID: b.SrvID, Name: b.Name, CreatedAt: b.CreatedAt}); err != nil {
		t.Fatal(err)
	}
	for _, m := range mails {
		if m.Subject == "开服冲榜·剧情冲刺奖励" {
			bGot++
		}
	}
	if bGot != 1 {
		t.Fatalf("the second place was not paid: %d", bGot)
	}
	// frozen: later progress does not move the list
	atDungeon(t, e, b.RID, 10300, true)
	cb.send(22701, proto.Message{"id": 1})
	if r := cb.waitFor(22701); r.Arr("rank_list")[0].Int("rid") != a.RID || r.Int("rank") != 2 {
		t.Fatalf("frozen list: %v", r)
	}
	// and a settled window pays no quest
	ca.send(22704, proto.Message{"quest_id": 1002})
	if r := ca.waitFor(22704); r.Int("result") != 0 {
		t.Fatalf("quest after the end: %v", r)
	}
}
