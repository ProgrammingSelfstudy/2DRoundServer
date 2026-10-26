package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The pass videos of the campaign (13015): of every dungeon the fight of the fastest clear, of the lowest power and of
// the latest. Only the clear that takes a role past a dungeon the first time is kept (the same role clears it once), so
// the replays stay few. The "time" is our own: the fight is a script, so we count its rounds, five seconds each.

const (
	dramaPassModule  = "dramapass"
	dramaPassSeconds = 5 // seconds a round counts for in the "time" of a pass video
	passFastest      = 1
	passLowest       = 2
	passLatest       = 3
)

type passEntry struct {
	RID    int64  `json:"rid"`
	SrvID  string `json:"srv"`
	Name   string `json:"name"`
	Face   int64  `json:"face"`
	Power  int64  `json:"power"`
	Time   int64  `json:"time"`
	Replay int64  `json:"replay"`
}

// passBoard is the three kept entries of a dungeon, by type.
type passBoard map[int64]*passEntry

type passRecord struct {
	Replay, Power, Time int64
}

// dramaPassKeep adds a clear to the board of its dungeon on the role's server.
func dramaPassKeep(st *store.Store, role *store.Role, dun int64, rec passRecord) error {
	all := map[int64]passBoard{}
	if _, err := st.LoadGlobal(role.SrvID, dramaPassModule, &all); err != nil {
		return err
	}
	if all[dun] == nil {
		all[dun] = passBoard{}
	}
	face := int64(role.FaceID)
	if face == 0 {
		face = store.DefaultFaceID
	}
	e := &passEntry{RID: role.RID, SrvID: role.SrvID, Name: role.Name, Face: face, Power: rec.Power, Time: rec.Time, Replay: rec.Replay}
	b := all[dun]
	if b[passFastest] == nil || e.Time < b[passFastest].Time {
		b[passFastest] = e
	}
	if b[passLowest] == nil || e.Power < b[passLowest].Power {
		b[passLowest] = e
	}
	b[passLatest] = e
	return st.SaveGlobal(role.SrvID, dramaPassModule, all)
}

// RegisterDramaPass registers the campaign's pass videos (13015), the progress among all players (13020) and the
// go-to-next-chapter button (13002).
func RegisterDramaPass(r *gateway.Router, st *store.Store) {
	r.Handle(13015, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		all := map[int64]passBoard{}
		if _, err := st.LoadGlobal(role.SrvID, dramaPassModule, &all); err != nil {
			return err
		}
		list := []proto.Message{}
		types := make([]int64, 0, 3)
		for t := range all[req.Int("dun_id")] {
			types = append(types, t)
		}
		sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
		for _, t := range types {
			e := all[req.Int("dun_id")][t]
			list = append(list, proto.Message{"rid": e.RID, "sid": e.SrvID, "name": e.Name, "face_id": e.Face, "power": e.Power, "time": e.Time,
				"repaly_id": e.Replay, "type": t, "face_update_time": 0, "face_file": ""})
		}
		return s.Send(13015, proto.Message{"dungeon_replay_log": list})
	})
	r.Handle(13020, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ranks, total, err := st.DramaProgress(role.SrvID)
		if err != nil {
			return err
		}
		d, err := dramaState(st, role.RID)
		if err != nil {
			return err
		}
		mine := dungeonIndex[d.MaxDunID]
		below := int64(0)
		if mine > 0 { // the roles with no progress stored stand at the first dungeon
			below = total - int64(len(ranks))
		}
		for _, id := range ranks {
			if dungeonIndex[id] < mine {
				below++
			}
		}
		val := int64(0)
		if below < 0 {
			below = 0
		}
		if total > 0 {
			val = below * 100000 / total
		}
		return s.Send(13020, proto.Message{"val": val})
	})
	r.Handle(13002, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		// the chapters follow on their own when a dungeon is cleared, so the button only shows at the end of the chain
		return s.Send(13002, proto.Message{"code": 0, "msg": "已经通关全部章节"})
	})
}
