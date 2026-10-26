package game

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// redbag.json: the red bags of the guild (公会红包) of the client (tools/extract_redbag.py).
//
//go:embed data/redbag.json
var redbagJSON []byte

const (
	redbagModule   = "guildredbag"
	redbagDayMod   = "redbagday"
	redbagLife     = 24 * 3600     // a red bag can be opened for a day
	redbagKeep     = 7 * 24 * 3600 // and is kept for the list and the rank for a week
	redbagSendItem = 1             // loss_type 1 of 13535: sent with the red bag item
)

type redbagKind struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Assets   string     `json:"assets"`
	Val      int64      `json:"val"`
	Num      int64      `json:"num"`
	Limit    int64      `json:"limit"`
	Price    int64      `json:"price"`
	LossItem [][2]int64 `json:"loss_item"`
	Reward   [][2]int64 `json:"reward"`
}

var redbagCfg = func() map[int64]redbagKind {
	var f struct {
		Bags []redbagKind `json:"bags"`
	}
	if err := json.Unmarshal(redbagJSON, &f); err != nil || len(f.Bags) == 0 {
		panic("game: bad redbag.json")
	}
	out := map[int64]redbagKind{}
	for _, b := range f.Bags {
		out[b.ID] = b
	}
	return out
}()

// redbagAsset is the currency a red bag holds: "coin" is the berry (1), "gold" the diamond (3).
func redbagAsset(label string) int64 {
	if label == "gold" {
		return 3
	}
	return 1
}

type redbagClaim struct {
	RID   int64  `json:"rid"`
	SrvID string `json:"srv"`
	Name  string `json:"name"`
	Face  int64  `json:"face"`
	Post  int    `json:"post"`
	Val   int64  `json:"val"`
	Time  int64  `json:"time"`
}

type redbag struct {
	ID     int64         `json:"id"`
	GID    int64         `json:"gid"`
	Kind   int64         `json:"kind"`
	RID    int64         `json:"rid"`
	Name   string        `json:"name"`
	Face   int64         `json:"face"`
	Post   int           `json:"post"`
	Val    int64         `json:"val"`
	Max    int64         `json:"max"`
	Sent   int64         `json:"sent"`
	Claims []redbagClaim `json:"claims"`
}

type redbagBook struct {
	Next int64     `json:"next"`
	Bags []*redbag `json:"bags"`
}

type redbagDay struct {
	Day  int64           `json:"day"`
	Sent int64           `json:"sent"`
	Kind map[int64]int64 `json:"kind"`
	Recv int64           `json:"recv"`
}

var redbagMu sync.Mutex

// left is what is still in the bag and shares what is still to be opened.
func (b *redbag) left() (val, shares int64) {
	val = b.Val
	for _, c := range b.Claims {
		val -= c.Val
	}
	return val, b.Max - int64(len(b.Claims))
}

func (b *redbag) claimedBy(rid int64) bool {
	for _, c := range b.Claims {
		if c.RID == rid {
			return true
		}
	}
	return false
}

// redbagShare is the next opener's share: a random part of what is left, at most twice the average share and at least 1,
// the last share takes the rest.
func redbagShare(left, shares int64) int64 {
	if shares <= 1 {
		return left
	}
	hi := min(left/shares*2, left-(shares-1))
	if hi < 1 {
		return 1
	}
	return 1 + rand.Int63n(hi)
}

func (g *GuildService) loadRedbags(now time.Time) (*redbagBook, error) {
	b := &redbagBook{}
	if _, err := g.Store.LoadGlobal(g.SrvID, redbagModule, b); err != nil {
		return nil, err
	}
	keep := b.Bags[:0]
	for _, x := range b.Bags {
		if x.Sent+redbagKeep > now.Unix() {
			keep = append(keep, x)
		}
	}
	b.Bags = keep
	return b, nil
}

func (g *GuildService) redbagDay(rid int64, now time.Time) (*redbagDay, error) {
	d := &redbagDay{}
	if _, err := g.Store.LoadState(rid, redbagDayMod, d); err != nil {
		return nil, err
	}
	if day := dayKey(now); d.Day != day {
		*d = redbagDay{Day: day}
	}
	if d.Kind == nil {
		d.Kind = map[int64]int64{}
	}
	return d, nil
}

// redbagRow is the line of a red bag in the list (13534) as the role sees it.
func (g *GuildService) redbagRow(b *redbag, rid int64) proto.Message {
	flag := int64(0)
	if b.claimedBy(rid) {
		flag = 1
	}
	return proto.Message{"id": b.ID, "type": b.Kind, "name": b.Name, "post": b.Post, "num": len(b.Claims), "max_num": b.Max, "max_val": b.Val,
		"time": b.Sent + redbagLife, "flag": flag, "msg_id": 0}
}

func (g *GuildService) registerRedbag(r *gateway.Router) {
	r.Handle(13534, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if err != nil {
			return s.Send(13534, proto.Message{"type": 0, "list": []proto.Message{}})
		}
		redbagMu.Lock()
		defer redbagMu.Unlock()
		book, err := g.loadRedbags(s.Now())
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, b := range book.Bags {
			if b.GID == gd.GID {
				list = append(list, g.redbagRow(b, role.RID))
			}
		}
		return s.Send(13534, proto.Message{"type": 0, "list": list})
	})
	r.Handle(13546, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d, err := g.redbagDay(role.RID, s.Now())
		if err != nil {
			return err
		}
		list := []proto.Message{}
		ids := make([]int64, 0, len(d.Kind))
		for id := range d.Kind {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			list = append(list, proto.Message{"id": id, "num": d.Kind[id]})
		}
		return s.Send(13546, proto.Message{"list": list})
	})
	// 13535 send: the red bag item (the red packet token) sends one; sending for money has no payment behind it
	r.Handle(13535, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(13535, proto.Message{"code": 0, "msg": msg, "day_send_num": 0}) }
		gd, m, err := g.mine(s)
		if err != nil {
			return refuse("你还没有加入公会")
		}
		kind, ok := redbagCfg[req.Int("type")]
		if !ok {
			return refuse("红包不存在")
		}
		redbagMu.Lock()
		defer redbagMu.Unlock()
		d, err := g.redbagDay(role.RID, s.Now())
		if err != nil {
			return err
		}
		switch {
		case req.Int("loss_type") != redbagSendItem || len(kind.LossItem) == 0:
			return refuse("暂不支持充值发红包")
		case d.Sent >= g.cfg("red_packet_send"):
			return refuse("今日发红包次数已达上限")
		case d.Kind[kind.ID] >= kind.Limit:
			return refuse("该红包今日发放次数已达上限")
		}
		book, err := g.loadRedbags(s.Now())
		if err != nil {
			return err
		}
		spendA, spendI := g.payFor(kind.LossItem)
		gainA, gainI := g.payFor(kind.Reward)
		if err := g.Store.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			return refuse("红包令不足")
		}
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		book.Next++
		b := &redbag{ID: book.Next, GID: gd.GID, Kind: kind.ID, RID: role.RID, Name: role.Name, Face: face, Post: m.Post, Val: kind.Val, Max: kind.Num, Sent: s.Now().Unix()}
		book.Bags = append(book.Bags, b)
		if err := g.Store.SaveGlobal(g.SrvID, redbagModule, book); err != nil {
			return err
		}
		d.Sent++
		d.Kind[kind.ID]++
		if err := g.Store.SaveState(role.RID, redbagDayMod, d); err != nil {
			return err
		}
		if err := s.Send(13535, proto.Message{"code": 1, "msg": "发放成功", "day_send_num": d.Sent}); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, g.Store); err != nil {
			return err
		}
		rows, err := g.Store.MembersOf(gd.GID)
		if err != nil {
			return nil
		}
		for _, mem := range rows {
			if sess := g.Hub.Online(mem.RID); sess != nil {
				sess.Send(13534, proto.Message{"type": 1, "list": []proto.Message{g.redbagRow(b, mem.RID)}})
			}
		}
		return nil
	})
	// 13536 open
	r.Handle(13536, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(13536, proto.Message{"code": 0, "msg": msg, "id": req.Int("id"), "type": 0, "val": 0, "day_recv_num": 0})
		}
		gd, m, err := g.mine(s)
		if err != nil {
			return refuse("你还没有加入公会")
		}
		redbagMu.Lock()
		defer redbagMu.Unlock()
		book, err := g.loadRedbags(s.Now())
		if err != nil {
			return err
		}
		var b *redbag
		for _, x := range book.Bags {
			if x.ID == req.Int("id") && x.GID == gd.GID {
				b = x
			}
		}
		d, err := g.redbagDay(role.RID, s.Now())
		if err != nil {
			return err
		}
		left, shares := int64(0), int64(0)
		if b != nil {
			left, shares = b.left()
		}
		switch {
		case b == nil:
			return refuse("红包不存在")
		case b.claimedBy(role.RID):
			return refuse("你已经领过这个红包了")
		case b.Sent+redbagLife <= s.Now().Unix():
			return refuse("红包已过期")
		case shares <= 0:
			return refuse("红包已被抢完")
		case d.Recv >= g.cfg("red_packet_get"):
			return refuse("今日领取红包次数已达上限")
		}
		val := redbagShare(left, shares)
		kind := redbagCfg[b.Kind]
		if err := g.Store.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{redbagAsset(kind.Assets): val}, map[int64]int64{}); err != nil {
			return err
		}
		face := int64(role.FaceID)
		if face == 0 {
			face = store.DefaultFaceID
		}
		b.Claims = append(b.Claims, redbagClaim{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Face: face, Post: m.Post, Val: val, Time: s.Now().Unix()})
		if err := g.Store.SaveGlobal(g.SrvID, redbagModule, book); err != nil {
			return err
		}
		d.Recv++
		if err := g.Store.SaveState(role.RID, redbagDayMod, d); err != nil {
			return err
		}
		if err := s.Send(13536, proto.Message{"code": 1, "msg": "领取成功", "id": b.ID, "type": b.Kind, "val": val, "day_recv_num": d.Recv}); err != nil {
			return err
		}
		if err := sendAssets(s, g.Store, role.RID); err != nil {
			return err
		}
		rows, err := g.Store.MembersOf(gd.GID)
		if err != nil {
			return nil
		}
		for _, mem := range rows { // the others see the count change
			if sess := g.Hub.Online(mem.RID); sess != nil && mem.RID != role.RID {
				sess.Send(13534, proto.Message{"type": 1, "list": []proto.Message{g.redbagRow(b, mem.RID)}})
			}
		}
		return nil
	})
	// 13540 who opened a red bag
	r.Handle(13540, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if err != nil {
			return nil
		}
		redbagMu.Lock()
		defer redbagMu.Unlock()
		book, err := g.loadRedbags(s.Now())
		if err != nil {
			return err
		}
		for _, b := range book.Bags {
			if b.ID != req.Int("id") || b.GID != gd.GID {
				continue
			}
			list := []proto.Message{}
			for _, c := range b.Claims {
				list = append(list, proto.Message{"rid": c.RID, "srv_id": c.SrvID, "name": c.Name, "face_id": c.Face, "avatar_bid": 0, "post": c.Post,
					"val": c.Val, "time": c.Time, "face_update_time": 0, "face_file": ""})
			}
			return s.Send(13540, proto.Message{"id": b.ID, "type": b.Kind, "name": b.Name, "face_id": b.Face, "face_update_time": 0, "face_file": "",
				"avatar_bid": 0, "post": b.Post, "val": b.Val, "list": list})
		}
		return nil
	})
	// 13545 the senders of the guild, the most generous first
	r.Handle(13545, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		gd, _, err := g.mine(s)
		if err != nil {
			return s.Send(13545, proto.Message{"list": []proto.Message{}})
		}
		redbagMu.Lock()
		defer redbagMu.Unlock()
		book, err := g.loadRedbags(s.Now())
		if err != nil {
			return err
		}
		type row struct {
			b          *redbag
			price, num int64
		}
		by := map[int64]*row{}
		for _, b := range book.Bags {
			if b.GID != gd.GID {
				continue
			}
			if by[b.RID] == nil {
				by[b.RID] = &row{b: b}
			}
			by[b.RID].price += redbagCfg[b.Kind].Price
			by[b.RID].num++
		}
		rows := make([]*row, 0, len(by))
		for _, x := range by {
			rows = append(rows, x)
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].price != rows[j].price {
				return rows[i].price > rows[j].price
			}
			return rows[i].b.RID < rows[j].b.RID
		})
		list := []proto.Message{}
		for _, x := range rows {
			list = append(list, proto.Message{"rid": x.b.RID, "srv_id": g.SrvID, "name": x.b.Name, "face_id": x.b.Face, "avatar_bid": 0,
				"price": x.price, "num": x.num, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(13545, proto.Message{"list": list})
	})
}

// redbagCounts are the red bags the role sent and opened today (the day_send_num / day_recv_num of 13523).
func (g *GuildService) redbagCounts(rid int64, now time.Time) (sent, recv int64) {
	d, err := g.redbagDay(rid, now)
	if err != nil {
		return 0, 0
	}
	return d.Sent, d.Recv
}
