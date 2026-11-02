package game

import (
	"errors"
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestPrivilegeTable(t *testing.T) {
	if len(privileges) != 6 {
		t.Fatalf("%d privileges", len(privileges))
	}
	for _, d := range privileges {
		first := max(d.LimitLev, 1)
		if privilegeReward(d.ID, first) == nil || privilegeReward(d.ID, 300) == nil {
			t.Fatalf("privilege %d has no reward at its level", d.ID)
		}
		if d.PayType == 1 {
			if p, ok := ChargeProductByID(d.ChargeID); !ok || !ChargeDeliverable(p) {
				t.Fatalf("privilege %d: product %d is not payable", d.ID, d.ChargeID)
			}
		}
	}
	if privilegeReward(106, 79) != nil {
		t.Fatal("the ghost privilege pays below its level")
	}
}

func TestPrivilegePurchase(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.EnsureServer(store.Server{SrvID: "symlf_1", Name: "s1", Platform: "symlf", ZoneID: 1, Host: "127.0.0.1", Port: 8800, Status: 1, GroupID: 1}); err != nil {
		t.Fatal(err)
	}
	acc, _ := st.FindOrCreateAccount("symlf", "pv")
	role, err := st.CreateRole(acc.ID, "symlf_1", "特权人", 1, 1, 10401)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	chargeClock = func() time.Time { return now }
	defer func() { chargeClock = time.Now }()
	buy := func(id int64) error {
		o, err := CreateChargeOrder(st, role, id, now)
		if err != nil {
			return err
		}
		_, err = ConfirmChargeOrder(st, o.OrderNo, "tester", "")
		return err
	}

	// the territory (level 90) and the ghost (level 80) privileges are for higher levels: no order is opened
	for _, id := range []int64{307, 310} {
		if _, err := CreateChargeOrder(st, role, id, now); !errors.Is(err, ErrNotPayable) {
			t.Fatalf("product %d at level 1: %v", id, err)
		}
	}
	// the weekly gift: the reward row of the level, 30 yuan of VIP experience, owned for seven days
	if err := buy(304); err != nil {
		t.Fatal(err)
	}
	a, _ := st.RoleAssets(role.RID)
	it, _ := st.RoleItems(role.RID)
	if a[1] != 100000 || it[14001] != 3 || it[10001] != 1000 {
		t.Fatalf("after the weekly gift: assets %v items %v", a, it)
	}
	if rc, _ := st.RoleCharge(role.RID); rc.TotalCents != 3000 || rc.VIPExp != 300 {
		t.Fatalf("paid in: %+v", rc)
	}
	p, _ := loadPrivileges(st, role.RID)
	if !p.active(101, now) || p.expire(101, now) != now.Unix()+7*86400 {
		t.Fatalf("101: %+v", p)
	}
	// while it lasts it cannot be bought again; when it ends it can
	if _, err := CreateChargeOrder(st, role, 304, now); !errors.Is(err, ErrNotPayable) {
		t.Fatalf("buying an owned privilege: %v", err)
	}
	now = now.Add(7*24*time.Hour + time.Second)
	if p.active(101, now) || p.expire(101, now) != 0 {
		t.Fatal("the weekly gift outlives its week")
	}
	if err := buy(304); err != nil {
		t.Fatalf("after the week: %v", err)
	}

	// the quick battle privilege is effect type 1 in the list the rest of the client reads (24500), id 102 in the page's (24502)
	if err := buy(305); err != nil {
		t.Fatal(err)
	}
	p, _ = loadPrivileges(st, role.RID)
	l500, l502 := privilegeList24500(p, now), privilegeList24502(p, now)
	if len(l500) != 5 || l500[0].Int("id") != 1 || l500[0].Int("status") != 1 || l500[0].Int("expire_time") != now.Unix()+30*86400 || l500[1].Int("status") != 0 {
		t.Fatalf("24500: %v", l500)
	}
	if len(l502) != 6 || l502[1].Int("id") != 102 || l502[1].Int("status") != 1 || l502[0].Int("id") != 101 || l502[0].Int("status") != 1 {
		t.Fatalf("24502: %v", l502)
	}
}

func TestPrivilegeWithDiamonds(t *testing.T) {
	e := newEnv(t, 100*time.Hour)
	c, role := e.enter("pv2", "特权人二")
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: 1000}, map[int64]int64{}); err != nil {
		t.Fatal(err)
	}
	diamonds := func() int64 { a, _ := e.st.RoleAssets(role.RID); return a[3] }

	c.send(24501, proto.Message{"id": 102}) // a money privilege cannot be bought with diamonds
	if r := c.waitFor(24501); r.Int("code") != 0 {
		t.Fatalf("102 with diamonds: %v", r)
	}
	c.send(24501, proto.Message{"id": 104}) // 1980 diamonds, the role has 1000
	if r := c.waitFor(24501); r.Int("code") != 0 || diamonds() != 1000 {
		t.Fatalf("104 without the diamonds: %v, %d left", r, diamonds())
	}
	c.send(24501, proto.Message{"id": 103}) // 980
	if r := c.waitFor(24501); r.Int("code") != 1 {
		t.Fatalf("103: %v", r)
	}
	if diamonds() != 20 {
		t.Fatalf("diamonds after 103: %d", diamonds())
	}
	if it, _ := e.st.RoleItems(role.RID); it[30054] != 1 || it[10403] != 5 {
		t.Fatalf("reward of 103: %v", it)
	}
	// the page and the client's other list both show it, for good (no end time)
	r := c.waitFor(24502)
	for _, x := range r.Arr("list") {
		if x.Int("id") == 103 && (x.Int("status") != 1 || x.Int("expire_time") != 0) {
			t.Fatalf("24502: %v", r)
		}
	}
	if r := c.waitFor(24500); r.Arr("list")[1].Int("id") != 2 || r.Arr("list")[1].Int("status") != 1 {
		t.Fatalf("24500: %v", r)
	}
	c.send(24501, proto.Message{"id": 103}) // owned for good
	if r := c.waitFor(24501); r.Int("code") != 0 || diamonds() != 20 {
		t.Fatalf("103 twice: %v, %d diamonds", r, diamonds())
	}
	c.send(24502, proto.Message{})
	if r := c.waitFor(24502); len(r.Arr("list")) != 6 {
		t.Fatalf("24502: %v", r)
	}
}
