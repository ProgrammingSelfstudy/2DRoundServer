package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"2droundserver/internal/store"
)

// charge.json: the top-up products of the client (tools/extract_charge.py).
//
//go:embed data/charge.json
var chargeJSON []byte

// ChargeProduct is a thing that can be paid for: the price is in yuan.
type ChargeProduct struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Val      float64    `json:"val"` // yuan; one product costs 0.1
	Gold     int64      `json:"gold"`
	Add      int64      `json:"add"`
	FirstAdd int64      `json:"first_add"`
	Items    [][2]int64 `json:"items"`
	VIPExp   int64      `json:"vip_exp"`
	Type     int64      `json:"type"` // 0 diamonds, 1 weekly gifts, 2 gifts and funds of the activities
	Sort     int64      `json:"sort"`
	Pic      int64      `json:"pic"`
}

// chargeAcc are the rewards of the accumulated top-up, by the sum of VIP experience paid in.
var chargeAcc = func() []struct {
	ID, Sum int64
	Items   [][2]int64
} {
	var f struct {
		Acc []struct {
			ID, Sum int64
			Items   [][2]int64
		} `json:"acc"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil {
		panic("game: bad charge.json")
	}
	return f.Acc
}()

var chargeTable = func() map[int64]ChargeProduct {
	var f struct {
		Products []ChargeProduct `json:"products"`
	}
	if err := json.Unmarshal(chargeJSON, &f); err != nil || len(f.Products) < 300 || len(chargeAcc) == 0 {
		panic("game: bad charge.json")
	}
	m := make(map[int64]ChargeProduct, len(f.Products))
	for _, p := range f.Products {
		m[p.ID] = p
	}
	return m
}()

// Cents is the price in cents. The 快乐币 panel's products are priced by `gold` (see klbcharge.go).
func (p ChargeProduct) Cents() int64 {
	if isKlbProduct(p) {
		return p.Gold * 100
	}
	return int64(math.Round(p.Val * 100))
}

// ChargeProductByID looks a product up.
func ChargeProductByID(id int64) (ChargeProduct, bool) {
	p, ok := chargeTable[id]
	return p, ok
}

// chargeTiers are the diamond tiers of the top-up page: the ones the original showed (capture 2026-10-21b: 6, 30, 68, 128,
// 198, 328 and 648 yuan; the 98 yuan row has no place in the page and the 25 to 518 "game top-up" rows are another channel's).
func chargeTiers() []ChargeProduct {
	var out []ChargeProduct
	for id := int64(0); id < 100; id++ {
		if p, ok := chargeTable[id]; ok && p.Type == 0 && p.Sort > 0 && strings.HasPrefix(p.Name, "普通充值") {
			out = append(out, p)
		}
	}
	return out
}

// IsChargeTier says whether a product is one of the diamond tiers of the top-up page. They are paid in 快乐币 (PayWebCharge); the
// QR code orders (10947, /pay/start) refuse them.
func IsChargeTier(p ChargeProduct) bool {
	for _, t := range chargeTiers() {
		if t.ID == p.ID {
			return true
		}
	}
	return false
}

// ChargeDeliverable says whether a product can be paid for: the diamond tiers, and the ones an activity module delivers.
func ChargeDeliverable(p ChargeProduct) bool {
	for _, t := range chargeTiers() {
		if t.ID == p.ID {
			return true
		}
	}
	_, ok := chargeHooks[p.ID]
	return ok
}

// ChargeHook delivers a product that is not plain diamonds (a gift, a card, a fund) after it was paid; it runs in the
// confirmation, before the order is closed, and returns what to credit besides.
type ChargeHook func(st *store.Store, role *store.Role, p ChargeProduct, bought int64) (assets, items map[int64]int64, err error)

var chargeHooks = map[int64]ChargeHook{}

// RegisterChargeHook makes a product payable and says how it is delivered.
func RegisterChargeHook(id int64, h ChargeHook) { chargeHooks[id] = h }

// chargePaidHooks run after any order was confirmed and delivered (the daily first top-up counts what was paid today).
var chargePaidHooks []func(st *store.Store, role *store.Role, p ChargeProduct) error

// chargeGuards say whether a product may be bought now (a gift limited by the cycle), asked when the order is opened.
var chargeGuards = map[int64]func(st *store.Store, role *store.Role) error{}

// RegisterChargeGuard adds a check to opening an order for a product; the error should be an ErrNotPayable.
func RegisterChargeGuard(id int64, g func(st *store.Store, role *store.Role) error) {
	chargeGuards[id] = g
}

// chargeGain is what a product pays out in diamonds and items; first says that the role never bought it before.
func chargeGain(p ChargeProduct, first bool) (assets, items map[int64]int64) {
	assets, items = map[int64]int64{}, map[int64]int64{}
	if p.Type == 0 && !isKlbProduct(p) {
		extra := p.Add
		if first {
			extra = p.FirstAdd
		}
		assets[3] = p.Gold + extra
	}
	return
}

// vipLevelFor is the VIP level a total of VIP experience reaches.
func vipLevelFor(exp int64) int {
	lev := 0
	for l := range vipExp {
		if exp >= vipExp[l] {
			lev = l
		}
	}
	return lev
}

var (
	ErrNoProduct   = errors.New("no such product")
	ErrNotPayable  = errors.New("this product cannot be paid for yet")
	ErrTooManyOpen = errors.New("too many orders wait for payment")
	ErrNoKlb       = errors.New("not enough 快乐币")
	ErrPriceMoney  = errors.New("the money does not match the product")
)

// errNotPayable makes a reason that is a kind of ErrNotPayable.
func errNotPayable(why string) error { return fmt.Errorf("%w: %s", ErrNotPayable, why) }

// maxOpenOrders is how many unpaid orders a role may hold at once.
const maxOpenOrders = 5

// NewOrderNo makes an order number.
func NewOrderNo(now time.Time, rid int64) string {
	return fmt.Sprintf("P%s%d%04d", now.In(dayZone).Format("20060102150405"), rid, rand.Intn(10000))
}

// CreateChargeOrder opens an order for a product; the price is the one in the table, whatever the client says.
func CreateChargeOrder(st *store.Store, role *store.Role, chargeID int64, now time.Time) (*store.PayOrder, error) {
	p, ok := chargeTable[chargeID]
	if !ok {
		return nil, ErrNoProduct
	}
	if !ChargeDeliverable(p) {
		return nil, ErrNotPayable
	}
	if g, ok := chargeGuards[chargeID]; ok {
		if err := g(st, role); err != nil {
			return nil, err
		}
	}
	if n, err := st.PendingPayOrders(role.RID); err != nil {
		return nil, err
	} else if n >= maxOpenOrders {
		return nil, ErrTooManyOpen
	}
	return st.CreatePayOrder(store.PayOrder{OrderNo: NewOrderNo(now, role.RID), RID: role.RID, SrvID: role.SrvID, ChargeID: chargeID, AmountCents: p.Cents()})
}

// ConfirmChargeOrder takes the money of an order as received: delivers the product, adds the VIP experience and raises the VIP
// level to the one the experience reaches (never lowers what the admin page set). store.ErrConflict when the order is not waiting.
func ConfirmChargeOrder(st *store.Store, no, operator, note string) (*store.PayOrder, error) {
	o, err := st.PayOrderByNo(no)
	if err != nil {
		return nil, err
	}
	unlock := st.LockRoleCharge(o.RID)
	defer unlock()
	o, _, err = confirmChargeLocked(st, no, operator, note, 0)
	return o, err
}

// confirmChargeLocked is ConfirmChargeOrder with the role's charge lock already held. With spendKlb > 0 the order is paid in 快乐币
// (pay_web): that many are taken in the same transaction, the diamonds are the tier's own and the first-purchase or later extra of
// the tier is not credited but returned for the 充值返利 mail.
func confirmChargeLocked(st *store.Store, no, operator, note string, spendKlb int64) (*store.PayOrder, int64, error) {
	// Another confirmation may have committed while this request waited.
	o, err := st.PayOrderByNo(no)
	if err != nil {
		return nil, 0, err
	}
	// Reject a completed/cancelled order before hooks can mutate module state.
	// This matters after an authorized reset: replaying an old paid order must
	// not reactivate a fund or increment a gift counter before the SQL conflict.
	// The store keeps its atomic status check as well; rewards/rules are unchanged.
	if o.Status != store.PayWaiting {
		return nil, 0, store.ErrConflict
	}
	p, ok := chargeTable[o.ChargeID]
	if !ok {
		return nil, 0, ErrNoProduct
	}
	role, err := st.RoleByID(o.RID)
	if err != nil {
		return nil, 0, err
	}
	rc, err := st.RoleCharge(o.RID)
	if err != nil {
		return nil, 0, err
	}
	bought := rc.Bought[fmt.Sprint(p.ID)]
	assets, items := chargeGain(p, bought == 0)
	var extra int64
	if spendKlb > 0 {
		ledger, err := st.RoleAssets(o.RID)
		if err != nil {
			return nil, 0, err
		}
		if ledger[klbLabel] < spendKlb {
			return nil, 0, ErrNoKlb
		}
		// the original credits the tier's diamonds and mails the extra (充值返利); the 快乐币 go in the same transaction
		if assets[3] > p.Gold {
			extra = assets[3] - p.Gold
			assets[3] = p.Gold
		}
		assets[klbLabel] -= spendKlb
	}
	if h, ok := chargeHooks[p.ID]; ok {
		a, i, err := h(st, role, p, bought)
		if err != nil {
			return nil, 0, err
		}
		for k, v := range a {
			assets[k] += v
		}
		for k, v := range i {
			items[k] += v
		}
	}
	o, err = st.ConfirmPayOrder(no, operator, note, p.VIPExp, assets, items)
	if err != nil {
		return nil, 0, err
	}
	for _, h := range chargePaidHooks {
		if err := h(st, role, p); err != nil {
			return o, extra, err
		}
	}
	if rc, err = st.RoleCharge(o.RID); err == nil {
		if lev := vipLevelFor(rc.VIPExp); lev > role.VIPLev {
			if err := st.SetRoleVIP(o.RID, lev); err != nil {
				return o, extra, err
			}
		}
	}
	return o, extra, err
}

// PayableProducts are the products that can be paid for now, the diamond tiers first and then the ones with a hook by id.
func PayableProducts() []ChargeProduct {
	out := chargeTiers()
	var ids []int64
	for id := range chargeHooks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if p, ok := chargeTable[id]; ok {
			out = append(out, p)
		}
	}
	return out
}
