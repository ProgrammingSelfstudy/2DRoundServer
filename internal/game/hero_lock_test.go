package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
)

func TestHeroLock(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("u1", "甲")
	c.send(11025, proto.Message{})
	c.recv(11025)
	hs, _ := e.st.PartnersOf(role.RID)
	id := hs[0].PartnerID
	lock := func(pid, typ int64) proto.Message {
		c.send(11015, proto.Message{"partner_id": pid, "type": typ})
		return c.recv(11015)
	}
	if r := lock(id, 1); r.Int("result") != 1 || r.Int("partner_id") != id || r.Int("type") != 1 {
		t.Fatalf("lock: %v", r)
	}
	c.send(11025, proto.Message{})
	var found bool
	for _, p := range c.recv(11025).Arr("partners") {
		if p.Int("partner_id") == id {
			l := p.Arr("is_lock")
			found = len(l) == 1 && l[0].Int("is_lock") == 1 && l[0].Int("lock_type") == 1
		}
	}
	if !found {
		t.Fatal("the hero list must show the lock")
	}
	if p, _ := e.st.PartnerByID(role.RID, id); !p.Locked {
		t.Fatal("not saved")
	}
	if r := lock(id, 0); r.Int("result") != 1 {
		t.Fatalf("unlock: %v", r)
	}
	if p, _ := e.st.PartnerByID(role.RID, id); p.Locked {
		t.Fatal("still locked")
	}
	if r := lock(99999, 1); r.Int("result") != 0 || r.Str("msg") == "" { // not the role's hero
		t.Fatalf("not own: %v", r)
	}
	if r := lock(id, 5); r.Int("result") != 0 {
		t.Fatalf("bad type: %v", r)
	}
}
