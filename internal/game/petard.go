package game

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// petard.json: the fireworks party (花火大会) of the client (tools/extract_petard.py).
//
//go:embed data/petard.json
var petardJSON []byte

const (
	petardModule  = "petard"
	petardCamp    = 93037 // ActionRankCommonType.petard: the camp_id the operator gives the activity in the schedule
	petardPackets = 1000  // diamonds in a red bag: ours, the table has none
	petardMinPart = 10    // the least a share of it holds
	petardList    = 50    // red bags the list shows
)

var petardCfg = func() (c struct {
	Const  map[string]int64    `json:"const"`
	Back   map[string][2]int64 `json:"back"` // what a meteor / a cannon gives back: [asset, amount]
	Awards []struct {
		ID, Count int64
		Award     [][2]int64
	} `json:"awards"`
	Wishes []struct{ ID, Pro int64 } `json:"wishes"`
}) {
	if err := json.Unmarshal(petardJSON, &c); err != nil || len(c.Awards) == 0 {
		panic("game: bad petard.json")
	}
	return
}()

type petardClaim struct {
	RID   int64  `json:"rid"`
	SrvID string `json:"srv"`
	Name  string `json:"name"`
	Lev   int64  `json:"lev"`
	Face  int64  `json:"face"`
	Time  int64  `json:"time"`
	Red   int64  `json:"red"`
	Val   int64  `json:"val"`
}

type petardPacket struct {
	ID     int64         `json:"id"`
	RID    int64         `json:"rid"`
	Name   string        `json:"name"`
	Face   int64         `json:"face"`
	Wish   int64         `json:"wish"`
	Total  int64         `json:"total"`
	Sent   int64         `json:"sent"`
	Claims []petardClaim `json:"claims"`
}

type petardWorld struct {
	Start   int64
	Heat    int64
	Next    int64
	Packets []*petardPacket
	History []petardClaim // the newest claims first, for the news (27006)
}

type petardRole struct {
	Start   int64
	Day     int64
	Sent    int64
	Recv    int64
	Claimed []int64 // the heat awards taken
}

var petardMu sync.Mutex

func (p *petardPacket) left() (val, shares int64) {
	val = p.Total
	for _, c := range p.Claims {
		val -= c.Val
	}
	return val, petardCfg.Const["max_get_num"] - int64(len(p.Claims))
}

func (p *petardPacket) claimedBy(rid int64) *petardClaim {
	for i := range p.Claims {
		if p.Claims[i].RID == rid {
			return &p.Claims[i]
		}
	}
	return nil
}

func (p *petardPacket) expired(now time.Time) bool {
	return p.Sent+petardCfg.Const["effect_time"]*3600 <= now.Unix()
}

func petardWish() int64 {
	total := int64(0)
	for _, w := range petardCfg.Wishes {
		total += w.Pro
	}
	r := int64(summonRand() * float64(total))
	for _, w := range petardCfg.Wishes {
		if r -= w.Pro; r < 0 {
			return w.ID
		}
	}
	return petardCfg.Wishes[0].ID
}

func petardClaimMsg(c petardClaim) proto.Message {
	return proto.Message{"r_rid": c.RID, "r_srvid": c.SrvID, "red_id": c.Red, "name": c.Name, "lev": c.Lev, "face_id": c.Face, "avatar_bid": 0,
		"time": c.Time, "item": []proto.Message{{"item_id": 3, "num": c.Val}}}
}

// RegisterPetard registers the fireworks party (27000-27008): meteors (any time) and firework cannons (19:00-23:00) are
// fired for items, personal points and the heat of the whole server; a cannon also sends a red bag of diamonds to everybody
// on the server; the heat gives awards. It runs while the operator has the activity with camp_id 93037 open in the
// schedule. The red bag's diamonds, its shares and the heat that one item adds are ours; the table has none of them.
func RegisterPetard(r *gateway.Router, st *store.Store, hub *gateway.Hub) {
	load := func(role *store.Role, now time.Time) (*petardRole, *petardWorld, int64, bool, error) {
		start, end, open := ActivityOpen(st, role.SrvID, petardCamp, now)
		d, w := &petardRole{}, &petardWorld{}
		if _, err := st.LoadState(role.RID, petardModule, d); err != nil {
			return nil, nil, 0, false, err
		}
		if _, err := st.LoadGlobal(role.SrvID, petardModule, w); err != nil {
			return nil, nil, 0, false, err
		}
		if d.Start != start {
			*d = petardRole{Start: start}
		}
		if w.Start != start {
			*w = petardWorld{Start: start}
		}
		if day := dayKey(now); d.Day != day {
			d.Day, d.Sent, d.Recv = day, 0, 0
		}
		if end == 0 {
			end = now.Unix() + actForever
		}
		return d, w, end, open, nil
	}
	maxHeat := petardCfg.Const["base_max_score"]
	threshold := func(count int64) int64 { return count * maxHeat / 1000 }
	info := func(d *petardRole, w *petardWorld, end int64) proto.Message {
		list := []proto.Message{}
		for _, a := range petardCfg.Awards {
			status := int64(0)
			switch {
			case has(d.Claimed, a.ID):
				status = 2
			case w.Heat >= threshold(a.Count):
				status = 1
			}
			list = append(list, proto.Message{"id": a.ID, "status": status})
		}
		return proto.Message{"end_time": end, "score": w.Heat, "lev_score": maxHeat, "score_award": list}
	}
	r.Handle(27000, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		d, w, end, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		return s.Send(27000, info(d, w, end))
	})
	r.Handle(27001, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		c := petardCfg.Const
		item, num := req.Int("id"), req.Int("num")
		refuse := func(msg string) error {
			return s.Send(27001, proto.Message{"id": item, "code": 0, "msg": msg, "flag": 0})
		}
		isCannon := item == c["firework_bid"]
		if (!isCannon && item != c["meteor_bid"]) || num < 1 || num > 1000 {
			return refuse("参数错误")
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		d, w, end, open, err := load(role, s.Now())
		if err != nil {
			return err
		}
		if !open {
			return refuse("活动未开启")
		}
		if h := int64(s.Now().In(dayZone).Hour()); isCannon && (h < c["petard_start_time"] || h >= c["petard_end_time"]) {
			return refuse(fmt.Sprintf("请在%d:00-%d:00之间使用", c["petard_start_time"], c["petard_end_time"]))
		}
		each, backLabel := c["meteor_score"], petardCfg.Back["meteor"]
		if isCannon {
			each, backLabel = c["firework_score"], petardCfg.Back["firework"]
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{backLabel[0], backLabel[1] * num}}, 1, ga, gi)
		if ItemKnown(c["petard_score"]) {
			gi[c["petard_score"]] += each * num
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{item: num}, ga, gi); err != nil {
			return refuse("道具不足")
		}
		w.Heat += each * num
		sent := int64(0)
		var news []*petardPacket
		if isCannon {
			face := int64(role.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			for i := int64(0); i < num && d.Sent < c["max_day_send_num"]; i++ {
				w.Next++
				p := &petardPacket{ID: w.Next, RID: role.RID, Name: role.Name, Face: face, Wish: petardWish(), Total: petardPackets, Sent: s.Now().Unix()}
				w.Packets = append(w.Packets, p)
				news = append(news, p)
				d.Sent++
				sent++
			}
		}
		keep := w.Packets[:0]
		for _, p := range w.Packets {
			if p.Sent+7*24*3600 > s.Now().Unix() {
				keep = append(keep, p)
			}
		}
		w.Packets = keep
		if err := st.SaveState(role.RID, petardModule, d); err != nil {
			return err
		}
		if err := st.SaveGlobal(role.SrvID, petardModule, w); err != nil {
			return err
		}
		if err := s.Send(27001, proto.Message{"id": item, "code": 1, "msg": "燃放成功", "flag": sent}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		if err := s.Send(27000, info(d, w, end)); err != nil {
			return err
		}
		if isCannon && hub != nil { // everybody sees the fireworks and the red dot of the new red bags
			hub.Each(func(rid int64, other *gateway.Session) {
				if o := other.Role(); o == nil || o.SrvID != role.SrvID {
					return
				}
				other.Send(27008, proto.Message{"flag": 1, "msg": fmt.Sprintf("%s燃放了礼花炮", role.Name)})
				if len(news) > 0 {
					other.Send(27004, proto.Message{"code": 1, "msg": ""})
				}
			})
		}
		return nil
	})
	r.Handle(27002, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		d, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for i := len(w.Packets) - 1; i >= 0 && len(list) < petardList; i-- {
			p := w.Packets[i]
			if p.expired(s.Now()) {
				continue
			}
			status := int64(0)
			left, shares := p.left()
			switch {
			case p.claimedBy(role.RID) != nil:
				status = 2
			case left > 0 && shares > 0:
				status = 1
			}
			list = append(list, proto.Message{"red_packet_id": p.ID, "msg_id": p.Wish, "name": p.Name, "status": status})
		}
		return s.Send(27002, proto.Message{"red_packet_list": list, "get_num": d.Recv, "max_num": petardCfg.Const["max_day_get_num"]})
	})
	r.Handle(27003, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(27003, proto.Message{"code": 0, "msg": msg, "get_red_packet_list": []proto.Message{}})
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		d, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		var p *petardPacket
		for _, x := range w.Packets {
			if x.ID == req.Int("red_packet_id") {
				p = x
			}
		}
		if p == nil {
			return refuse("红包不存在")
		}
		left, shares := p.left()
		mine := p.claimedBy(role.RID)
		msg := "查看成功"
		if mine == nil {
			switch {
			case p.expired(s.Now()):
				return refuse("红包已过期")
			case shares <= 0 || left <= 0:
				return refuse("红包已被抢完")
			case d.Recv >= petardCfg.Const["max_day_get_num"]:
				return refuse("今日领取红包次数已达上限")
			}
			val := min(petardMinPart+rand63(left-petardMinPart*shares+1), left)
			if shares == 1 {
				val = left
			}
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, map[int64]int64{3: val}, map[int64]int64{}); err != nil {
				return err
			}
			face := int64(role.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			c := petardClaim{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Lev: int64(role.Lev), Face: face, Time: s.Now().Unix(), Red: p.ID, Val: val}
			p.Claims = append(p.Claims, c)
			w.History = append([]petardClaim{c}, w.History...)
			if n := int(petardCfg.Const["broadcast_num"]); len(w.History) > n {
				w.History = w.History[:n]
			}
			d.Recv++
			if err := st.SaveState(role.RID, petardModule, d); err != nil {
				return err
			}
			if err := st.SaveGlobal(role.SrvID, petardModule, w); err != nil {
				return err
			}
			msg = "领取成功"
			defer sendAssets(s, st, role.RID)
		}
		list := []proto.Message{}
		for _, c := range p.Claims {
			list = append(list, petardClaimMsg(c))
		}
		return s.Send(27003, proto.Message{"code": 1, "msg": msg, "get_red_packet_list": list, "red_packet_id": p.ID, "get_num": len(p.Claims),
			"max_num": petardCfg.Const["max_get_num"], "end_time": p.Sent + petardCfg.Const["effect_time"]*3600, "send_name": p.Name, "send_face_id": p.Face, "send_avatar_bid": 0})
	})
	r.Handle(27006, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		_, w, _, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, c := range w.History {
			list = append(list, petardClaimMsg(c))
		}
		return s.Send(27006, proto.Message{"get_red_packet_list": list})
	})
	r.Handle(27007, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error {
			return s.Send(27007, proto.Message{"code": 0, "msg": msg, "id": req.Int("id")})
		}
		petardMu.Lock()
		defer petardMu.Unlock()
		d, w, end, _, err := load(role, s.Now())
		if err != nil {
			return err
		}
		idx := sort.Search(len(petardCfg.Awards), func(i int) bool { return petardCfg.Awards[i].ID >= req.Int("id") })
		if idx == len(petardCfg.Awards) || petardCfg.Awards[idx].ID != req.Int("id") {
			return refuse("奖励不存在")
		}
		a := petardCfg.Awards[idx]
		switch {
		case has(d.Claimed, a.ID):
			return refuse("已经领取过了")
		case w.Heat < threshold(a.Count):
			return refuse("热度不够")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, x := range a.Award {
			if ItemKnown(x[0]) {
				splitGains([]sellGain{{x[0], x[1]}}, 1, ga, gi)
			}
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		d.Claimed = append(d.Claimed, a.ID)
		if err := st.SaveState(role.RID, petardModule, d); err != nil {
			return err
		}
		if err := s.Send(27007, proto.Message{"code": 1, "msg": "领取成功", "id": a.ID}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return s.Send(27000, info(d, w, end))
	})
}

// rand63 is a number from 0 to n-1 (0 when n is not above 1).
func rand63(n int64) int64 {
	if n <= 1 {
		return 0
	}
	return int64(summonRand() * float64(n))
}
