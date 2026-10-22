package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// drama_milestones.json: the campaign milestone rewards of the client
// (tools/extract_drama_milestones.py).
//
//go:embed data/drama_milestones.json
var milestonesJSON []byte

type milestone struct {
	Limit int64      `json:"limit"` // reached when the cleared max_dun_id >= Limit (the client's own test)
	Items [][2]int64 `json:"items"`
}

var milestones = func() map[int64]milestone {
	var f struct {
		Rewards map[int64]milestone `json:"rewards"`
	}
	if err := json.Unmarshal(milestonesJSON, &f); err != nil || len(f.Rewards) == 0 {
		panic("game: bad drama_milestones.json")
	}
	return f.Rewards
}()

// The client's table lists four things for reward 1 (4,000,000,000 gems among
// them), but the original server paid only 200,000,000 captain exp (asset 5): the
// claim in capture original-2026-10-22 sent 12742 with that one asset, the diamond
// count did not move, and the client's own window shows only the EXP icon. So
// reward 1 is that one asset here; the rest of the table is paid as listed.
func init() {
	if m, ok := milestones[1]; ok {
		m.Items = [][2]int64{{5, 200_000_000}}
		milestones[1] = m
	}
}

// maxMilestoneAmount is a sanity limit (our policy) for what one entry may pay.
// It is above the 200,000,000 captain exp of reward 1, below the client's other
// test values (4,000,000,000), which are refused.
const maxMilestoneAmount = 200_000_000

// RegisterDramaMilestones registers 13008 (the milestone rewards already taken)
// and 13009 (take one, once the dungeon it needs has been reached).
func RegisterDramaMilestones(r *gateway.Router, st *store.Store) {
	r.Handle(13008, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ids, err := st.DramaMilestonesTaken(role.RID)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, proto.Message{"id": id})
		}
		return s.Send(13008, proto.Message{"list": list})
	})
	r.Handle(13009, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("id")
		reply := func(code int, msg string) error {
			return s.Send(13009, proto.Message{"code": code, "msg": msg, "id": id})
		}
		m, ok := milestones[id]
		if !ok {
			return reply(0, "奖励不存在")
		}
		d, err := dramaState(st, role.RID)
		if err != nil {
			return err
		}
		if clearedMax(d) < m.Limit { // the client's own test on the max_dun_id it was told
			return reply(0, "尚未通关指定关卡")
		}
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, g := range m.Items {
			if g[1] > maxMilestoneAmount {
				return reply(0, "奖励配置异常")
			}
			splitGains([]sellGain{{g[0], g[1]}}, 1, assets, items)
		}
		if err := st.TakeDramaMilestone(role.RID, id, assets, items); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return reply(0, "奖励已领取")
			}
			return err
		}
		// the original tells the gain with 12742 (the client's "恭喜获得" window) and
		// answers 13009 with an empty msg (capture original-2026-10-22)
		if err := sendAssetGain(s, assets); err != nil {
			return err
		}
		if err := reply(1, ""); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}

// sendAssetGain sends 12742, the client's "you got these" window for assets
// (mainui_controller handle12742). num is a string on the wire; source 0, is_bind
// 1, id 0 and move_to 0 are what the original sent. Nothing is sent for no assets.
func sendAssetGain(s *gateway.Session, assets map[int64]int64) error {
	if len(assets) == 0 {
		return nil
	}
	list := make([]proto.Message, 0, len(assets))
	for _, k := range mergedKeys(assets, nil) {
		list = append(list, proto.Message{"bid": k, "is_bind": 1, "num": strconv.FormatInt(assets[k], 10), "id": 0, "move_to": 0})
	}
	return s.Send(12742, proto.Message{"asset_list": list, "source": 0})
}
