package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// skinlottery.json: the world recruit (世界征兵, 26600~26602) of the client (tools/extract_skinlottery.py).
//
//go:embed data/skinlottery.json
var skinLotteryJSON []byte

const (
	slModule  = "skinlottery"
	slSpecial = 1 // the prize at this position is the one the activity is about (the first of the table)
	slFirst   = 5 // the special prize can come from this draw on, and surely by the last
)

type slPrize struct{ ID, Num int64 }

type slCamp struct {
	Token  int64
	Costs  []int64
	Prizes []slPrize
}

var slCamps = func() map[int64]*slCamp {
	var f struct {
		Camps map[string]*slCamp `json:"camps"`
	}
	if err := json.Unmarshal(skinLotteryJSON, &f); err != nil || len(f.Camps) == 0 {
		panic("game: bad skinlottery.json")
	}
	out := map[int64]*slCamp{}
	for k, c := range f.Camps {
		id, _ := strconv.ParseInt(k, 10, 64)
		out[id] = c
	}
	return out
}()

type slRole struct {
	Camp, Start int64
	Drawn       []int64 // the positions drawn, in order
}

func slOpen(st *store.Store, role *store.Role, now time.Time) (camp *slCamp, id, start, end int64, ok bool) {
	ids := make([]int64, 0, len(slCamps))
	for id := range slCamps {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if a, b, open := ActivityOpen(st, role.SrvID, id, now); open {
			return slCamps[id], id, a, b, true
		}
	}
	return nil, 0, 0, 0, false
}

// slPick draws the next position: the special prize with a chance that grows from the fifth draw (certain at the
// last), otherwise one of the other prizes left. Ours: the client names no chances.
func slPick(n int64, drawn []int64, prizes int, rng *rand.Rand) int64 {
	has := func(p int64) bool {
		for _, d := range drawn {
			if d == p {
				return true
			}
		}
		return false
	}
	if !has(slSpecial) {
		last := int64(prizes)
		if n >= last || (n >= slFirst && rng.Int63n(last-slFirst+2) < n-slFirst+1) {
			return slSpecial
		}
	}
	var left []int64
	for p := int64(1); p <= int64(prizes); p++ {
		if !has(p) && p != slSpecial {
			left = append(left, p)
		}
	}
	return left[rng.Intn(len(left))]
}

// RegisterSkinLottery registers the world recruit (an operator opens it with the lottery id of the table as the
// camp_id): ten prizes, each position drawn once, the first draw free and the n-th costing the tokens the table
// names; the special prize comes by the tenth draw at the latest.
func RegisterSkinLottery(r *gateway.Router, st *store.Store) {
	fights := fightServiceOf(st)
	rng := func() *rand.Rand {
		if fights.Rand != nil {
			return fights.Rand()
		}
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	load := func(s *gateway.Session) (*store.Role, *slRole, *slCamp, int64, int64, bool, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil, 0, 0, false, nil
		}
		camp, id, start, end, open := slOpen(st, role, s.Now())
		d := &slRole{}
		if _, err := st.LoadState(role.RID, slModule, d); err != nil {
			return nil, nil, nil, 0, 0, false, err
		}
		if open && (d.Camp != id || d.Start != start) {
			*d = slRole{Camp: id, Start: start}
		}
		return role, d, camp, id, end, open, nil
	}
	r.Handle(26601, func(s *gateway.Session, _ proto.Message) error {
		role, d, _, id, end, open, err := load(s)
		if err != nil || role == nil {
			return err
		}
		sorts := []proto.Message{}
		if open {
			for _, p := range d.Drawn {
				sorts = append(sorts, proto.Message{"sort": p})
			}
		}
		left := int64(0)
		if open {
			left = max(0, end-s.Now().Unix())
			if end == 0 {
				left = actForever
			}
		}
		return s.Send(26601, proto.Message{"time": len(d.Drawn), "last_time": left, "lottery_id": id, "sort_list": sorts})
	})
	r.Handle(26600, func(s *gateway.Session, _ proto.Message) error {
		role, d, camp, _, _, open, err := load(s)
		if err != nil || role == nil {
			return err
		}
		refuse := func(msg string) error {
			return s.Send(26600, proto.Message{"flag": 0, "msg": msg, "time": len(d.Drawn), "sort": 0})
		}
		n := int64(len(d.Drawn)) + 1
		if !open {
			return refuse("活动已结束")
		}
		if n > int64(len(camp.Prizes)) {
			return refuse("已经征兵完毕")
		}
		spendI := map[int64]int64{}
		if cost := camp.Costs[n-1]; cost > 0 {
			spendI[camp.Token] = cost
		}
		pos := slPick(n, d.Drawn, len(camp.Prizes), rng())
		prize := camp.Prizes[pos-1]
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains([]sellGain{{prize.ID, prize.Num}}, 1, ga, gi)
		if err := st.Exchange(role.RID, map[int64]int64{}, spendI, ga, gi); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return refuse("世界征兵令不足")
			}
			return err
		}
		d.Drawn = append(d.Drawn, pos)
		if err := st.SaveState(role.RID, slModule, d); err != nil {
			return err
		}
		if err := s.Send(26600, proto.Message{"flag": 1, "msg": "", "time": len(d.Drawn), "sort": pos}); err != nil {
			return err
		}
		if err := s.Send(26602, proto.Message{"reward_list": []proto.Message{{"bid": prize.ID, "num": prize.Num}}}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
