package game

import (
	"2droundserver/internal/store"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

var settlementKeys = []string{atModule, eliteModule, caModule, amModule, drModule, guildWarModule, champModule, "crosschamp"}

func seedSettlement(t *testing.T, key string, due bool) (*env, http.Handler, *store.Role) {
	t.Helper()
	e := newEnv(t, 100*time.Hour)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, dayZone)
	if key == atModule {
		now = now.AddDate(0, 0, 5)
	}
	e.setNow(now)
	c, role := e.enter("settle-fixture", "补结算")
	chargeBarrier(c)
	sv, _ := e.st.Server(role.SrvID)
	sv.OpenTime = now.Add(-24 * time.Hour).Unix()
	if err := e.st.UpsertServer(*sv); err != nil {
		t.Fatal(err)
	}
	var state any
	switch key {
	case atModule:
		k, _, _ := atSeason(now)
		season, settled := k, k-7*86400
		if due {
			season, settled = k-7*86400, k-14*86400
		}
		state = &atBoard{Settled: settled, Teams: map[int64]*atTeam{1: {ID: 1, Season: season, Signed: true, Score: 2000, Do: 8, Members: []atMember{{RID: role.RID}}}}}
	case amModule:
		k, _ := amSeason(now)
		if due {
			k, _ = amSeason(now.AddDate(0, 0, -7))
		}
		state = &amBoard{Roles: map[int64]*amRole{role.RID: {RID: role.RID, Season: k, Score: 2000, Do: 8}}}
	case caModule:
		k, _, _ := caSeason(now)
		if due {
			k, _, _ = caSeason(now.AddDate(0, 0, -7))
		}
		state = &caBoard{Roles: map[int64]*caRole{role.RID: {RID: role.RID, SrvID: role.SrvID, Season: k, Score: 2000, Fought: true}}}
	case eliteModule:
		if due {
			sv.OpenTime = now.Add(-10 * 24 * time.Hour).Unix()
		}
		if err := e.st.UpsertServer(*sv); err != nil {
			t.Fatal(err)
		}
		state = &eliteWorld{Done: 0}
		if err := e.st.SaveState(role.RID, eliteModule, &eliteRole{Lev: 30, Score: 3000}); err != nil {
			t.Fatal(err)
		}
	case drModule:
		if due {
			sv.OpenTime = now.Add(-20 * 24 * time.Hour).Unix()
		}
		if err := e.st.UpsertServer(*sv); err != nil {
			t.Fatal(err)
		}
		if err := e.st.SetRoleLevel(role.RID, 200); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.ArenaPlayerOf(role.RID, 1000, now.Unix()); err != nil {
			t.Fatal(err)
		}
		state = &drWorld{}
	case guildWarModule:
		day := dayStart(now)
		if due {
			day = day.AddDate(0, 0, -1)
		}
		side := gwSide{GID: 1, SrvID: role.SrvID, Name: "测试公会", People: []gwPerson{{RID: role.RID, Name: role.Name}}, Score: map[int64]int64{role.RID: 100}, Stars: map[int64]int64{role.RID: 3}, Used: map[int64]int64{}}
		state = &gwWar{Key: dayKey(day), Pairs: []*gwPair{{Sides: [2]gwSide{side, {GID: 2, SrvID: role.SrvID, Score: map[int64]int64{}, Stars: map[int64]int64{}}}}}}
	case champModule, "crosschamp":
		t0 := now.Unix()
		if due {
			t0 = now.Unix() - champLength() - 60
		}
		ev := &champEvent{Key: dayKey(time.Unix(t0, 0)), T0: t0, Entries: []champEntrant{{arenaOpp: arenaOpp{RID: role.RID, SrvID: role.SrvID, Name: role.Name}, Rank: 1}}, Bets: map[int64][]champBet{}, Chips: map[int64]*champChips{role.RID: {Have: 100}}, Seen: map[int64]bool{}}
		// The game's own rounds have finished; only its normal final payout is pending.
		for i := 0; i < champRounds; i++ {
			ev.Paired[i], ev.Locked[i], ev.Applied[i] = true, true, true
		}
		state = &champState{Ev: ev}
	}
	if err := e.st.SaveGlobal(role.SrvID, key, state); err != nil {
		t.Fatal(err)
	}
	return e, opsTestHandler(e), role
}
func settlementCounts(t *testing.T, e *env, role *store.Role) (int, []byte) {
	t.Helper()
	_, n, err := e.st.ListMails(1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := e.st.RoleAssets(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	items, err := e.st.RoleItems(role.RID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal([]any{assets, items})
	return n, b
}
func doSettlement(t *testing.T, h http.Handler, key string) adminSettleResult {
	t.Helper()
	w := opsRequest(h, "POST", "/ops/season/settle?srv=symlf_1&key="+key, AdminOperation{Reason: "补做已到期结算", AdminID: 1, AdminName: "super", IP: "127.0.0.1"})
	if w.Code != 200 {
		t.Fatalf("settle %s: %d %s", key, w.Code, w.Body)
	}
	var out adminSettleResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAdminSettlementEveryAuthorizedModuleIsDueOnlyAndIdempotent(t *testing.T) {
	for _, key := range settlementKeys {
		t.Run(key+"/ongoing", func(t *testing.T) {
			e, h, role := seedSettlement(t, key, false)
			before := globalBytes(t, e.st, role.SrvID)
			n, assets := settlementCounts(t, e, role)
			if out := doSettlement(t, h, key); out.Settled || out.Reason != "not_due" {
				t.Fatalf("ongoing season advanced: %+v", out)
			}
			if after := globalBytes(t, e.st, role.SrvID); after[key] != before[key] {
				t.Fatal("not_due changed state")
			}
			n2, a2 := settlementCounts(t, e, role)
			if n != n2 || !bytes.Equal(assets, a2) {
				t.Fatal("not_due rewarded")
			}
		})
		t.Run(key+"/due", func(t *testing.T) {
			due, dh, dr := seedSettlement(t, key, true)
			old, oldAssets := settlementCounts(t, due, dr)
			if out := doSettlement(t, dh, key); !out.Settled || out.Reason != "settled" {
				t.Fatalf("due season not settled: %+v", out)
			}
			first, firstAssets := settlementCounts(t, due, dr)
			if first <= old {
				t.Fatal("fixture did not exercise actual reward mail")
			}
			if !bytes.Equal(oldAssets, firstAssets) {
				t.Fatal("settlement altered assets directly instead of original mail flow")
			}
			if out := doSettlement(t, dh, key); out.Settled || out.Reason != "already" {
				t.Fatalf("repeat result: %+v", out)
			}
			second, secondAssets := settlementCounts(t, due, dr)
			if second != first || !bytes.Equal(firstAssets, secondAssets) {
				t.Fatal("duplicate payout")
			}
			logs, _, err := due.st.ListAudit(10, 0)
			if err != nil || len(logs) < 2 || logs[0].Action != "server.season.settle" {
				t.Fatal("missing settlement audit")
			}
		})
	}
}

// Exercise the exact service rollover functions used by its native request or
// timer while a concurrent admin request attempts the same saved settlement.
func nativeSettlement(e *env, key string) error {
	_, o, err := globalOwner(e.st, key)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := e.gw.Now()
	switch s := o.service.(type) {
	case *atSvc:
		_, err = s.board("symlf_1", now)
	case *amSvc:
		_, err = s.board("symlf_1", now)
	case *caSvc:
		_, err = s.board("symlf_1", now)
	case *eliteSvc:
		err = s.settleSeasons("symlf_1", now)
	case *gwSvc:
		_, _, _, err = s.ensure("symlf_1", now)
	case *champSvc:
		// Saved round is already past its natural end; advance is what load/tick runs.
		b := &champState{}
		_, err = e.st.LoadGlobal("symlf_1", s.module, b)
		if err == nil && !b.Ev.Done {
			_, err = s.advance("symlf_1", b, s.now())
			if err == nil {
				if s.cross {
					err = e.st.SaveGlobal("symlf_1", "crosschamp", b)
				} else {
					err = e.st.SaveGlobal("symlf_1", champModule, b)
				}
			}
		}
	default:
		w := &drWorld{}
		_, err = e.st.LoadGlobal("symlf_1", drModule, w)
		if err == nil && !w.Settled {
			err = w.settle(e.st, "symlf_1")
			if err == nil {
				err = e.st.SaveGlobal("symlf_1", drModule, w)
			}
		}
	}
	return err
}
func TestAdminSettlementSerializesWithNativeRollover(t *testing.T) {
	for _, key := range settlementKeys {
		t.Run(key, func(t *testing.T) {
			expected := 0
			t.Run("single_reference", func(t *testing.T) {
				e, h, role := seedSettlement(t, key, true)
				before, _ := settlementCounts(t, e, role)
				doSettlement(t, h, key)
				after, _ := settlementCounts(t, e, role)
				expected = after - before
				if expected <= 0 {
					t.Fatal("reference paid nothing")
				}
			})
			t.Run("concurrent", func(t *testing.T) {
				e, _, role := seedSettlement(t, key, true)
				before, _ := settlementCounts(t, e, role)
				start := make(chan struct{})
				errs := make(chan error, 2)
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					_, err := settleGlobal(e.st, role.SrvID, key, e.gw.Now(), AdminOperation{Reason: "并发补结算"})
					errs <- err
				}()
				go func() { defer wg.Done(); <-start; errs <- nativeSettlement(e, key) }()
				close(start)
				wg.Wait()
				close(errs)
				for err := range errs {
					if err != nil {
						t.Fatal(err)
					}
				}
				once, _ := settlementCounts(t, e, role)
				if once-before != expected {
					t.Fatalf("timer/admin overlap: got %d reward mails, single settlement has %d", once-before, expected)
				}
				if _, err := settleGlobal(e.st, role.SrvID, key, e.gw.Now(), AdminOperation{Reason: "并发后复核"}); err != nil {
					t.Fatal(err)
				}
				twice, _ := settlementCounts(t, e, role)
				if once != twice {
					t.Fatal("repeat after overlap paid again")
				}
			})
		})
	}
}
func TestAdminSettlementRejectsReadOnlyModulesAndBadReason(t *testing.T) {
	e := newEnv(t, time.Hour)
	h := opsTestHandler(e)
	for _, m := range AdminModules() {
		if m.Scope != "global" {
			continue
		}
		authorized := false
		for _, key := range settlementKeys {
			authorized = authorized || m.Key == key
		}
		if authorized {
			continue
		}
		key := m.Key
		if strings.HasSuffix(key, "*") {
			key = strings.TrimSuffix(key, "*") + "1"
		}
		w := opsRequest(h, "POST", "/ops/season/settle?srv=symlf_1&key="+key, AdminOperation{Reason: "不允许额外结算"})
		if w.Code != 405 {
			t.Fatalf("%s should be read-only: %d %s", key, w.Code, w.Body)
		}
	}
	for _, reason := range []string{"", " ", strings.Repeat("字", 201), "reason\n"} {
		w := opsRequest(h, "POST", "/ops/season/settle?srv=symlf_1&key=daysrank", AdminOperation{Reason: reason})
		if w.Code != 422 {
			t.Fatal("bad reason accepted")
		}
	}
}

func TestAdminSettlementPartialMailFailureRollsBackAndRetryPaysOnce(t *testing.T) {
	e, h, role := seedSettlement(t, atModule, true)
	b := &atBoard{}
	if _, err := e.st.LoadGlobal(role.SrvID, atModule, b); err != nil {
		t.Fatal(err)
	}
	var season int64
	for _, team := range b.Teams {
		season = team.Season
	}
	// The valid team is mailed first, then a missing recipient fails its FK.
	b.Teams[2] = &atTeam{ID: 2, Season: season, Signed: true, Score: 1000, Members: []atMember{{RID: 99999999}}}
	if err := e.st.SaveGlobal(role.SrvID, atModule, b); err != nil {
		t.Fatal(err)
	}
	raw, _, _ := e.st.GlobalModule(role.SrvID, atModule)
	before, _ := settlementCounts(t, e, role)
	w := opsRequest(h, "POST", "/ops/season/settle?srv=symlf_1&key=arenateam", AdminOperation{Reason: "失败回滚测试"})
	if w.Code != 500 {
		t.Fatalf("expected FK failure: %d %s", w.Code, w.Body)
	}
	after, _ := settlementCounts(t, e, role)
	saved, _, _ := e.st.GlobalModule(role.SrvID, atModule)
	if before != after || !bytes.Equal(raw, saved) {
		t.Fatal("partial settlement left mail/state behind")
	}
	logs, _, _ := e.st.ListAudit(100, 0)
	for _, l := range logs {
		if l.Action == "server.season.settle" {
			t.Fatal("failed payout kept success audit")
		}
	}
	delete(b.Teams, 2)
	if err := e.st.SaveGlobal(role.SrvID, atModule, b); err != nil {
		t.Fatal(err)
	}
	doSettlement(t, h, atModule)
	paid, _ := settlementCounts(t, e, role)
	doSettlement(t, h, atModule)
	again, _ := settlementCounts(t, e, role)
	if paid <= before || paid != again {
		t.Fatal("retry did not produce exactly one payout")
	}
}
