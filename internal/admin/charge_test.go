package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

func TestRoleChargeSummaryPermissionsPagingIsolationAndReadOnly(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("charge-summary", "symlf_1", "充值汇总")
	other := e.seedRole("other-charge", "symlf_1", "其他角色")
	for i := 0; i < 3; i++ {
		if _, err := e.st.CreatePayOrder(store.PayOrder{OrderNo: fmt.Sprintf("own-%d", i), RID: role.RID, SrvID: role.SrvID, ChargeID: 4, AmountCents: 3000}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := e.st.CreatePayOrder(store.PayOrder{OrderNo: fmt.Sprintf("other-%d", i), RID: other.RID, SrvID: other.SrvID, ChargeID: 3, AmountCents: 600}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.st.CancelPayOrder("own-0", "test", "cancelled fixture"); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/roles/%d/charge?limit=2&offset=1", role.RID)
	before, _ := e.st.RoleModules(role.RID)
	auditBefore := e.auditActions()
	paidBefore, _ := e.st.RoleCharge(role.RID)
	for _, token := range []string{viewer, operator, super} {
		r := e.do(token, "GET", path, nil)
		if r.Status != 200 {
			t.Fatalf("summary: %d %s", r.Status, r.Body)
		}
		var body struct {
			RID     int64
			Modules []struct {
				Key    string
				Exists bool
				View   game.AdminChargeView
			}
			Orders struct {
				Items                []store.PayOrder
				Total, Limit, Offset int
			}
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.RID != role.RID || len(body.Modules) != len(game.AdminChargeKeys()) || body.Orders.Total != 3 || body.Orders.Limit != 2 || body.Orders.Offset != 1 || len(body.Orders.Items) != 2 {
			t.Fatalf("bad summary shape: %s", r.Body)
		}
		if body.Orders.Items[0].OrderNo != "own-1" || body.Orders.Items[1].OrderNo != "own-0" || body.Orders.Items[1].Status != store.PayCancelled {
			t.Fatal("filtering must happen before paging and include all statuses")
		}
		for _, row := range body.Orders.Items {
			if row.RID != role.RID {
				t.Fatal("another role's order leaked")
			}
		}
		for _, m := range body.Modules {
			if m.Exists || len(m.View.Protocols) == 0 {
				t.Fatal("missing default read-only view or state initialized")
			}
		}
	}
	if r := e.do("", "GET", path, nil); r.Status != 401 {
		t.Fatalf("public summary: %d", r.Status)
	}
	if r := e.do(viewer, "GET", "/api/roles/9999999/charge", nil); r.Status != 404 {
		t.Fatalf("unknown role: %d", r.Status)
	}
	after, _ := e.st.RoleModules(role.RID)
	paidAfter, _ := e.st.RoleCharge(role.RID)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(auditBefore, e.auditActions()) || !reflect.DeepEqual(paidBefore, paidAfter) {
		t.Fatal("summary initialized state, changed paid data or wrote audit")
	}
}

func chargeResetSeed(key string, m game.AdminModule, now int64) any {
	today := (now + 8*3600) / 86400
	switch key {
	case "daycharge":
		return map[string]any{"Day": today, "Num": 300, "Taken": true}
	case "monthcard":
		return map[string]any{"Base": [2]int64{0, 0}, "Start": [2]int64{today - 1, today - 1}, "Last": [2]int64{today, today}, "Days": [2]int64{1, 1}}
	case "monthfund":
		return map[string]any{"Start": [2]int64{today - 1, today - 1}, "Last": [2]int64{today, today}}
	case "limitgift":
		items := map[string]any{}
		for _, r := range m.Resets {
			items[r.Item] = map[string]any{"Start": now, "Bought": true}
		}
		return map[string]any{"Gifts": items}
	case "cyclegift":
		items := map[string]int64{}
		for _, r := range m.Resets {
			items[r.Item] = 9
		}
		return map[string]any{"End": [3]int64{0, 1, 1}, "Count": items}
	default:
		return map[string]any{"Start": [2]int64{today - 1, today - 1}, "Taken": [2]int64{1, 1}, "Last": [2]int64{today, today}, "Chosen": 2}
	}
}

func TestChargeResetEveryRegisteredHTTPItem(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("charge-reset-http", "symlf_1", "HTTP重置")
	auditBefore := len(e.auditActions())
	operations := 0
	assetsBefore, _ := e.st.RoleAssets(role.RID)
	paidBefore, _ := e.st.RoleCharge(role.RID)
	for _, key := range game.AdminChargeKeys() {
		m, _ := game.FindAdminModule("role", key)
		path := fmt.Sprintf("/api/roles/%d/modules/%s/reset", role.RID, key)
		for _, item := range m.Resets {
			if err := e.st.SaveState(role.RID, key, chargeResetSeed(key, m, e.s.Now().Unix())); err != nil {
				t.Fatal(err)
			}
			body := map[string]string{"item": item.Item, "reason": "逐项白名单回归"}
			for _, tc := range []struct {
				token  string
				status int
			}{{"", 401}, {viewer, 403}, {operator, 403}} {
				if r := e.do(tc.token, "POST", path, body); r.Status != tc.status {
					t.Fatalf("permissions %s/%s: %d", key, item.Item, r.Status)
				}
			}
			r := e.do(super, "POST", path, body)
			if r.Status != 200 {
				t.Fatalf("reset %s/%s: %d %s", key, item.Item, r.Status, r.Body)
			}
			operations++
			raw, exists, err := e.st.RoleModule(role.RID, key)
			if err != nil || exists == (item.Item == "all") {
				t.Fatalf("wrong reset existence for %s/%s", key, item.Item)
			}
			if exists {
				var state map[string]json.RawMessage
				json.Unmarshal(raw, &state)
				if key == "limitgift" || key == "cyclegift" {
					field := "Gifts"
					if key == "cyclegift" {
						field = "Count"
					}
					var items map[string]any
					json.Unmarshal(state[field], &items)
					if _, ok := items[item.Item]; ok || len(items) != len(m.Resets)-1 {
						t.Fatal("reset did not affect exactly one configured id")
					}
				}
			}
			logs, _, err := e.st.ListAudit(1, 0)
			if err != nil || len(logs) != 1 {
				t.Fatal("missing reset audit")
			}
			var detail map[string]any
			json.Unmarshal([]byte(logs[0].DetailJSON), &detail)
			if logs[0].Action != "role.module.reset" || detail["key"] != key || detail["item"] != item.Item || detail["reason"] != body["reason"] || detail["diff"] == nil {
				t.Fatal("reset audit incomplete")
			}
		}
	}
	if len(e.auditActions()) != auditBefore+operations {
		t.Fatal("one successful reset must produce one audit entry")
	}
	assetsAfter, _ := e.st.RoleAssets(role.RID)
	paidAfter, _ := e.st.RoleCharge(role.RID)
	if !reflect.DeepEqual(assetsBefore, assetsAfter) || !reflect.DeepEqual(paidBefore, paidAfter) {
		t.Fatal("HTTP reset delivered rewards or altered paid totals")
	}
}

func TestChargeResetUnknownConfigAndGatewayFailuresLeaveState(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("invalid-charge-reset", "symlf_1", "非法重置")
	for _, key := range []string{"limitgift", "cyclegift", "monthcard", "monthfund", "daycharge"} {
		m, _ := game.FindAdminModule("role", key)
		if err := e.st.SaveState(role.RID, key, chargeResetSeed(key, m, e.s.Now().Unix())); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/api/roles/%d/modules/%s/reset", role.RID, key)
		before, _, _ := e.st.RoleModule(role.RID, key)
		auditBefore := len(e.auditActions())
		for _, item := range []string{"999999999", "-1", "01", "all", "arbitrary"} {
			if r := e.do(super, "POST", path, map[string]string{"item": item, "reason": "拒绝测试"}); r.Status != 422 {
				t.Fatalf("unknown configuration id %s/%s accepted: %d", key, item, r.Status)
			}
		}
		valid := m.Resets[0].Item
		e.gw.mu.Lock()
		e.gw.onlineRID = strconv.FormatInt(role.RID, 10)
		e.gw.mu.Unlock()
		if r := e.do(super, "POST", path, map[string]string{"item": valid, "reason": "在线拒绝"}); r.Status != 409 {
			t.Fatalf("online %s: %d", key, r.Status)
		}
		e.gw.mu.Lock()
		e.gw.onlineRID = ""
		e.gw.down = true
		e.gw.mu.Unlock()
		if r := e.do(super, "POST", path, map[string]string{"item": valid, "reason": "网关故障"}); r.Status != 502 {
			t.Fatalf("gateway failure %s: %d", key, r.Status)
		}
		e.gw.mu.Lock()
		e.gw.down = false
		e.gw.mu.Unlock()
		after, _, _ := e.st.RoleModule(role.RID, key)
		if !bytes.Equal(before, after) || len(e.auditActions()) != auditBefore {
			t.Fatal("rejected operation changed state/audit")
		}
	}
}

func TestChargeResetSharesPaymentLockBeforeReadingPaidBase(t *testing.T) {
	e := newEnv(t)
	_, _, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("reset-payment-lock", "symlf_1", "串行重置")
	m, _ := game.FindAdminModule("role", "monthcard")
	if err := e.st.SaveState(role.RID, "monthcard", chargeResetSeed("monthcard", m, e.s.Now().Unix())); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreatePayOrder(store.PayOrder{OrderNo: "in-flight", RID: role.RID, SrvID: role.SrvID, ChargeID: 4, AmountCents: 3000}); err != nil {
		t.Fatal(err)
	}
	probed := make(chan struct{}, 1)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { probed <- struct{}{}; fmt.Fprint(w, `{"online":false}`) }))
	defer gw.Close()
	e.s.GatewayURL = gw.URL
	unlock := e.st.LockRoleCharge(role.RID)
	result := make(chan int, 1)
	requestErr := make(chan error, 1)
	go func() {
		req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/roles/%d/modules/monthcard/reset", e.ts.URL, role.RID), strings.NewReader(`{"item":"0","reason":"payment lock test"}`))
		if err != nil {
			requestErr <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+super)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			requestErr <- err
			return
		}
		defer resp.Body.Close()
		result <- resp.StatusCode
	}()
	<-probed // Online check must occur even while the payment lock is held.
	premature := false
	select {
	case <-result:
		premature = true
	case err := <-requestErr:
		unlock()
		t.Fatal(err)
	case <-time.After(30 * time.Millisecond):
	}
	if _, err := e.st.ConfirmPayOrder("in-flight", "test", "", 300, map[int64]int64{}, map[int64]int64{}); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	if premature {
		t.Fatal("reset read paid base without sharing payment lock")
	}
	select {
	case code := <-result:
		if code != 200 {
			t.Fatalf("reset: %d", code)
		}
	case err := <-requestErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("reset remained blocked")
	}
	raw, _, _ := e.st.RoleModule(role.RID, "monthcard")
	var state struct{ Base [2]int64 }
	json.Unmarshal(raw, &state)
	if state.Base[0] != 3000 {
		t.Fatalf("reset used stale paid base: %d", state.Base[0])
	}
}
