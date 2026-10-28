package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// the 21st of October 2026 is a Wednesday, the third day of the arena season
func champAt(day, hour, min, sec int) time.Time {
	return time.Date(2026, 10, day, hour, min, sec, 0, dayZone)
}

var champT0 = champAt(21, 21, 5, 0)

// champRole makes a role with heroes and a place in the arena, which is what the match asks for.
func (e *env) champRole(account, name string) (*client, *store.Role) {
	e.t.Helper()
	c, role := e.enter(account, name)
	if _, err := e.st.GrantStarterPartners(role.RID, starterSeeds(role.Sex)); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.st.ArenaPlayerOf(role.RID, arenaInitScore, e.clk.Load()/1e9); err != nil {
		e.t.Fatal(err)
	}
	return c, role
}

func TestChampionCalendar(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(champAt(20, 12, 0, 0)) // Tuesday: nothing is open
	c, _ := e.champRole("ch0", "冠军客")
	c.send(20251, proto.Message{})
	if r := c.waitFor(20251); r.Int("rank") != 0 || r.Int("can_bet") != 0 {
		t.Fatalf("a standing before any match: %v", r)
	}
	if b := c.waitFor(20250); b.Int("step") != 0 {
		t.Fatalf("a match on a Tuesday: %v", b)
	}
	// the day of the match: the selection is announced and counts down to 21:05
	e.setNow(champAt(21, 12, 0, 0))
	c.send(20251, proto.Message{})
	c.waitFor(20251)
	b := c.waitFor(20250)
	if b.Int("step") != champStepScore || b.Int("step_status") != 0 || b.Int("step_status_time") != champT0.Unix() {
		t.Fatalf("the announcement: %v", b)
	}
	// asking again does not push the same state again, or the client would ask back for ever
	c.send(20251, proto.Message{})
	c.waitFor(20251)
	c.send(20252, proto.Message{})
	c.waitFor(20252)
	if c.sawWithin(20250, 300*time.Millisecond) {
		t.Fatalf("the same state was pushed twice")
	}
}

// sawWithin says whether a message of cmd comes in the next d, reading what else comes meanwhile.
func (c *client) sawWithin(cmd uint16, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		c.c.SetReadDeadline(end)
		fr, err := proto.ReadFrame(c.br)
		if err != nil {
			return false
		}
		if fr.Command == cmd {
			return true
		}
	}
	return false
}

func TestChampionSelectionBetAndGroups(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(champT0.Add(10 * time.Second))
	c, role := e.champRole("ch1", "冠军客")

	// round 1, getting ready: the role is in, ranked within 128, with its chips and the state pushed
	c.send(20251, proto.Message{})
	info := c.waitFor(20251)
	if r := info.Int("rank"); r < 1 || r > champSize || info.Int("can_bet") != 60 {
		t.Fatalf("standing: %v", info)
	}
	base := c.waitFor(20250)
	if base.Int("step") != champStepScore || base.Int("step_status") != 1 || base.Int("round") != 1 || base.Int("round_status") != 1 {
		t.Fatalf("base: %v", base)
	}
	c.send(20252, proto.Message{})
	mine := c.waitFor(20252)
	if mine.Int("a_rid") != role.RID || len(mine.Arr("a_plist")) == 0 || len(mine.Arr("b_plist")) == 0 || mine.Int("ret") != 0 || mine.Int("replay_id") != 0 {
		t.Fatalf("my match before the fight: %v", mine)
	}

	// betting: only while betting, once a round, not more than the chips
	c.send(20254, proto.Message{"bet_type": 1, "bet_val": 10})
	if r := c.waitFor(20254); r.Int("code") != 0 {
		t.Fatalf("a bet while getting ready: %v", r)
	}
	e.setNow(champT0.Add(60 * time.Second))
	c.send(20253, proto.Message{})
	g := c.waitFor(20253)
	if g.Int("a_rid") == 0 || g.Int("a_bet_ratio") != 2000 || g.Int("b_bet_ratio") != 2000 || g.Int("bet_type") != 0 {
		t.Fatalf("the match to bet on: %v", g)
	}
	c.send(20254, proto.Message{"bet_type": 3, "bet_val": 10})
	if r := c.waitFor(20254); r.Int("code") != 0 {
		t.Fatalf("a bet on nobody: %v", r)
	}
	c.send(20254, proto.Message{"bet_type": 1, "bet_val": 61})
	if r := c.waitFor(20254); r.Int("code") != 0 {
		t.Fatalf("a bet over the chips: %v", r)
	}
	c.send(20254, proto.Message{"bet_type": 1, "bet_val": 40})
	if r := c.waitFor(20254); r.Int("code") != 1 || r.Int("can_bet") != 20 || r.Int("bet_type") != 1 || r.Int("bet_val") != 40 {
		t.Fatalf("a bet: %v", r)
	}
	pool := c.waitFor(20257)
	if pool.Int("a_bet") != 40 || pool.Int("a_bet_ratio") >= 2000 || pool.Int("b_bet_ratio") <= 2000 {
		t.Fatalf("the pool after a bet: %v", pool)
	}
	c.send(20254, proto.Message{"bet_type": 2, "bet_val": 10})
	if r := c.waitFor(20254); r.Int("code") != 0 {
		t.Fatalf("a second bet in a round: %v", r)
	}
	c.send(20253, proto.Message{})
	if g := c.waitFor(20253); g.Int("bet_type") != 1 || g.Int("bet_val") != 40 {
		t.Fatalf("my bet shown: %v", g)
	}

	// the fight begins: the replay is there, the result not yet
	e.setNow(champT0.Add(130 * time.Second))
	c.send(20252, proto.Message{})
	mine = c.waitFor(20252)
	if mine.Int("replay_id") == 0 || mine.Int("ret") != 0 {
		t.Fatalf("my match while it is fought: %v", mine)
	}
	replay := mine.Int("replay_id")
	// it ended: the result shows, the log has it
	e.setNow(champT0.Add(310 * time.Second))
	c.send(20252, proto.Message{})
	mine = c.waitFor(20252)
	if mine.Int("round") != 2 && mine.Int("ret") == 0 && mine.Int("replay_id") == replay {
		t.Fatalf("my match after it: %v", mine)
	}
	c.send(20258, proto.Message{})
	log := c.waitFor(20258).Arr("list")
	if len(log) == 0 || log[0].Int("a_rid") != role.RID || log[0].Int("ret") == 0 {
		t.Fatalf("log: %v", log)
	}
	if won := log[0].Int("ret") == 1; won != (log[0].Int("score") == champWinPoints) {
		t.Fatalf("points of the win: %v", log[0])
	}
	c.send(20255, proto.Message{})
	bets := c.waitFor(20255).Arr("list")
	if len(bets) != 1 || bets[0].Int("bet") != 40 || bets[0].Int("target") != 1 || bets[0].Int("ret") == 0 {
		t.Fatalf("my bets: %v", bets)
	}
	if bets[0].Int("ret") == 1 && bets[0].Int("get_bet") < 40*champOddsMin/1000 {
		t.Fatalf("a won bet pays: %v", bets[0])
	}

	// the selection is over: the best 32 stand in four groups of eight
	e.setNow(champT0.Add(time.Duration(champOffsets[champScoreRnds]+10) * time.Second))
	c.send(20260, proto.Message{})
	groups := c.waitFor(20260).Arr("list")
	if len(groups) != champGroups {
		t.Fatalf("groups: %v", groups)
	}
	seen := map[int64]bool{}
	for _, g := range groups {
		slots := g.Arr("pos_list")
		if len(slots) != 8 {
			t.Fatalf("a group at the start: %v", g)
		}
		for _, sl := range slots {
			if seen[sl.Int("rid")] {
				t.Fatalf("a role twice: %v", sl)
			}
			seen[sl.Int("rid")] = true
		}
	}
	// a match of a group, by its place
	c.send(20263, proto.Message{"group": 1, "pos": 1})
	if m := c.waitFor(20263); m.Int("step") != champStep32 || m.Int("round") != 1 || m.Int("group") != 1 || m.Int("a_rid") == 0 || m.Int("b_rid") == 0 {
		t.Fatalf("match at 1/1: %v", m)
	}
	c.send(20262, proto.Message{})
	if g := c.waitFor(20262); g.Int("group") == 0 || g.Int("pos")%2 != 1 {
		t.Fatalf("the match to bet on: %v", g)
	}
	// the chips of the next round came
	c.send(20251, proto.Message{})
	if info := c.waitFor(20251); info.Int("can_bet") < 60 {
		t.Fatalf("chips of the next round: %v", info)
	}
}

func TestChampionToTheEnd(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(champT0.Add(10 * time.Second))
	c, role := e.champRole("ch2", "冠军客")
	c.send(20251, proto.Message{})
	c.waitFor(20251)
	c.waitFor(20250)
	c.send(20254, proto.Message{"bet_type": 1, "bet_val": 10}) // refused: not the time; keeps chips
	c.waitFor(20254)

	end := champT0.Add(time.Duration(champLength()+30) * time.Second)
	e.setNow(end)
	c.send(20281, proto.Message{})
	rank := c.waitFor(20281)
	list := rank.Arr("rank_list")
	if len(list) != champSize || rank.Int("rank") < 1 || rank.Int("power") == 0 {
		t.Fatalf("ranking: %d %v", len(list), rank)
	}
	places := map[int64]bool{}
	for i, r := range list {
		if r.Int("rank") != int64(i+1) || places[r.Int("rank")] {
			t.Fatalf("places are 1..128 once each: %d %v", i, r)
		}
		places[r.Int("rank")] = true
	}
	c.send(20261, proto.Message{})
	if top := c.waitFor(20261).Arr("pos_list"); len(top) != 7 || top[6].Int("pos") != 7 || top[6].Int("rid") != list[0].Int("rid") {
		t.Fatalf("the last four and the champion: %v (first of the ranking %v)", top, list[0])
	}
	c.send(20260, proto.Message{})
	for _, g := range c.waitFor(20260).Arr("list") {
		if slots := g.Arr("pos_list"); len(slots) != 14 || slots[13].Int("ret") == 0 || slots[0].Int("ret") == 0 {
			t.Fatalf("a finished group: %v", g)
		}
	}
	// the match is over, the statues and the push
	c.send(20280, proto.Message{})
	if st := c.waitFor(20280).Arr("rank_list"); len(st) != 3 || st[0].Int("rank") != 1 || st[0].Int("rid") != list[0].Int("rid") {
		t.Fatalf("statues: %v", st)
	}
	c.send(20251, proto.Message{})
	info := c.waitFor(20251)
	b := c.waitFor(20250)
	if b.Int("step") != champStep4 || b.Int("step_status") != 2 {
		t.Fatalf("base after the match: %v", b)
	}
	// the place, shown once
	c.send(20256, proto.Message{})
	res := c.waitFor(20256)
	if res.Int("rank") < 1 || res.Int("cnum") < champScoreRnds {
		t.Fatalf("result: %v", res)
	}
	c.send(20256, proto.Message{})
	if c.sawWithin(20256, 300*time.Millisecond) {
		t.Fatalf("the result was shown twice")
	}
	if info.Int("best_rank") != res.Int("rank") {
		t.Fatalf("best rank: %v / %v", info, res)
	}
	// the reward went by mail: the place's items and the chips as 竞技声望
	mails, err := e.st.MailsForRole(role)
	if err != nil {
		t.Fatal(err)
	}
	got := false
	for _, m := range mails {
		if m.Subject == "冠军赛奖励" && len(m.Attachments) > 0 {
			got = true
		}
	}
	if !got {
		t.Fatalf("no reward mail: %v", mails)
	}
	// and a day after, the match shows as not open
	e.setNow(champAt(23, 12, 0, 0))
	c.send(20251, proto.Message{})
	c.waitFor(20251)
	if b := c.waitFor(20250); b.Int("step") != 0 {
		t.Fatalf("a match that is long over: %v", b)
	}
}

func TestChampionNoRolesNoMatch(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(champT0.Add(10 * time.Second))
	c, _ := e.enter("ch3", "没进竞技场") // a role that never opened the arena
	c.send(20251, proto.Message{})
	if r := c.waitFor(20251); r.Int("rank") != 0 {
		t.Fatalf("a rank for nobody: %v", r)
	}
	c.send(20281, proto.Message{})
	if r := c.waitFor(20281); len(r.Arr("rank_list")) != 0 {
		t.Fatalf("a ranking of nothing: %v", r)
	}
}

func TestChampionTickPushesWhenItMovesOn(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	e.setNow(champT0.Add(10 * time.Second))
	c, _ := e.champRole("ch4", "冠军客")
	e.champ.tick()
	b := c.waitFor(20250)
	if b.Int("round") != 1 || b.Int("round_status") != 1 {
		t.Fatalf("first push: %v", b)
	}
	e.champ.tick() // nothing new
	e.setNow(champT0.Add(125 * time.Second))
	e.champ.tick()
	b = c.waitFor(20250)
	if b.Int("round_status") != 3 || b.Int("flag") != 2 {
		t.Fatalf("the fight begins, the clients play it: %v", b)
	}
	e.setNow(champT0.Add(time.Duration(champLength()+30) * time.Second))
	e.champ.tick()
	top := c.waitFor(20282)
	if len(top.Arr("rank_list")) != 3 {
		t.Fatalf("the three best are shown when it ends: %v", top)
	}
}

func TestCrossChampion(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	// the 25th of October 2026 is a Sunday: the cross match starts at 12:00
	sunday := champAt(25, 12, 0, 0)
	e.setNow(sunday.Add(10 * time.Second))
	c, role := e.champRole("cc1", "跨服客")
	board := &caBoard{Roles: map[int64]*caRole{role.RID: {RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: 120, Score: 1500, Fought: true, Season: 1}}}
	if err := e.st.SaveGlobal(role.SrvID, caModule, board); err != nil {
		t.Fatal(err)
	}
	c.send(26200, proto.Message{})
	b := c.waitFor(26200)
	if b.Int("step") != champStepScore || b.Int("is_open") != 1 || b.Int("start_time") != dayStart(sunday).Unix() {
		t.Fatalf("base: %v", b)
	}
	// the entrants are the cross arena's best, robots after them, up to 128; the local match is not touched
	c.send(26214, proto.Message{})
	r := c.waitFor(26214)
	list := r.Arr("rank_list")
	if len(list) != champSize || r.Int("rank") == 0 {
		t.Fatalf("ranking of %d: rank %d", len(list), r.Int("rank"))
	}
	var me bool
	for _, e := range list {
		if e.Int("rid") == role.RID {
			me = true
		}
	}
	if !me {
		t.Fatal("the cross arena's best is not in the match")
	}
	c.send(26201, proto.Message{})
	if r := c.waitFor(26201); r.Int("rank") == 0 {
		t.Fatalf("own standing: %v", r)
	}
	if ev := (&champState{}); true {
		if ok, _ := e.st.LoadGlobal(role.SrvID, "crosschamp", ev); !ok || ev.Ev == nil || len(ev.Ev.Entries) != champSize {
			t.Fatalf("state: %v %+v", ok, ev.Ev)
		}
		local := &champState{}
		if ok, _ := e.st.LoadGlobal(role.SrvID, champModule, local); ok && local.Ev != nil {
			t.Fatal("the local match was started by the cross one")
		}
	}
}
