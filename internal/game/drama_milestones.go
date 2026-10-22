package game

import (
	_ "embed"
	"encoding/json"
	"errors"

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
	Limit int64      `json:"limit"` // reached when max_dun_id >= Limit (the client's own test)
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

// maxMilestoneAmount is a sanity limit (our policy): the client's reward 1 lists
// 4,000,000,000 gems, which can only be a test value, and is refused.
const maxMilestoneAmount = 100_000_000

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
		if d.MaxDunID < m.Limit {
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
		if err := reply(1, "领取成功"); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
