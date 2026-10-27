package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"2droundserver/internal/game"
	"2droundserver/internal/store"
)

// The payment side of the admin page. The operator puts the QR codes of the payment channels here (the payment page of the
// login service shows them), sees the orders players opened and confirms the ones whose money arrived; confirming delivers the
// product. A mistaken order can be cancelled while it waits; an order the operator has to make by hand (the player paid
// outside the game) is made with confirm set.

const maxQRBytes = 600 << 10 // the request body is at most 1 MiB, and a data URL is a third bigger than the image

var payKeys = map[string]bool{"qr_wechat": true, "qr_alipay": true, "notice": true}

type payOrderRow struct {
	store.PayOrder
	Product string `json:"product"`
	Role    string `json:"role"`
}

func (s *Server) payConfig(w http.ResponseWriter, r *http.Request, p *principal) {
	cfg, err := s.Store.PayConfig()
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"qr_wechat": cfg["qr_wechat"], "qr_alipay": cfg["qr_alipay"], "notice": cfg["notice"]})
}

// setPayConfig sets one thing: a QR image as a data URL (png, jpeg or webp up to 600 KiB), or the notice text; empty removes it.
func (s *Server) setPayConfig(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if !decode(w, r, &q) {
		return
	}
	if !payKeys[q.Key] {
		fail(w, http.StatusUnprocessableEntity, "key must be qr_wechat, qr_alipay or notice")
		return
	}
	switch {
	case q.Key == "notice":
		if len([]rune(q.Value)) > 300 {
			fail(w, http.StatusUnprocessableEntity, "the notice is at most 300 characters")
			return
		}
	case q.Value != "":
		ok := false
		for _, pre := range []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/webp;base64,"} {
			ok = ok || strings.HasPrefix(q.Value, pre)
		}
		if !ok || len(q.Value) > maxQRBytes*4/3+64 {
			fail(w, http.StatusUnprocessableEntity, "the QR code must be a png, jpeg or webp image of up to 600 KiB")
			return
		}
	}
	if err := s.Store.SetPayConfig(q.Key, q.Value); err != nil {
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "pay.config", q.Key, map[string]any{"set": q.Value != ""})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) rows(orders []store.PayOrder) []payOrderRow {
	out := make([]payOrderRow, 0, len(orders))
	for _, o := range orders {
		row := payOrderRow{PayOrder: o}
		if pr, ok := game.ChargeProductByID(o.ChargeID); ok {
			row.Product = pr.Name
		}
		if role, err := s.Store.RoleByID(o.RID); err == nil {
			row.Role = role.Name
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) listPayOrders(w http.ResponseWriter, r *http.Request, p *principal) {
	status := -1
	if v := r.URL.Query().Get("status"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 2 {
			fail(w, http.StatusBadRequest, "status is 0, 1 or 2")
			return
		}
		status = n
	}
	limit, _ := page(r)
	list, err := s.Store.PayOrders(status, limit)
	if err != nil {
		internalErr(w, s, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": s.rows(list)})
}

// confirmed delivers an order and tells the role if it is online.
func (s *Server) confirmOrder(r *http.Request, p *principal, no, note string) (*store.PayOrder, map[string]any, int, string) {
	o, err := game.ConfirmChargeOrder(s.Store, no, p.Admin.Username, note)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, nil, 404, "no such order"
	case errors.Is(err, store.ErrConflict):
		return nil, nil, 409, "the order is not waiting for payment any more"
	case errors.Is(err, game.ErrNoProduct):
		return nil, nil, 422, "the product of the order is not in the table"
	case err != nil:
		s.Log.Printf("admin: confirm %s: %v", no, err)
		return nil, nil, 500, "internal error"
	}
	resp := map[string]any{"ok": true, "order": o}
	var out struct {
		Refreshed bool `json:"refreshed"`
	}
	if err := s.gateway("POST", fmt.Sprintf("/push/charge?rid=%d", o.RID), &out); err != nil {
		resp["push_error"] = err.Error() // delivered all the same: the role sees it when it logs in
	} else {
		resp["refreshed"] = out.Refreshed
	}
	s.audit(r, p, "pay.confirm", no, map[string]any{"rid": o.RID, "charge_id": o.ChargeID, "amount_cents": o.AmountCents, "note": note})
	return o, resp, 200, ""
}

func (s *Server) confirmPayOrder(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Note string `json:"note"`
	}
	if !decode(w, r, &q) {
		return
	}
	_, resp, code, msg := s.confirmOrder(r, p, r.PathValue("no"), q.Note)
	if msg != "" {
		fail(w, code, msg)
		return
	}
	writeJSON(w, 200, resp)
}

func (s *Server) cancelPayOrder(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		Note string `json:"note"`
	}
	if !decode(w, r, &q) {
		return
	}
	no := r.PathValue("no")
	err := s.Store.CancelPayOrder(no, p.Admin.Username, q.Note)
	switch {
	case errors.Is(err, store.ErrConflict):
		fail(w, 409, "the order is not waiting for payment any more")
		return
	case err != nil:
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "pay.cancel", no, map[string]any{"note": q.Note})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// createPayOrder makes an order by hand for a role; with confirm it is delivered at once (the player paid outside the game).
func (s *Server) createPayOrder(w http.ResponseWriter, r *http.Request, p *principal) {
	var q struct {
		RID      int64  `json:"rid"`
		ChargeID int64  `json:"charge_id"`
		Confirm  bool   `json:"confirm"`
		Note     string `json:"note"`
	}
	if !decode(w, r, &q) {
		return
	}
	role, err := s.Store.RoleByID(q.RID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, 404, "no such role")
		return
	}
	if err != nil {
		internalErr(w, s, err)
		return
	}
	o, err := game.CreateChargeOrder(s.Store, role, q.ChargeID, s.Now())
	switch {
	case errors.Is(err, game.ErrNoProduct):
		fail(w, 422, "no such product")
		return
	case errors.Is(err, game.ErrNotPayable):
		fail(w, 422, "this product cannot be paid for yet")
		return
	case errors.Is(err, game.ErrTooManyOpen):
		fail(w, 409, "the role has too many orders waiting")
		return
	case err != nil:
		internalErr(w, s, err)
		return
	}
	s.audit(r, p, "pay.create", o.OrderNo, map[string]any{"rid": o.RID, "charge_id": o.ChargeID})
	if !q.Confirm {
		writeJSON(w, 201, map[string]any{"ok": true, "order": o})
		return
	}
	_, resp, code, msg := s.confirmOrder(r, p, o.OrderNo, q.Note)
	if msg != "" {
		fail(w, code, msg)
		return
	}
	writeJSON(w, 201, resp)
}

// payProducts lists what can be paid for, for the admin page's menu.
func (s *Server) payProducts(w http.ResponseWriter, r *http.Request, p *principal) {
	out := []map[string]any{}
	for _, pr := range game.PayableProducts() {
		out = append(out, map[string]any{"id": pr.ID, "name": pr.Name, "yuan": pr.Val})
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
