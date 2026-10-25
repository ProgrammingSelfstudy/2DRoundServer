package game

import (
	_ "embed"
	"encoding/json"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// smashegg.json: the egg smashing event of the client (tools/extract_smashegg.py).
//
//go:embed data/smashegg.json
var smasheggJSON []byte

const (
	eggModule    = "smashegg"
	eggLogModule = "smashegg_log"
	eggDiamond   = 3
	eggExp       = 5 // the captain's experience, given with every hammer bought
	eggEventDays = 7 // the event is always open; the end time sent is this far ahead
)

type eggConf struct {
	Const  map[string]int64 `json:"const"`
	Awards []struct {
		Bid int64 `json:"bid"`
		Num int64 `json:"num"`
	} `json:"awards"`
}

var eggCfg = func() *eggConf {
	c := &eggConf{}
	if err := json.Unmarshal(smasheggJSON, c); err != nil || c.Const["egg_num"] == 0 || len(c.Awards) != 4 {
		panic("game: bad smashegg.json")
	}
	return c
}()

// eggBestWeights are the chances of the four best prizes (the rule text of the client: 15%, 15%, 35%, 35%) in the order
// of the client's list of them.
var eggBestWeights = [4]int{15, 15, 35, 35}

// eggPlain is what a plain egg gives: 10% a skin shard, 10% a hundred of the training item and the rest one of these
// (the client says "other items" for 80% and does not list them).
var eggPlain = [][2]int64{{1, 50000}, {22, 20000}, {10410, 5}, {10001, 5}, {10003, 3}, {10412, 2}}

type eggSlot struct {
	Best  bool
	Smash bool
	Prize [2]int64 // item, amount of the prize once smashed
}

type eggLogRow struct {
	Name string
	Bid  int64
	Num  int64
}

type eggState struct {
	Day   int64
	Lucky int64
	Free  int64
	Eggs  []eggSlot
	Log   []eggLogRow
}

type eggLog struct{ Rows []eggLogRow }

type eggSvc struct{ st *store.Store }

// RegisterSmashegg registers the egg smashing event (16680-16685): six eggs smashed with small golden hammers, one of
// them a best egg now and then (more likely the higher the luck, which every egg smashed raises), a refresh of the six
// (one free a day, then for diamonds; free again once all six are smashed), buying hammers and the records of prizes of
// the role and of the server. The event is always open. Our rules, not captured.
func RegisterSmashegg(r *gateway.Router, db *store.Store) {
	e := &eggSvc{st: db}
	r.Handle(16680, e.info)
	r.Handle(16681, e.refresh)
	r.Handle(16682, e.smash)
	r.Handle(16683, e.smashAll)
	r.Handle(16684, e.buy)
	r.Handle(16685, e.records)
}

func (e *eggSvc) with(s *gateway.Session, fn func(role *store.Role, st *eggState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &eggState{}
	if _, err := e.st.LoadState(role.RID, eggModule, st); err != nil {
		return err
	}
	if day := dayKey(s.Now()); st.Day != day {
		st.Day, st.Free = day, eggCfg.Const["free_time"]
	}
	if len(st.Eggs) == 0 {
		st.deal(newRng().Intn)
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, eggModule, st)
}

func eggWeighted(n func(int) int, weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	x := n(total)
	for i, w := range weights {
		if x < w {
			return i
		}
		x -= w
	}
	return len(weights) - 1
}

// deal puts six fresh eggs down. A best egg comes with the chance luck / luck max (always at the maximum) and clears the luck.
func (st *eggState) deal(n func(int) int) {
	count, max := int(eggCfg.Const["egg_num"]), eggCfg.Const["lucky_max"]
	st.Eggs = make([]eggSlot, count)
	if st.Lucky >= max || (st.Lucky > 0 && int64(n(int(max))) < st.Lucky) {
		st.Eggs[n(count)].Best = true
		st.Lucky = 0
	}
}

// open decides what the egg at i holds.
func (st *eggState) open(i int, n func(int) int) {
	eg := &st.Eggs[i]
	eg.Smash = true
	if eg.Best {
		a := eggCfg.Awards[eggWeighted(n, eggBestWeights[:])]
		eg.Prize = [2]int64{a.Bid, a.Num}
	} else {
		switch x := n(100); {
		case x < 10:
			eg.Prize = [2]int64{29, 1}
		case x < 20:
			eg.Prize = [2]int64{10450, 100}
		default:
			eg.Prize = eggPlain[n(len(eggPlain))]
		}
	}
	if max := eggCfg.Const["lucky_max"]; st.Lucky < max {
		st.Lucky = min(max, st.Lucky+eggCfg.Const["add_lucky"])
	}
}

func (st *eggState) left() (n int) {
	for _, eg := range st.Eggs {
		if !eg.Smash {
			n++
		}
	}
	return
}

func (e *eggSvc) sendInfo(s *gateway.Session, st *eggState) error {
	eggs := []proto.Message{}
	for i, eg := range st.Eggs {
		typ, status, show := int64(1), int64(0), []proto.Message{}
		if eg.Best {
			typ = 2
		}
		if eg.Smash {
			status = 1
			show = append(show, proto.Message{"item_id": eg.Prize[0], "num": eg.Prize[1]})
		}
		eggs = append(eggs, proto.Message{"pos": i + 1, "type": typ, "status": status, "show_reward": show})
	}
	return s.Send(16680, proto.Message{"lucky": st.Lucky, "free_num": st.Free, "endtime": s.Now().Add(eggEventDays * 24 * time.Hour).Unix(), "egg_status": eggs})
}

func (e *eggSvc) info(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(_ *store.Role, st *eggState) error { return e.sendInfo(s, st) })
}

func eggReply(s *gateway.Session, cmd uint16, ok bool, msg string) error {
	code := int64(0)
	if ok {
		code = 1
	}
	return s.Send(cmd, proto.Message{"code": code, "msg": msg})
}

// refresh (16681): the free one first, then for diamonds.
func (e *eggSvc) refresh(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *eggState) error {
		if st.Free > 0 {
			st.Free--
		} else if err := e.st.Exchange(role.RID, map[int64]int64{eggDiamond: eggCfg.Const["refresh_gold"]}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}); err != nil {
			return eggReply(s, 16681, false, "钻石不足")
		} else if err := sendAssets(s, e.st, role.RID); err != nil {
			return err
		}
		st.deal(newRng().Intn)
		if err := eggReply(s, 16681, true, ""); err != nil {
			return err
		}
		return e.sendInfo(s, st)
	})
}

// take smashes the eggs at the positions with the hammers of the role, pays the prizes and writes the records.
func (e *eggSvc) take(s *gateway.Session, role *store.Role, st *eggState, cmd uint16, which []int) error {
	hammer := eggCfg.Const["item_bid"]
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{hammer: int64(len(which))}, map[int64]int64{}, map[int64]int64{}); err != nil {
		return eggReply(s, cmd, false, "小金锤数量不足")
	}
	n := newRng().Intn
	assets, items := map[int64]int64{}, map[int64]int64{}
	var won []eggLogRow
	for _, i := range which {
		st.open(i, n)
		p := st.Eggs[i].Prize
		splitGains([]sellGain{{p[0], p[1]}}, 1, assets, items)
		won = append(won, eggLogRow{Name: role.Name, Bid: p[0], Num: p[1]})
	}
	if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, assets, items); err != nil {
		return err
	}
	keep := int(eggCfg.Const["record_len"])
	for _, w := range won {
		st.Log = append([]eggLogRow{w}, st.Log...)
	}
	if len(st.Log) > keep {
		st.Log = st.Log[:keep]
	}
	all := &eggLog{}
	if _, err := e.st.LoadGlobal(role.SrvID, eggLogModule, all); err != nil {
		return err
	}
	for _, w := range won {
		all.Rows = append([]eggLogRow{w}, all.Rows...)
	}
	if len(all.Rows) > keep {
		all.Rows = all.Rows[:keep]
	}
	if err := e.st.SaveGlobal(role.SrvID, eggLogModule, all); err != nil {
		return err
	}
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	if err := sendBackpack(s, e.st); err != nil {
		return err
	}
	if err := eggReply(s, cmd, true, ""); err != nil {
		return err
	}
	if err := e.sendInfo(s, st); err != nil {
		return err
	}
	if st.left() > 0 {
		return nil
	}
	// all smashed: the next six come by themselves, for nothing
	st.deal(n)
	return e.sendInfo(s, st)
}

// smash (16682): one egg.
func (e *eggSvc) smash(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, st *eggState) error {
		i := int(req.Int("pos")) - 1
		if i < 0 || i >= len(st.Eggs) {
			return eggReply(s, 16682, false, "金蛋不存在")
		}
		if st.Eggs[i].Smash {
			return eggReply(s, 16682, false, "这个蛋已经砸开了")
		}
		return e.take(s, role, st, 16682, []int{i})
	})
}

// smashAll (16683): every egg left.
func (e *eggSvc) smashAll(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *eggState) error {
		var which []int
		for i, eg := range st.Eggs {
			if !eg.Smash {
				which = append(which, i)
			}
		}
		if len(which) == 0 {
			return eggReply(s, 16683, false, "没有可以砸的蛋")
		}
		return e.take(s, role, st, 16683, which)
	})
}

// buy (16684): num hammers for diamonds, each with some captain experience.
func (e *eggSvc) buy(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, st *eggState) error {
		num := req.Int("num")
		if num <= 0 || num > 9999 {
			return eggReply(s, 16684, false, "数量不对")
		}
		err := e.st.Exchange(role.RID, map[int64]int64{eggDiamond: num * eggCfg.Const["item_price"]}, map[int64]int64{},
			map[int64]int64{eggExp: num * eggCfg.Const["give_exp"]}, map[int64]int64{eggCfg.Const["item_bid"]: num})
		if err != nil {
			return eggReply(s, 16684, false, "钻石不足")
		}
		if err := sendAssets(s, e.st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, e.st); err != nil {
			return err
		}
		return eggReply(s, 16684, true, "购买成功")
	})
}

// records (16685): type 1 the role's own prizes, 2 the server's.
func (e *eggSvc) records(s *gateway.Session, req proto.Message) error {
	return e.with(s, func(role *store.Role, st *eggState) error {
		typ, rows := req.Int("type"), st.Log
		if typ == 2 {
			all := &eggLog{}
			if _, err := e.st.LoadGlobal(role.SrvID, eggLogModule, all); err != nil {
				return err
			}
			rows = all.Rows
		}
		list := []proto.Message{}
		for _, r := range rows {
			list = append(list, proto.Message{"role_name": r.Name, "bid": r.Bid, "num": r.Num})
		}
		return s.Send(16685, proto.Message{"type": typ, "log_list": list})
	})
}
