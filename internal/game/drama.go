package game

import (
	_ "embed"
	"encoding/json"
	"errors"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// drama_dungeons.json is the main-line dungeon chain of the restored client
// (tools/extract_drama_dungeons.py): one mode, 16 chapters, 640 dungeons.
//
//go:embed data/drama_dungeons.json
var dramaJSON []byte

type dungeon struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Mode    int    `json:"mode"`
	Chapter int    `json:"chapter"`
	Next    int64  `json:"next"`
}

var dramaChain = func() (f struct {
	First    int64     `json:"first"`
	Dungeons []dungeon `json:"dungeons"`
}) {
	if err := json.Unmarshal(dramaJSON, &f); err != nil || len(f.Dungeons) == 0 {
		panic("game: bad drama_dungeons.json")
	}
	return f
}()

// Status of a dungeon that has not been cleared and can be challenged. Seen on a
// device: BttleTopDramaView.updateNextBtnStatus shows the challenge button only for
// status 2 with cool_time 0; status 1 is the cooling-down state (it needs a
// cool_time, otherwise no button at all), 3 means passed ("前往下一章").
const dungeonNotCleared = 2

// legacyNotCleared is what earlier versions stored for the same state; it is
// read as dungeonNotCleared.
const legacyNotCleared = 1

// dramaState returns the stored progress of a role, or the start of the campaign.
func dramaState(st *store.Store, rid int64) (store.Drama, error) {
	d, err := st.Drama(rid)
	if err == nil {
		if d.Status == legacyNotCleared {
			d.Status = dungeonNotCleared
		}
		return *d, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Drama{}, err
	}
	first := dramaChain.Dungeons[0]
	return store.Drama{RID: rid, Mode: first.Mode, ChapterID: first.Chapter, DunID: first.ID,
		Status: dungeonNotCleared, MaxDunID: first.ID, IsFirst: true}, nil
}

// pushDrama sends 13000, the campaign state. Nobody requests it: the client has
// no sender for it, so the server pushes it after login. The client builds the
// main-city buildings from max_dun_id (main_scene_controller.lua createBuildVo
// returns early while there is no drama data).
func pushDrama(s *gateway.Session, st *store.Store, rid int64) error {
	d, err := dramaState(st, rid)
	if err != nil {
		return err
	}
	dd, err := st.DramaDaily(rid, dayKey(s.Now()))
	if err != nil {
		return err
	}
	return s.Send(13000, proto.Message{
		"mode": d.Mode, "chapter_id": d.ChapterID, "dun_id": d.DunID, "status": d.Status,
		"is_first": b2i(d.IsFirst), "cool_time": 0, "max_dun_id": d.MaxDunID,
		// auto_num is today's sweeps used out of auto_num_max (the original sends
		// 10 for the latter, capture original-2026-10-20); the client decides from it
		// whether a sweep is still free and whether the button is still open
		"auto_num": dd.Sweep, "auto_num_max": autoNumMax,
		"mode_list": dramaModeList(d),
	})
}

// dramaModeList is the per-dungeon state of the current chapter, as the original
// server sends it in 13000: the dungeons of the chapter up to the current one,
// newest first, 3 = passed and the current one with its own status. Chapter
// status is 0 in the capture.
func dramaModeList(d store.Drama) []proto.Message {
	cur, ok := dungeonIndex[d.DunID]
	if !ok {
		return []proto.Message{}
	}
	var duns []proto.Message
	for i := cur; i >= 0 && dramaChain.Dungeons[i].Chapter == d.ChapterID && dramaChain.Dungeons[i].Mode == d.Mode; i-- {
		status := int64(dungeonCleared)
		if i == cur {
			status = int64(d.Status)
		}
		duns = append(duns, proto.Message{"dun_id": dramaChain.Dungeons[i].ID, "status": status, "cool_time": 0, "auto_num": 0})
	}
	return []proto.Message{{
		"mode":         d.Mode,
		"chapter_list": []proto.Message{{"chapter_id": d.ChapterID, "status": 0, "dun_list": duns}},
	}}
}
