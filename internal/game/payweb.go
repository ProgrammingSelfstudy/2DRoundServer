package game

import (
	"fmt"
	"time"

	"2droundserver/internal/store"
)

// pay_web is how the original server takes a top-up (capture 2026-11-05, testdata/captures/original-2026-11-05-charge): the client
// pays in 快乐币 by calling POST /api.php/pf/sygame/pay_web/?srv_id&role_id&money&name&productId on the server list's `host`; the
// server takes `money` 快乐币 (one per yuan) on the spot, delivers the product and answers with the 快乐币 left as the body. The
// real money was paid before, when the player bought 快乐币 (the panel's products 30001 to 30020, paid by QR code and confirmed in the
// admin page), so no order waits for anyone here.

// payWebMailExpire is how long the 充值返利 mail lives (the original's was 7 days: capture time_out - send_time = 604800).
const payWebMailExpire = 7 * 24 * 3600

// PayWebResult is what a pay_web payment did.
type PayWebResult struct {
	Order   *store.PayOrder
	Gold    int64 // diamonds credited at once (the tier's own)
	Extra   int64 // diamonds of the 充值返利 mail (the first-purchase or later extra); 0 = no mail
	MailID  int64
	KlbLeft int64
}

// PayWebCharge pays a product with 快乐币: money is the yuan the client sent and must be the product's price. The 快乐币 products
// themselves cannot be bought this way (they are what is paid with). ErrNoKlb when the role has too few; nothing changes then.
func PayWebCharge(st *store.Store, role *store.Role, productID, money int64, now time.Time) (*PayWebResult, error) {
	p, ok := chargeTable[productID]
	if !ok {
		return nil, ErrNoProduct
	}
	if !ChargeDeliverable(p) || isKlbProduct(p) {
		return nil, ErrNotPayable
	}
	if money <= 0 || money*100 != p.Cents() {
		return nil, ErrPriceMoney
	}
	unlock := st.LockRoleCharge(role.RID)
	defer unlock()
	if g, ok := chargeGuards[productID]; ok {
		if err := g(st, role); err != nil {
			return nil, err
		}
	}
	o, err := st.CreatePayOrder(store.PayOrder{OrderNo: NewOrderNo(now, role.RID), RID: role.RID, SrvID: role.SrvID, ChargeID: productID, AmountCents: p.Cents()})
	if err != nil {
		return nil, err
	}
	no := o.OrderNo
	o, extra, err := confirmChargeLocked(st, no, "pay_web", fmt.Sprintf("%d 快乐币", money), money)
	if err != nil {
		if o == nil || o.Status != store.PayDone {
			st.CancelPayOrder(no, "pay_web", err.Error()) // not delivered: do not leave the order waiting
		}
		return nil, err
	}
	res := &PayWebResult{Order: o, Gold: p.Gold, Extra: extra}
	if extra > 0 {
		m, err := st.CreateMail(store.Mail{
			SrvID: role.SrvID, Type: 1, Subject: "充值返利", FromName: "", CreatedBy: "pay_web", ExpireAt: now.Unix() + payWebMailExpire,
			Content:     fmt.Sprintf("恭喜您充值成功，获得%d钻石。并且额外获得充值返利%d钻石。\n希望您在游戏中再接再厉，独占鳌头！", p.Gold, extra),
			Attachments: []store.Attachment{{Kind: store.AttachAsset, ItemID: 3, Num: extra}},
		}, []int64{role.RID})
		if err != nil {
			return res, err // delivered; only the mail is missing
		}
		res.MailID = m.ID
	}
	ledger, err := st.RoleAssets(role.RID)
	if err != nil {
		return res, err
	}
	res.KlbLeft = klbWire(ledger)
	return res, nil
}
