package game

import (
	"testing"

	"2droundserver/internal/store"
)

func TestExtraStats(t *testing.T) {
	p := store.Partner{BID: 10502, Star: 13}
	if got := extraStats(p); len(got) != 0 {
		t.Fatalf("nothing worn: %v", got)
	}
	p.Artifacts = []store.ArtPiece{{Base: 10455, Pos: 1, Attr: []store.ArtAttr{{ID: 113, Val: 40}, {ID: 101, Val: 900}}}}
	p.Holy = []store.HolyPiece{{Base: 1010201, Slot: 1, Main: []store.HolyAttr{{ID: 101, Val: 200}}, Rand: []store.HolyAttr{{Pos: 1, ID: 114, Val: 25}, {Pos: 2, ID: 123, Val: 9}}}}
	p.Horcrux = store.HorcruxState{Status: 1, Lev: 0} // 10502 has slots 5, 6, 10; slot 10 of level 0 is dodge_magic 17
	got := extraStats(p)
	if got["crit_rate"] != 40 || got["crit_ratio"] != 25 || got["dodge_magic"] != 17 {
		t.Fatalf("sums: %v", got)
	}
	if _, ok := got["atk"]; ok || got["cure"] != 0 {
		t.Fatalf("only the hit, dodge and crit keys: %v", got)
	}
	m := partnerMessage(p)
	base := partnerMessage(store.Partner{BID: 10502, Star: 13})
	if m.Int("crit_rate") != base.Int("crit_rate")+40 || m.Int("dodge_magic") != base.Int("dodge_magic")+17 {
		t.Fatalf("hero entry: %v vs %v", m.Int("crit_rate"), base.Int("crit_rate"))
	}
}
