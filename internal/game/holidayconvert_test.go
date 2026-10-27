package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestHolidayConvert(t *testing.T) {
	e := newEnv(t, time.Minute)
	c, role := e.enter("a", "甲")
	camp := partnerTable[10501].Camp
	row, ok := convertRowOf(7, camp)
	if !ok {
		t.Skipf("no conversion row for camp %d", camp)
	}
	seeds := []store.PartnerSeed{{BID: 10501, Star: 7}}
	for i := 0; i < row.Expend+1; i++ {
		seeds = append(seeds, store.PartnerSeed{BID: 10502, Star: 5})
	}
	if _, err := e.st.GrantStarterPartners(role.RID, seeds); err != nil {
		t.Fatal(err)
	}
	heroes, _ := e.st.PartnersOf(role.RID)
	var srcID int64
	var mats []int64
	for _, h := range heroes {
		if h.BID == 10501 && h.Star == 7 {
			srcID = h.PartnerID
		}
		if h.BID == 10502 && h.Star == 5 {
			mats = append(mats, h.PartnerID)
		}
	}
	list := func(ids []int64) []proto.Message {
		out := []proto.Message{}
		for _, id := range ids {
			out = append(out, proto.Message{"partner_id": id})
		}
		return out
	}
	convert := func(bid int64, ids []int64) proto.Message {
		c.send(16686, proto.Message{"partner_id": srcID, "partner_bid": bid, "expend": list(ids)})
		return c.waitFor(16686)
	}
	if r := convert(10502, mats[:row.Expend]); r.Int("code") != 0 {
		t.Fatalf("no time power yet: %v", r)
	}
	var atts []store.Attachment
	for _, l := range row.Loss {
		atts = append(atts, store.Attachment{Kind: store.AttachItem, ItemID: l[0], Num: l[1]})
	}
	m := e.sendMail(store.Mail{Subject: "g", Attachments: atts}, role.RID)
	c.send(10801, proto.Message{"id": m.ID, "srv_id": "symlf_1"})
	c.waitFor(10801)
	c.waitFor(10500)
	if r := convert(10501, mats[:row.Expend]); r.Int("code") != 0 {
		t.Fatalf("same hero: %v", r)
	}
	if r := convert(10502, mats[:row.Expend-1]); r.Int("code") != 0 {
		t.Fatalf("too few: %v", r)
	}
	if r := convert(10503, mats[:row.Expend]); r.Int("code") != 0 {
		t.Fatalf("materials of another hero: %v", r)
	}
	r := convert(10502, mats[:row.Expend])
	if r.Int("code") != 1 {
		t.Fatalf("convert: %v", r)
	}
	after, _ := e.st.PartnersOf(role.RID)
	var got *store.Partner
	left := 0
	for i, h := range after {
		if h.PartnerID == srcID {
			t.Fatal("the old hero is still there")
		}
		if h.BID == 10502 && h.Star == 7 {
			got = &after[i]
		}
		if h.BID == 10502 && h.Star == 5 {
			left++
		}
	}
	if got == nil || left != 1 {
		t.Fatalf("heroes after: %+v", after)
	}
}
