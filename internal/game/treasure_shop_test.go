package game

import (
	"2droundserver/internal/proto"
	"errors"
	"testing"
	"time"
)

func TestTreasureShopExchange(t *testing.T) {
	e := newEnv(t, time.Minute)
	now := time.Date(2026, 11, 27, 20, 0, 0, 0, dayZone)
	e.setNow(now)
	c, role := enterH5Guild(e, "pointshop", "探宝商城")
	ask := func(cmd uint16, body proto.Message) proto.Message {
		t.Helper()
		body["type"] = 16
		c.send(cmd, body)
		return c.waitFor(cmd)
	}
	buy := func(slot, amount int) proto.Message {
		return ask(13407, proto.Message{"order": slot, "num": amount, "buy_type": 1})
	}
	read := func() proto.Message { return ask(13403, proto.Message{}) }
	if r := read(); r.Int("code") != 0 {
		t.Fatal("level gate", r)
	}
	if err := e.st.SetRoleLevel(role.RID, 10); err != nil {
		t.Fatal(err)
	}
	if r := buy(1, 1); r.Int("code") != 0 {
		t.Fatal("unseen offer", r)
	}
	shelf := read()
	goods := shelf.Arr("item_list")
	if len(goods) != 8 || goods[0].Int("item_id") != 25902 || goods[0].Int("item_num") != 50 || goods[0].Int("price") != 8000 || shelf.Int("refresh_time") != nextMidnight(now) {
		t.Fatal("shelf", shelf)
	}
	for i, g := range goods {
		if g.Int("order") != int64(i+1) || g.Int("pay_type") != 18 || g.Int("limit_count") != 1 {
			t.Fatal(g)
		}
	}
	if r := buy(1, 1); r.Int("code") != 0 || r.Str("msg") != "探宝积分不足" {
		t.Fatal(r)
	}
	if r := ask(13405, proto.Message{}); r.Int("code") != 0 {
		t.Fatal(r)
	}
	if r := read(); r.Int("count") != 0 || r.Arr("item_list")[0].Int("has_buy") != 0 {
		t.Fatal(r)
	}
	if err := e.st.Exchange(role.RID, nil, nil, map[int64]int64{18: 8650}, nil); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][2]int{{1, 0}, {1, 2}, {0, 1}, {9, 1}} {
		if r := buy(args[0], args[1]); r.Int("code") != 0 {
			t.Fatal("invalid order", r)
		}
	}
	if r := buy(1, 1); r.Int("code") != 1 || r.Int("order") != 1 || r.Int("num") != 1 {
		t.Fatal(r)
	}
	if r := buy(1, 1); r.Int("code") != 0 {
		t.Fatal("duplicate", r)
	}
	if r := read(); r.Arr("item_list")[0].Int("has_buy") != 1 {
		t.Fatal(r)
	}
	points, _ := e.st.RoleAssets(role.RID)
	bag, _ := e.st.RoleItems(role.RID)
	if points[18] != 650 || bag[25902] != 50 {
		t.Fatal(points[18], bag[25902])
	}
	refreshed := ask(13405, proto.Message{})
	if refreshed.Int("code") != 1 || refreshed.Int("count") != 1 || refreshed.Arr("item_list")[0].Int("item_id") != 26903 {
		t.Fatal(refreshed)
	}
	for _, g := range refreshed.Arr("item_list") {
		if g.Int("has_buy") != 0 || !h5ShopItemAllowed(g.Int("item_id")) {
			t.Fatal(g)
		}
	}
	if points, _ := e.st.RoleAssets(role.RID); points[18] != 600 {
		t.Fatal(points)
	}
	e.setNow(now.Add(24 * time.Hour))
	if r := buy(8, 1); r.Int("code") != 0 || r.Str("msg") != "货架已更新，请重新查看" {
		t.Fatal("expired offer", r)
	}
	reset := read()
	if reset.Int("count") != 0 || reset.Int("refresh_time") != nextMidnight(now.Add(24*time.Hour)) || reset.Arr("item_list")[0].Int("item_id") != 25902 {
		t.Fatal(reset)
	}
	if points, _ := e.st.RoleAssets(role.RID); points[18] != 600 {
		t.Fatal("daily refresh charged", points)
	}
}

func TestTreasureShopConcurrentExchange(t *testing.T) {
	e := newEnv(t, time.Minute)
	_, role := e.enter("shoptx", "探宝")
	if err := e.st.Exchange(role.RID, nil, nil, map[int64]int64{18: 20000}, nil); err != nil {
		t.Fatal(err)
	}
	before := treasureShopState{Expires: nextMidnight(time.Now())}
	if err := e.st.SaveState(role.RID, treasureShopKey, before); err != nil {
		t.Fatal(err)
	}
	next := before
	next.Sold = 1
	results := make(chan error, 2)
	for range 2 {
		go func() {
			results <- before.commit(e.st, role.RID, next, map[int64]int64{18: 8000}, map[int64]int64{25902: 50})
		}()
	}
	var success, stale int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, errShelfChanged):
			stale++
		default:
			t.Fatal(err)
		}
	}
	balance, _ := e.st.RoleAssets(role.RID)
	bag, _ := e.st.RoleItems(role.RID)
	if success != 1 || stale != 1 || balance[18] != 12000 || bag[25902] != 50 {
		t.Fatal(success, stale, balance[18], bag[25902])
	}
}
