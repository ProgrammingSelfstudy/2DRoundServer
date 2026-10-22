package game

import (
	"testing"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

func TestProfileCard(t *testing.T) {
	e := newEnv(t, time.Minute)
	ca, a := e.enter("a", "甲")
	_, b := e.enter("b", "乙")
	if _, err := e.st.GrantStarterPartners(b.RID, []store.PartnerSeed{{BID: 10501, Star: 5}, {BID: 10101, Star: 1}}); err != nil {
		t.Fatal(err)
	}

	ca.send(10315, proto.Message{"rid": b.RID, "srv_id": "symlf_1"})
	r := ca.recv(10315)
	heroes := r.Arr("partner_list")
	if r.Int("rid") != b.RID || r.Str("name") != "乙" || r.Int("lev") != int64(b.Lev) || r.Int("is_friend") != 0 {
		t.Fatalf("card: %v", r)
	}
	if len(heroes) != 2 || heroes[0].Int("bid") != 10501 || r.Int("power") <= 0 {
		t.Fatalf("heroes should be strongest first and give a power: %v", r)
	}

	// friends are marked, and a role can open its own card
	if err := e.st.RequestFriend(a.RID, b.RID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AcceptFriendRequest(a.RID, b.RID, maxFriends); err != nil {
		t.Fatal(err)
	}
	ca.send(10315, proto.Message{"rid": b.RID, "srv_id": "symlf_1"})
	if r := ca.recv(10315); r.Int("is_friend") != 1 {
		t.Fatalf("friend flag: %v", r)
	}
	ca.send(10315, proto.Message{"rid": a.RID, "srv_id": "symlf_1"})
	if r := ca.recv(10315); r.Int("rid") != a.RID || r.Int("is_friend") != 0 {
		t.Fatalf("own card: %v", r)
	}

	// unknown role, other server: no reply
	ca.send(10315, proto.Message{"rid": 99999, "srv_id": "symlf_1"})
	ca.send(10315, proto.Message{"rid": b.RID, "srv_id": "other_1"})
	ca.expectNothing()
}
