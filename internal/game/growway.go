package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	growModule = "growway"
	growMax    = 1000 // entries kept; older ones are dropped
)

// growEntry is one line of the "成长之路" of the personal space: the event (the id of Config.RoomGrowData.data_growth_way_desc, which holds
// the sentence with %s for the args), its number in the role's own count, the time and up to four args.
type growEntry struct {
	ID, Order, Time int64
	Args            []string
}

type growDoc struct {
	Count   int64 // entries ever recorded; the next order is Count+1
	Entries []growEntry
}

// RecordGrowth adds a line to a role's growth way. The sentences are the client's; `args` fill the %s of the sentence in order.
// An unknown event id is still stored (the client shows "无记录").
func RecordGrowth(st *store.Store, rid, id, now int64, args ...string) error {
	d := &growDoc{}
	if _, err := st.LoadState(rid, growModule, d); err != nil {
		return err
	}
	d.Count++
	d.Entries = append(d.Entries, growEntry{ID: id, Order: d.Count, Time: now, Args: args})
	if len(d.Entries) > growMax {
		d.Entries = d.Entries[len(d.Entries)-growMax:]
	}
	return st.SaveState(rid, growModule, d)
}

// growHas tells whether the role already has an entry of the event, for the "first time" lines.
func growHas(st *store.Store, rid, id int64) (bool, error) {
	d := &growDoc{}
	if _, err := st.LoadState(rid, growModule, d); err != nil {
		return false, err
	}
	for _, e := range d.Entries {
		if e.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// growPage is the answer of 25830 / 25832: start 0 asks for the newest `num` lines, otherwise the lines whose order is in
// [start, start+num).
func growPage(d *growDoc, start, num int64) []proto.Message {
	list := append([]growEntry(nil), d.Entries...)
	sort.Slice(list, func(i, j int) bool { return list[i].Order > list[j].Order }) // newest first, like the original (capture original-2026-10-27)
	out := []proto.Message{}
	for _, e := range list {
		if start == 0 {
			if e.Order <= d.Count-num {
				continue
			}
		} else if e.Order < start || e.Order >= start+num {
			continue
		}
		args := []proto.Message{}
		for i, a := range e.Args {
			args = append(args, proto.Message{"pos": i + 1, "val": a})
		}
		out = append(out, proto.Message{"id": e.ID, "order": e.Order, "time": e.Time, "arge": args})
	}
	return out
}

// registerGrowWay registers 25830 (the role's own growth way) and 25832 (another role's, by id).
func registerGrowWay(r *gateway.Router, st *store.Store) {
	r.Handle(25830, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d := &growDoc{}
		if _, err := st.LoadState(role.RID, growModule, d); err != nil {
			return err
		}
		return s.Send(25830, proto.Message{"start": req.Int("start"), "num": req.Int("num"), "max_num": d.Count,
			"progress": growPage(d, req.Int("start"), req.Int("num"))})
	})
	r.Handle(25832, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		d := &growDoc{}
		if _, err := st.LoadState(req.Int("rid"), growModule, d); err != nil {
			return err
		}
		return s.Send(25832, proto.Message{"rid": req.Int("rid"), "srv_id": req.Str("srv_id"), "start": req.Int("start"), "num": req.Int("num"),
			"max_num": d.Count, "room_grow_info": growPage(d, req.Int("start"), req.Int("num"))})
	})
}
