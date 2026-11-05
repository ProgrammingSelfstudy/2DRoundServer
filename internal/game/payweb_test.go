package game

import (
	"errors"
	"strings"
	"testing"
	"time"

	"2droundserver/internal/store"
)

func payWebEnv(t *testing.T) (*store.Store, *store.Role) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.FindOrCreateAccount("symlf", "payweb1")
	role, err := st.CreateRole(acc.ID, "symlf_1", "直付人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	return st, role
}

// The original's pay_web (capture 2026-11-05): `money` 快乐币 are taken at once, the tier's diamonds are credited and the
// first-purchase extra (and the later extra) arrives as a 充值返利 mail instead of diamonds.
func TestPayWebCharge(t *testing.T) {
	st, role := payWebEnv(t)
	now := time.Unix(1_800_000_000, 0)
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{klbLabel: 100}, nil); err != nil {
		t.Fatal(err)
	}

	// 30 yuan, product 4: 300 diamonds at once, 300 more by mail the first time, then 58
	res, err := PayWebCharge(st, role, 4, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Gold != 300 || res.Extra != 300 || res.KlbLeft != 70 || res.MailID == 0 {
		t.Fatalf("first 30 yuan: %+v", res)
	}
	a, _ := st.RoleAssets(role.RID)
	if a[3] != 300 || a[klbLabel] != 70 {
		t.Fatalf("assets: %v", a)
	}
	mails, err := st.MailsForRole(role)
	if err != nil || len(mails) != 1 {
		t.Fatalf("mails: %v %v", err, mails)
	}
	m := mails[0]
	if m.Subject != "充值返利" || m.ExpireAt != now.Unix()+604800 || len(m.Attachments) != 1 || m.Attachments[0].ItemID != 3 || m.Attachments[0].Num != 300 ||
		m.Content != "恭喜您充值成功，获得300钻石。并且额外获得充值返利300钻石。\n希望您在游戏中再接再厉，独占鳌头！" {
		t.Fatalf("the mail: %+v", m)
	}
	rc, _ := st.RoleCharge(role.RID)
	if rc.TotalCents != 3000 || rc.VIPExp != 300 || rc.Bought["4"] != 1 {
		t.Fatalf("totals: %+v", rc)
	}
	o, _ := st.PayOrderByNo(res.Order.OrderNo)
	if o.Status != store.PayDone || o.Operator != "pay_web" {
		t.Fatalf("order: %+v", o)
	}

	res, err = PayWebCharge(st, role, 4, 30, now)
	if err != nil || res.Gold != 300 || res.Extra != 58 || res.KlbLeft != 40 {
		t.Fatalf("second 30 yuan: %v %+v", err, res)
	}
	if m, _ := st.MailsForRole(role); len(m) != 2 || !strings.Contains(m[0].Content+m[1].Content, "充值返利58钻石") {
		t.Fatalf("the later mail: %+v", m)
	}

	// too few 快乐币: nothing changes and no order waits
	before, _ := st.RoleAssets(role.RID)
	if _, err := PayWebCharge(st, role, 20, 68, now); !errors.Is(err, ErrNoKlb) {
		t.Fatalf("68 yuan with 40: %v", err)
	}
	after, _ := st.RoleAssets(role.RID)
	if after[3] != before[3] || after[klbLabel] != before[klbLabel] {
		t.Fatalf("a refused payment changed %v to %v", before, after)
	}
	if n, _ := st.PendingPayOrders(role.RID); n != 0 {
		t.Fatalf("%d orders wait after a refusal", n)
	}

	// the money must be the product's price; the 快乐币 products cannot be paid with 快乐币; an undeliverable one is refused
	if _, err := PayWebCharge(st, role, 4, 6, now); !errors.Is(err, ErrPriceMoney) {
		t.Fatalf("wrong money: %v", err)
	}
	if _, err := PayWebCharge(st, role, 30001, 38, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("a 快乐币 product: %v", err)
	}
	if _, err := PayWebCharge(st, role, 101, 1, now); !errors.Is(err, ErrNotPayable) && !errors.Is(err, ErrPriceMoney) {
		t.Fatalf("a product nothing delivers: %v", err)
	}
}

// A gift goes through the same payment: its guard (the daily limit) and its hook apply, and no 充值返利 mail is sent.
func TestPayWebGift(t *testing.T) {
	st, role := payWebEnv(t)
	now := time.Unix(1_800_000_000, 0)
	if err := st.Exchange(role.RID, nil, nil, map[int64]int64{klbLabel: 10}, nil); err != nil {
		t.Fatal(err)
	}
	res, err := PayWebCharge(st, role, 301, 1, now) // 每日礼包, 1 yuan (capture: 21016 then pay_web productId 301)
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra != 0 || res.MailID != 0 || res.KlbLeft != 9 {
		t.Fatalf("gift: %+v", res)
	}
	if a, _ := st.RoleAssets(role.RID); a[klbLabel] != 9 {
		t.Fatalf("assets: %v", a)
	}
	if rc, _ := st.RoleCharge(role.RID); rc.Bought["301"] != 1 || rc.VIPExp != 10 {
		t.Fatalf("totals: %+v", rc)
	}
	if m, _ := st.MailsForRole(role); len(m) != 0 {
		t.Fatalf("a gift sends no mail: %+v", m)
	}
}
