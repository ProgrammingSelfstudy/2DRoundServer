package game

import (
	"testing"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestH5SocialFriendBlacklistLifecycle(t *testing.T) {
	e := newSocialEnv(t)
	ca, a := e.enter(store.PlatformH5, "a", "甲")
	cb, b := e.enter(store.PlatformH5, "b", "乙")
	_, c := e.role(store.PlatformH5, "c", "丙")
	ca.send(13303, rid(b))
	if reply := ca.recv(13303); reply.Int("code") != 1 {
		t.Fatalf("friend request: %v", reply)
	}
	cb.recv(13304)
	cb.send(13332, rid(a))
	if reply := cb.recv(13332); reply.Int("code") != 1 {
		t.Fatalf("block: %v", reply)
	}
	cb.send(13330, proto.Message{})
	if list := cb.recv(13330).Arr("black_list"); !same(ids(list), a.RID) {
		t.Fatalf("blacklist: %v", list)
	}
	cb.send(13311, proto.Message{})
	if pending := cb.recv(13311).Arr("friend_req_list"); len(pending) != 0 {
		t.Fatalf("block did not clear the request: %v", pending)
	}
	accept := rid(a)
	accept["agreed"] = 1
	cb.send(13305, accept)
	if reply := cb.recv(13305); reply.Int("code") != 0 {
		t.Fatalf("accepted a cleared request: %v", reply)
	}
	ca.send(13303, rid(b))
	if reply := ca.recv(13303); reply.Int("code") != 0 {
		t.Fatalf("request from blocked role: %v", reply)
	}
	cb.send(13303, rid(a))
	if reply := cb.recv(13303); reply.Int("code") != 0 {
		t.Fatalf("request from blocker: %v", reply)
	}
	for _, cli := range []*client{ca, cb} {
		cli.send(13320, proto.Message{})
		if recommendations := cli.recv(13320).Arr("recommend_list"); !same(ids(recommendations), c.RID) {
			t.Fatalf("blocked peer included or unrelated recommendation missing: %v", recommendations)
		}
	}
	cb.send(13333, rid(a))
	if reply := cb.recv(13333); reply.Int("code") != 1 {
		t.Fatalf("unblock: %v", reply)
	}
	befriend(t, ca, a, cb, b)
	cb.send(13300, proto.Message{})
	if list := cb.recv(13300).Arr("friend_list"); !same(ids(list), a.RID) {
		t.Fatalf("friend list after unblock and accept: %v", list)
	}
	cb.send(13332, rid(a))
	if reply := cb.recv(13332); reply.Int("code") != 1 {
		t.Fatalf("block friend: %v", reply)
	}
	if push := ca.recv(13308); push.Int("rid") != b.RID {
		t.Fatalf("friend removal push: %v", push)
	}
	for _, cli := range []*client{ca, cb} {
		cli.send(13300, proto.Message{})
		if list := cli.recv(13300).Arr("friend_list"); len(list) != 0 {
			t.Fatalf("friendship survived block: %v", list)
		}
	}
}

func TestH5SocialProfileUsesCurrentFiveStarMinimum(t *testing.T) {
	e := newSocialEnv(t)
	h5, _ := e.enter(store.PlatformH5, "a", "甲")
	legacy, _ := e.enter("symlf", "b", "乙")
	_, target := e.role("symlf", "c", "丙")
	seeds := []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10503, Star: 7}, {BID: 10401, Star: 5}, {BID: 10101, Star: 1}}
	if granted, err := e.st.GrantStarterPartners(target.RID, seeds); err != nil || !granted {
		t.Fatalf("profile seeds: granted=%v err=%v", granted, err)
	}
	h5.send(10315, rid(target))
	card := h5.recv(10315)
	heroes := card.Arr("partner_list")
	stars := make(map[int64]int64, len(heroes))
	for _, hero := range heroes {
		stars[hero.Int("bid")] = hero.Int("star")
	}
	if len(heroes) != 3 || stars[10501] != 5 || stars[10503] != 7 || stars[10401] != 5 || card.Int("power") <= 0 {
		t.Fatalf("H5 card should show all known heroes currently at five stars or higher: %v", card)
	}
	legacy.send(10315, rid(target))
	if original := legacy.recv(10315); len(original.Arr("partner_list")) != len(seeds) || original.Int("power") <= card.Int("power") {
		t.Fatalf("legacy card lost heroes or power: %v", original)
	}
}
