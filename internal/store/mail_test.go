package store

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type mailEnv struct {
	s     *Store
	a, b  *Role // two roles of different accounts on symlf_1
	other *Role // a role on symlf_2
	clock *time.Time
	t     *testing.T
	accA  *Account
}

func newMailEnv(t *testing.T) *mailEnv {
	t.Helper()
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	e := &mailEnv{s: s, clock: &now, t: t}
	for _, id := range []string{"symlf_1", "symlf_2"} {
		if err := s.EnsureServer(srv(id)); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(acc, srvID, name string) *Role {
		a, _ := s.FindOrCreateAccount("symlf", acc)
		r, err := s.CreateRole(a.ID, srvID, name, 0, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	e.a, e.b, e.other = mk("ua", "symlf_1", "甲"), mk("ub", "symlf_1", "乙"), mk("uc", "symlf_2", "丙")
	return e
}

func (e *mailEnv) advance(d time.Duration) { *e.clock = e.clock.Add(d) }

func gold(n int64) Attachment     { return Attachment{Kind: AttachAsset, ItemID: 3, Num: n} }
func item(id, n int64) Attachment { return Attachment{Kind: AttachItem, ItemID: id, Num: n} }

func TestDirectMailStatusFlow(t *testing.T) {
	e := newMailEnv(t)
	m, err := e.s.CreateMail(Mail{Subject: "hi", Content: "body", Attachments: []Attachment{gold(100), item(40112, 2)}}, []int64{e.a.RID})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := e.s.MailsForRole(e.b); len(got) != 0 {
		t.Fatal("another role must not see a direct mail")
	}
	list, _ := e.s.MailsForRole(e.a)
	if len(list) != 1 || list[0].Status() != 0 || !list[0].HasItems() || list[0].FromName != "GM" || list[0].Type != 1 || len(list[0].Attachments) != 2 {
		t.Fatalf("fresh mail: %+v", list)
	}
	e.advance(time.Minute)
	at, err := e.s.MarkMailRead(e.a.RID, m.ID)
	if err != nil || at != e.clock.Unix() {
		t.Fatalf("read: %d %v", at, err)
	}
	e.advance(time.Minute)
	if at2, _ := e.s.MarkMailRead(e.a.RID, m.ID); at2 != at {
		t.Fatal("the first read time must be kept")
	}
	one, _ := e.s.MailForRole(e.a.RID, m.ID)
	if one.Status() != 1 || !one.HasItems() {
		t.Fatalf("read, unclaimed: %+v", one)
	}
	taken, _, err := e.s.ClaimMail(e.a.RID, m.ID)
	if err != nil || len(taken) != 2 {
		t.Fatalf("claim: %v %v", taken, err)
	}
	one, _ = e.s.MailForRole(e.a.RID, m.ID)
	if one.Status() != 2 || one.HasItems() {
		t.Fatalf("claimed: %+v", one)
	}
	assets, _ := e.s.RoleAssets(e.a.RID)
	items, _ := e.s.RoleItems(e.a.RID)
	if assets[3] != 100 || items[40112] != 2 {
		t.Fatalf("ledgers: %v %v", assets, items)
	}
}

func TestClaimOnlyOnceAndOnlyOwnMail(t *testing.T) {
	e := newMailEnv(t)
	m, _ := e.s.CreateMail(Mail{Subject: "x", Attachments: []Attachment{gold(5)}}, []int64{e.a.RID})
	plain, _ := e.s.CreateMail(Mail{Subject: "no items"}, []int64{e.a.RID})
	if _, _, err := e.s.ClaimMail(e.b.RID, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's mail: %v", err)
	}
	if _, _, err := e.s.ClaimMail(e.a.RID, plain.ID); !errors.Is(err, ErrNoAttachments) {
		t.Fatalf("mail without attachments: %v", err)
	}
	if _, _, err := e.s.ClaimMail(e.a.RID, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.s.ClaimMail(e.a.RID, m.ID); !errors.Is(err, ErrNoAttachments) {
		t.Fatalf("second claim: %v", err)
	}
	if assets, _ := e.s.RoleAssets(e.a.RID); assets[3] != 5 {
		t.Fatalf("granted twice: %v", assets)
	}
	if _, _, err := e.s.ClaimMail(e.a.RID, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown mail: %v", err)
	}
}

func TestConcurrentClaimGrantsOnce(t *testing.T) {
	s, err := Open(t.TempDir() + "/c.db") // a file database, so goroutines really overlap
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.EnsureServer(srv("symlf_1"))
	a, _ := s.FindOrCreateAccount("symlf", "u")
	r, _ := s.CreateRole(a.ID, "symlf_1", "甲", 0, 1, 0)
	m, _ := s.CreateMail(Mail{Subject: "x", Attachments: []Attachment{gold(7)}}, []int64{r.RID})
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.ClaimMail(r.RID, m.ID); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assets, _ := s.RoleAssets(r.RID)
	if ok != 1 || assets[3] != 7 {
		t.Fatalf("successful claims=%d, granted=%v (want exactly one claim and 7)", ok, assets)
	}
}

func TestBroadcastDeliveryRules(t *testing.T) {
	e := newMailEnv(t)
	e.advance(time.Hour)
	all, _ := e.s.CreateMail(Mail{Scope: MailBroadcast, Subject: "all servers", Attachments: []Attachment{gold(1)}}, nil)
	one, _ := e.s.CreateMail(Mail{Scope: MailBroadcast, SrvID: "symlf_1", Subject: "server 1 only"}, nil)
	expired, _ := e.s.CreateMail(Mail{Scope: MailBroadcast, Subject: "expired", ExpireAt: e.clock.Unix() + 60}, nil)
	_ = expired
	e.advance(2 * time.Minute) // the third mail has expired by now

	la, _ := e.s.MailsForRole(e.a)
	if len(la) != 2 {
		t.Fatalf("role on server 1 should get the two live broadcasts, got %d: %+v", len(la), la)
	}
	lo, _ := e.s.MailsForRole(e.other)
	if len(lo) != 1 || lo[0].ID != all.ID {
		t.Fatalf("role on server 2 gets only the all-server mail: %+v", lo)
	}
	_ = one

	// a role created after the broadcast does not receive it
	acc, _ := e.s.FindOrCreateAccount("symlf", "late")
	late, _ := e.s.CreateRole(acc.ID, "symlf_1", "晚到", 0, 1, 0)
	if l, _ := e.s.MailsForRole(late); len(l) != 0 {
		t.Fatalf("new role must not get older broadcasts: %+v", l)
	}
	// but it does get one sent afterwards
	e.advance(time.Minute)
	e.s.CreateMail(Mail{Scope: MailBroadcast, Subject: "later"}, nil)
	if l, _ := e.s.MailsForRole(late); len(l) != 1 || l[0].Subject != "later" {
		t.Fatalf("later broadcast: %+v", l)
	}
	// listing twice does not duplicate
	if l, _ := e.s.MailsForRole(e.a); len(l) != 3 {
		t.Fatalf("after another list: %d", len(l))
	}
}

func TestDeleteRulesAndSoftDelete(t *testing.T) {
	e := newMailEnv(t)
	b, _ := e.s.CreateMail(Mail{Scope: MailBroadcast, Subject: "bc"}, nil) // created at the role's own creation second: delivered
	withItems, _ := e.s.CreateMail(Mail{Subject: "items", Attachments: []Attachment{gold(1)}}, []int64{e.a.RID})
	plain, _ := e.s.CreateMail(Mail{Subject: "plain"}, []int64{e.a.RID})
	e.s.MailsForRole(e.a)

	done, _ := e.s.DeleteMails(e.a.RID, []int64{plain.ID, withItems.ID, b.ID})
	if len(done) != 0 {
		t.Fatalf("unread mail must not be deletable: %v", done)
	}
	e.s.MarkMailRead(e.a.RID, plain.ID)
	e.s.MarkMailRead(e.a.RID, withItems.ID)
	e.s.MarkMailRead(e.a.RID, b.ID)
	done, _ = e.s.DeleteMails(e.a.RID, []int64{plain.ID, withItems.ID, b.ID})
	if len(done) != 2 || done[0] != plain.ID || done[1] != b.ID {
		t.Fatalf("read mail without unclaimed items may go, the one with items stays: %v", done)
	}
	e.s.ClaimMail(e.a.RID, withItems.ID)
	if done, _ = e.s.DeleteMails(e.a.RID, []int64{withItems.ID}); len(done) != 1 {
		t.Fatalf("claimed mail is deletable: %v", done)
	}
	// the deleted broadcast must not come back on the next list
	if l, _ := e.s.MailsForRole(e.a); len(l) != 0 {
		t.Fatalf("deleted mails reappeared: %+v", l)
	}
	// and another role's copy is unaffected
	e.s.MailsForRole(e.b)
	if l, _ := e.s.MailsForRole(e.b); len(l) != 1 {
		t.Fatalf("other role lost its copy: %+v", l)
	}
}

func TestClaimAllAndListLimit(t *testing.T) {
	e := newMailEnv(t)
	for i := 0; i < 3; i++ {
		e.s.CreateMail(Mail{Subject: "g", Attachments: []Attachment{gold(10)}}, []int64{e.a.RID})
		e.advance(time.Second)
	}
	e.s.CreateMail(Mail{Subject: "none"}, []int64{e.a.RID})
	got, err := e.s.ClaimAllMails(e.a)
	if err != nil || len(got) != 3 {
		t.Fatalf("claim all: %v %v", got, err)
	}
	if assets, _ := e.s.RoleAssets(e.a.RID); assets[3] != 30 {
		t.Fatalf("assets: %v", assets)
	}
	if again, _ := e.s.ClaimAllMails(e.a); len(again) != 0 {
		t.Fatalf("second claim-all: %v", again)
	}
	for i := 0; i < MaxListedMails+5; i++ {
		e.s.CreateMail(Mail{Subject: "n"}, []int64{e.a.RID})
		e.advance(time.Second)
	}
	l, _ := e.s.MailsForRole(e.a)
	if len(l) != MaxListedMails {
		t.Fatalf("list must be capped at %d, got %d", MaxListedMails, len(l))
	}
	for i := 1; i < len(l); i++ {
		if l[i].CreatedAt > l[i-1].CreatedAt {
			t.Fatal("list must be newest first")
		}
	}
}

func TestCreateMailValidation(t *testing.T) {
	e := newMailEnv(t)
	if _, err := e.s.CreateMail(Mail{Subject: "x"}, nil); err == nil {
		t.Fatal("direct mail without recipients")
	}
	if _, err := e.s.CreateMail(Mail{Scope: MailBroadcast, Subject: "x"}, []int64{e.a.RID}); err == nil {
		t.Fatal("broadcast with recipients")
	}
	if _, err := e.s.CreateMail(Mail{Subject: "x", Attachments: []Attachment{{Kind: "asset", ItemID: 3, Num: 0}}}, []int64{e.a.RID}); err == nil {
		t.Fatal("zero-amount attachment")
	}
	if _, err := e.s.CreateMail(Mail{Subject: "x", Attachments: []Attachment{{Kind: "bogus", ItemID: 3, Num: 1}}}, []int64{e.a.RID}); err == nil {
		t.Fatal("unknown attachment kind")
	}
	// a failed create must leave nothing behind
	if l, _ := e.s.MailsForRole(e.a); len(l) != 0 {
		t.Fatalf("partial mail left behind: %+v", l)
	}
}
