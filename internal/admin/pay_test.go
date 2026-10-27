package admin

import (
	"encoding/json"
	"strings"
	"testing"

)

func TestAdminPay(t *testing.T) {
	e := newEnv(t)
	viewer, operator, super := e.tokens()
	e.seedServer("symlf_1")
	role := e.seedRole("pay1", "symlf_1", "付款人")

	// the QR codes: the manager sets them, the others look
	png := "data:image/png;base64,iVBORw0KGgo="
	if res := e.do(operator, "PUT", "/api/pay/config", map[string]any{"key": "qr_wechat", "value": png}); res.Status != 403 {
		t.Fatalf("operator sets a QR: %d", res.Status)
	}
	if res := e.do(super, "PUT", "/api/pay/config", map[string]any{"key": "qr_wechat", "value": "http://evil/x.png"}); res.Status != 422 {
		t.Fatalf("not a data URL: %d", res.Status)
	}
	if res := e.do(super, "PUT", "/api/pay/config", map[string]any{"key": "other", "value": "x"}); res.Status != 422 {
		t.Fatalf("unknown key: %d", res.Status)
	}
	if res := e.do(super, "PUT", "/api/pay/config", map[string]any{"key": "qr_wechat", "value": png}); res.Status != 200 {
		t.Fatalf("set the QR: %d %s", res.Status, res.Body)
	}
	e.do(super, "PUT", "/api/pay/config", map[string]any{"key": "notice", "value": "付款请备注订单号"})
	res := e.do(viewer, "GET", "/api/pay/config", nil)
	if res.Status != 200 || !strings.Contains(string(res.Body), png) || !strings.Contains(string(res.Body), "付款请备注订单号") {
		t.Fatalf("read the config: %d %s", res.Status, res.Body)
	}

	// an order of the product menu
	res = e.do(viewer, "GET", "/api/pay/products", nil)
	if res.Status != 200 || !strings.Contains(string(res.Body), `"id":4`) {
		t.Fatalf("products: %d %s", res.Status, res.Body)
	}
	res = e.do(super, "POST", "/api/pay/orders", map[string]any{"rid": role.RID, "charge_id": 4})
	if res.Status != 201 {
		t.Fatalf("create: %d %s", res.Status, res.Body)
	}
	var made struct {
		Order struct {
			OrderNo string `json:"order_no"`
		} `json:"order"`
	}
	json.Unmarshal(res.Body, &made)
	no := made.Order.OrderNo
	if res := e.do(super, "POST", "/api/pay/orders", map[string]any{"rid": role.RID, "charge_id": 101}); res.Status != 422 {
		t.Fatalf("an undeliverable product: %d", res.Status)
	}
	list := e.do(viewer, "GET", "/api/pay/orders?status=0", nil)
	if list.Status != 200 || !strings.Contains(string(list.Body), no) || !strings.Contains(string(list.Body), "普通充值30") || !strings.Contains(string(list.Body), "付款人") {
		t.Fatalf("list: %d %s", list.Status, list.Body)
	}
	// confirming is the manager's
	if res := e.do(operator, "POST", "/api/pay/orders/"+no+"/confirm", map[string]any{}); res.Status != 403 {
		t.Fatalf("operator confirms: %d", res.Status)
	}
	e.gw.mu.Lock()
	e.gw.refreshed = true
	e.gw.mu.Unlock()
	res = e.do(super, "POST", "/api/pay/orders/"+no+"/confirm", map[string]any{"note": "wx 30"})
	if res.Status != 200 || !strings.Contains(string(res.Body), `"refreshed":true`) {
		t.Fatalf("confirm: %d %s", res.Status, res.Body)
	}
	e.gw.mu.Lock()
	pushed := append([]string(nil), e.gw.charges...)
	e.gw.mu.Unlock()
	if len(pushed) != 1 {
		t.Fatalf("the gateway was told %v", pushed)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 600 {
		t.Fatalf("diamonds: %v", a)
	}
	if res := e.do(super, "POST", "/api/pay/orders/"+no+"/confirm", map[string]any{}); res.Status != 409 {
		t.Fatalf("a second confirm: %d", res.Status)
	}
	if res := e.do(super, "POST", "/api/pay/orders/"+no+"/cancel", map[string]any{}); res.Status != 409 {
		t.Fatalf("cancel a paid order: %d", res.Status)
	}
	if res := e.do(super, "POST", "/api/pay/orders/nope/confirm", map[string]any{}); res.Status != 404 {
		t.Fatalf("unknown order: %d", res.Status)
	}
	// cancel one, and make one by hand that is delivered at once
	res = e.do(super, "POST", "/api/pay/orders", map[string]any{"rid": role.RID, "charge_id": 3})
	json.Unmarshal(res.Body, &made)
	if res := e.do(super, "POST", "/api/pay/orders/"+made.Order.OrderNo+"/cancel", map[string]any{"note": "拍错"}); res.Status != 200 {
		t.Fatalf("cancel: %d %s", res.Status, res.Body)
	}
	if res := e.do(super, "POST", "/api/pay/orders/"+made.Order.OrderNo+"/confirm", map[string]any{}); res.Status != 409 {
		t.Fatalf("confirm a cancelled order: %d", res.Status)
	}
	res = e.do(super, "POST", "/api/pay/orders", map[string]any{"rid": role.RID, "charge_id": 3, "confirm": true, "note": "线下"})
	if res.Status != 201 {
		t.Fatalf("by hand: %d %s", res.Status, res.Body)
	}
	if a, _ := e.st.RoleAssets(role.RID); a[3] != 600+120 {
		t.Fatalf("diamonds after the order by hand: %v", a)
	}
}
