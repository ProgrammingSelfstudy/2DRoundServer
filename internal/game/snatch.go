package game

import (
	_ "embed"
	"encoding/json"
	"sort"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// snatch.json: the holiday snatch (节日夺宝, festivalaction 25700~25704) of the client (tools/extract_snatch.py).
//
//go:embed data/snatch.json
var snatchJSON []byte

const (
	snatchModule = "snatch"
	snatchCamp   = 993029 // ActionRankCommonType.action_treasure: the camp_id the operator gives the activity in the schedule
	snatchLogMax = 30
)

type snatchGood struct {
	ID, Group int64
	Name      string
	Award     [][2]int64
	Expend    [][2]int64
	Limit     int64
	LimitRole int64 `json:"limit_role"`
}

var snatchCfg = func() (out struct {
	Const map[string]json.RawMessage
	Goods []snatchGood
}) {
	if err := json.Unmarshal(snatchJSON, &out); err != nil || len(out.Goods) == 0 {
		panic("game: bad snatch.json")
	}
	return
}()

// snatchGroups are the goods by group (f_id), the groups in order; a round shows one group.
var snatchGroups = func() (out [][]snatchGood) {
	by := map[int64][]snatchGood{}
	var ids []int64
	for _, g := range snatchCfg.Goods {
		if _, ok := by[g.Group]; !ok {
			ids = append(ids, g.Group)
		}
		by[g.Group] = append(by[g.Group], g)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		out = append(out, by[id])
	}
	return
}()

// snatchWindows are the two daily windows in seconds from midnight: [start, end).
var snatchWindows = func() (out [2][2]int64) {
	for i, k := range []string{"snatch_time1", "snatch_time2"} {
		var w [2][2]int64
		if err := json.Unmarshal(snatchCfg.Const[k], &w); err != nil {
			panic("game: bad snatch.json times")
		}
		out[i] = [2]int64{w[0][0]*3600 + w[0][1]*60, w[1][0]*3600 + w[1][1]*60}
	}
	return
}()

type snatchRun struct{ Rid, N int64 } // Rid 0 is the robots that fill the slots left at the end

type snatchItem struct {
	Runs   []snatchRun
	Done   bool
	Win    int64 // the winning slot, from 1
	WinRid int64
}

func (it *snatchItem) sold() (n int64) {
	for _, r := range it.Runs {
		n += r.N
	}
	return
}

func (it *snatchItem) mine(rid int64) (n int64) {
	for _, r := range it.Runs {
		if r.Rid == rid {
			n += r.N
		}
	}
	return
}

// slots are the numbers a role holds: the runs lay the numbers one after the other.
func (it *snatchItem) slots(rid int64) (out []int64) {
	at := int64(0)
	for _, r := range it.Runs {
		for i := int64(1); i <= r.N && r.Rid == rid; i++ {
			out = append(out, at+i)
		}
		at += r.N
	}
	return
}

type snatchLog struct {
	Win, Prize string
	Join, Max  int64
	Time       int64
	Awards     [][2]int64
}

type snatchWorld struct {
	Round int64
	Items map[int64]*snatchItem // by good id
	Logs  []snatchLog
}

type snatchMyLog struct {
	Win    bool
	WinNum int64
	Buy    []int64
	snatchLog
}

type snatchRole struct{ Logs []snatchMyLog }

// snatchRound is the round a time falls in (the day and which window) and whether the window is open; the round of a closed
// time is the next one. start and end are the window's.
func snatchRound(t time.Time) (round int64, open bool, start, end int64) {
	day := dayStart(t)
	since := t.Unix() - day.Unix()
	for i, w := range snatchWindows {
		if since < w[1] {
			return dayKey(t)*2 + int64(i), since >= w[0], day.Unix() + w[0], day.Unix() + w[1]
		}
	}
	next := day.AddDate(0, 0, 1)
	return (dayKey(t)+1)*2 + 0, false, next.Unix() + snatchWindows[0][0], next.Unix() + snatchWindows[0][1]
}

// snatchGoods are the goods of a round.
func snatchGoods(round int64) []snatchGood { return snatchGroups[int(round/2)%len(snatchGroups)] }

func snatchGoodByID(id int64) (snatchGood, bool) {
	for _, g := range snatchCfg.Goods {
		if g.ID == id {
			return g, true
		}
	}
	return snatchGood{}, false
}

// RegisterSnatch registers the holiday snatch: 25700 (the page), 25701 (join with tickets), 25702 (the opening of a good), 25703
// (the server's winners) and 25704 (my records); 26300 / 26301 (the personal gift, which is paid and not open). It runs while the
// operator has the activity with camp_id 993029 open in the schedule, in the two daily windows of the table (12:00-14:00 and
// 18:00-20:00). Every round shows the goods of one group; a ticket (37006) buys a slot, a good draws its winner when its slots are
// all taken or when the window ends. Ours, not captured: the slots left at the window's end are filled by robots (the table has no
// minimum of people beyond the full slots), the winner takes the prize by mail, everyone else gets the score item (37007, 3 per
// ticket) by mail, the round's group goes by the day, and the server is one zone.
func RegisterSnatch(r *gateway.Router, st *store.Store) {
	var score [2]int64
	_ = json.Unmarshal(snatchCfg.Const["item_score"], &score)

	// draw picks the winner of a good, mails the prize and the scores and writes the logs.
	draw := func(srv string, w *snatchWorld, good snatchGood, it *snatchItem, now time.Time) error {
		if it.Done {
			return nil
		}
		if left := good.Limit - it.sold(); left > 0 {
			it.Runs = append(it.Runs, snatchRun{0, left})
		}
		it.Win = int64(summonRand()*float64(good.Limit)) + 1
		at := int64(0)
		for _, run := range it.Runs {
			if it.Win <= at+run.N {
				it.WinRid = run.Rid
				break
			}
			at += run.N
		}
		it.Done = true
		winName := "神秘玩家"
		if it.WinRid != 0 {
			if role, err := st.RoleByID(it.WinRid); err == nil && role != nil {
				winName = role.Name
			}
		}
		lg := snatchLog{Win: winName, Prize: good.Name, Join: good.Limit, Max: good.Limit, Time: now.Unix(), Awards: good.Award}
		w.Logs = append([]snatchLog{lg}, w.Logs...)
		if len(w.Logs) > snatchLogMax {
			w.Logs = w.Logs[:snatchLogMax]
		}
		seen := map[int64]bool{}
		for _, run := range it.Runs {
			if run.Rid == 0 || seen[run.Rid] {
				continue
			}
			seen[run.Rid] = true
			won := run.Rid == it.WinRid
			my := &snatchRole{}
			if _, err := st.LoadState(run.Rid, snatchModule, my); err != nil {
				return err
			}
			ml := snatchMyLog{Win: won, Buy: it.slots(run.Rid), snatchLog: lg}
			if won {
				ml.WinNum = it.Win
			}
			my.Logs = append([]snatchMyLog{ml}, my.Logs...)
			if len(my.Logs) > snatchLogMax {
				my.Logs = my.Logs[:snatchLogMax]
			}
			if err := st.SaveState(run.Rid, snatchModule, my); err != nil {
				return err
			}
			m := store.Mail{SrvID: srv, FromName: "节日夺宝", CreatedBy: "snatch"}
			if won {
				m.Subject = "夺宝中奖"
				m.Content = "恭喜你在节日夺宝中抽中了" + good.Name + "，这是奖励。"
				m.Attachments = attachmentsOf(good.Award)
			} else {
				m.Subject = "夺宝积分"
				m.Content = "你参与的" + good.Name + "已开奖，未中奖，夺宝币按 1:3 返还为夺宝积分。"
				m.Attachments = attachmentsOf([][2]int64{{score[0], score[1] * run.N}})
			}
			if _, err := st.CreateMail(m, []int64{run.Rid}); err != nil {
				return err
			}
		}
		return nil
	}
	// load reads the server's round and settles the one that has gone by.
	load := func(srv string, now time.Time) (*snatchWorld, int64, bool, int64, int64, error) {
		round, open, start, end := snatchRound(now)
		w := &snatchWorld{}
		if _, err := st.LoadGlobal(srv, snatchModule, w); err != nil {
			return nil, 0, false, 0, 0, err
		}
		if w.Items == nil {
			w.Items = map[int64]*snatchItem{}
		}
		if w.Round != round {
			if len(w.Items) > 0 {
				for _, g := range snatchGoods(w.Round) {
					if it := w.Items[g.ID]; it != nil && len(it.Runs) > 0 {
						if err := draw(srv, w, g, it, now); err != nil {
							return nil, 0, false, 0, 0, err
						}
					}
				}
			}
			w.Round, w.Items = round, map[int64]*snatchItem{}
			if err := st.SaveGlobal(srv, snatchModule, w); err != nil {
				return nil, 0, false, 0, 0, err
			}
		}
		return w, round, open, start, end, nil
	}
	logMsgs := func(logs []snatchLog) []proto.Message {
		out := []proto.Message{}
		for _, l := range logs {
			out = append(out, proto.Message{"win_name": l.Win, "join_num": l.Join, "max_num": l.Max, "time": l.Time, "award_name": l.Prize, "awards": awardMsgs(l.Awards)})
		}
		return out
	}
	page := func(s *gateway.Session, role *store.Role) (proto.Message, error) {
		now := s.Now()
		w, round, open, start, end, err := load(role.SrvID, now)
		if err != nil {
			return nil, err
		}
		stateTime, state := start, int64(0)
		if open {
			stateTime, state = end, 1
		}
		list := []proto.Message{}
		if _, _, on := openOrNot(st, role, now); on {
			for i, g := range snatchGoods(round) {
				it := w.Items[g.ID]
				if it == nil {
					it = &snatchItem{}
				}
				istate := int64(0)
				if it.Done {
					istate = 2
				}
				list = append(list, proto.Message{"pos": i + 1, "id": g.ID, "num": it.sold(), "state": istate, "state_time": stateTime,
					"ext": []proto.Message{{"key": 1, "val": it.mine(role.RID)}}})
			}
		}
		return proto.Message{"state": state, "state_time": stateTime, "holiday_snatch_info": list, "logs": logMsgs(w.Logs)}, nil
	}
	r.Handle(25700, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		m, err := page(s, role)
		if err != nil {
			return err
		}
		return s.Send(25700, m)
	})
	r.Handle(25701, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		refuse := func(msg string) error { return s.Send(25701, proto.Message{"code": 0, "msg": msg}) }
		now := s.Now()
		if _, _, on := openOrNot(st, role, now); !on {
			return refuse("活动未开启")
		}
		w, round, open, _, _, err := load(role.SrvID, now)
		if err != nil {
			return err
		}
		if !open {
			return refuse("活动尚未开启")
		}
		goods := snatchGoods(round)
		pos, num := int(req.Int("pos")), req.Int("num")
		if pos < 1 || pos > len(goods) || num < 1 {
			return refuse("参数不对")
		}
		g := goods[pos-1]
		it := w.Items[g.ID]
		if it == nil {
			it = &snatchItem{}
			w.Items[g.ID] = it
		}
		if it.Done || it.sold() >= g.Limit {
			return refuse("人数已满")
		}
		if num > g.Limit-it.sold() || it.mine(role.RID)+num > g.LimitRole {
			return refuse("超过可参与人次")
		}
		spend := map[int64]int64{}
		for _, e := range g.Expend {
			spend[e[0]] += e[1] * num
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, spend, map[int64]int64{}, map[int64]int64{}); err != nil {
			return refuse("夺宝币不足")
		}
		if n := len(it.Runs); n > 0 && it.Runs[n-1].Rid == role.RID {
			it.Runs[n-1].N += num
		} else {
			it.Runs = append(it.Runs, snatchRun{role.RID, num})
		}
		if it.sold() >= g.Limit {
			if err := draw(role.SrvID, w, g, it, now); err != nil {
				return err
			}
		}
		if err := st.SaveGlobal(role.SrvID, snatchModule, w); err != nil {
			return err
		}
		if err := s.Send(25701, proto.Message{"code": 1, "msg": "参与成功"}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		m, err := page(s, role)
		if err != nil {
			return err
		}
		return s.Send(25700, m)
	})
	r.Handle(25702, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		w, round, _, _, _, err := load(role.SrvID, s.Now())
		if err != nil {
			return err
		}
		out := proto.Message{"is_win": 0, "win_num": 0, "rid": 0, "srv_id": "", "win_name": "", "win_face": 0, "win_lev": 0, "id": 0, "buy_nums": []proto.Message{}}
		goods := snatchGoods(round)
		if pos := int(req.Int("pos")); pos >= 1 && pos <= len(goods) {
			g := goods[pos-1]
			out["id"] = g.ID
			if it := w.Items[g.ID]; it != nil && it.Done {
				out["win_num"] = it.Win
				out["is_win"] = 0
				if it.WinRid == role.RID {
					out["is_win"] = 1
				}
				out["win_name"] = "神秘玩家"
				out["win_face"] = store.DefaultFaceID
				out["win_lev"] = 1
				if winner, err := st.RoleByID(it.WinRid); it.WinRid != 0 && err == nil && winner != nil {
					out["rid"], out["srv_id"], out["win_name"], out["win_face"], out["win_lev"] = winner.RID, winner.SrvID, winner.Name, winner.FaceID, winner.Lev
				}
				nums := []proto.Message{}
				for _, n := range it.slots(role.RID) {
					nums = append(nums, proto.Message{"num": n})
				}
				out["buy_nums"] = nums
			}
		}
		return s.Send(25702, out)
	})
	r.Handle(25703, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		w, _, _, _, _, err := load(role.SrvID, s.Now())
		if err != nil {
			return err
		}
		return s.Send(25703, proto.Message{"logs": logMsgs(w.Logs)})
	})
	r.Handle(25704, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if _, _, _, _, _, err := load(role.SrvID, s.Now()); err != nil { // settle a gone round first
			return err
		}
		my := &snatchRole{}
		if _, err := st.LoadState(role.RID, snatchModule, my); err != nil {
			return err
		}
		logs := []proto.Message{}
		for _, l := range my.Logs {
			buy := []proto.Message{}
			for _, n := range l.Buy {
				buy = append(buy, proto.Message{"num": n})
			}
			win := 0
			if l.Win {
				win = 1
			}
			logs = append(logs, proto.Message{"is_win": win, "win_num": l.WinNum, "buy_nums": buy, "time": l.Time, "award_name": l.Prize, "awards": awardMsgs(l.Awards)})
		}
		return s.Send(25704, proto.Message{"logs": logs})
	})
	// the personal gift is paid: the page is empty and nothing activates it
	r.Handle(26300, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(26300, proto.Message{"end_time": 0, "count": 0, "award_id": 0})
	})
	r.Handle(26301, func(s *gateway.Session, _ proto.Message) error {
		return s.Send(26301, proto.Message{"flag": 0})
	})
}

func awardMsgs(l [][2]int64) []proto.Message {
	out := []proto.Message{}
	for _, a := range l {
		out = append(out, proto.Message{"id": a[0], "num": a[1]})
	}
	return out
}

func openOrNot(st *store.Store, role *store.Role, now time.Time) (start, end int64, open bool) {
	return ActivityOpen(st, role.SrvID, snatchCamp, now)
}
