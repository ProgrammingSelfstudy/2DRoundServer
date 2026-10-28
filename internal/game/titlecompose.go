package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// titlecompose.json: the title compositions of the client (tools/extract_titlecompose.py).
//
//go:embed data/titlecompose.json
var titleComposeJSON []byte

type titleStep struct {
	Material int64     `json:"material"`
	Coin     [][]int64 `json:"coin"`
	Rate     int64     `json:"rate"` // per mille
	Award    [][]int64 `json:"award"`
}

var titleCompose = func() struct {
	Compound map[int64]map[int64]map[int64]titleStep `json:"compound"`
	MaxLev   map[int64]int64                         `json:"max_lev"`
} {
	var t struct {
		Compound map[int64]map[int64]map[int64]titleStep `json:"compound"`
		MaxLev   map[int64]int64                         `json:"max_lev"`
	}
	if err := json.Unmarshal(titleComposeJSON, &t); err != nil || len(t.Compound) != 44 {
		panic("game: bad titlecompose.json: " + fmt.Sprint(err))
	}
	return t
}()

const titleLevelModule = "titlelev"

type titleLevelDoc struct{ Lev map[string]int64 }

// titleLevels reads the composed level of each title the role has, by title id.
func titleLevels(st *store.Store, rid int64) (map[int64]int64, error) {
	d := &titleLevelDoc{}
	if _, err := st.LoadState(rid, titleLevelModule, d); err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	for k, v := range d.Lev {
		id, _ := strconv.ParseInt(k, 10, 64)
		out[id] = v
	}
	return out, nil
}

// RegisterTitleCompose registers 23304, the composition of a title: 1~5 pieces of the title's material and coin give a chance
// (the table's `rate`) to raise the title one level; a title the role does not have yet is given at level 1 by the first success. A
// failure pays back the table's `award` (currency 40). The title attributes (`attr`) are not added to the role.
func RegisterTitleCompose(r *gateway.Router, st *store.Store) {
	r.Handle(23304, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		no := func(msg string) error { return s.Send(23304, proto.Message{"result": 0, "msg": msg, "flag": 0}) }
		id, n := req.Int("item_id"), req.Int("item_num")
		levs, err := titleLevels(st, role.RID)
		if err != nil {
			return err
		}
		lev := levs[id]
		if _, ok := titleCompose.Compound[id]; !ok {
			return no("该称号不能合成")
		}
		if lev >= titleCompose.MaxLev[id] {
			return no("称号已达最高等级")
		}
		step, ok := titleCompose.Compound[id][lev][n]
		if !ok {
			return no("合成数量不对")
		}
		spendA, spendI := splitCost(step.Coin, 1)
		spendI[step.Material] += n
		win := rand.Int63n(1000) < step.Rate
		gainA, gainI := map[int64]int64{}, map[int64]int64{}
		if !win {
			gainA, gainI = splitCost(step.Award, 1)
		}
		if err := st.Exchange(role.RID, spendA, spendI, gainA, gainI); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return no("合成材料不足")
			}
			return err
		}
		flag, msg := 0, "合成失败"
		if win {
			d := &titleLevelDoc{Lev: map[string]int64{}}
			if _, err := st.LoadState(role.RID, titleLevelModule, d); err != nil {
				return err
			}
			if d.Lev == nil {
				d.Lev = map[string]int64{}
			}
			d.Lev[strconv.FormatInt(id, 10)] = lev + 1
			if err := st.SaveState(role.RID, titleLevelModule, d); err != nil {
				return err
			}
			if lev == 0 {
				if err := st.EnsureLook(role.RID, store.LookTitle, id); err != nil {
					return err
				}
			}
			flag, msg = 1, "合成成功"
		}
		if err := s.Send(23304, proto.Message{"result": 1, "msg": msg, "flag": flag}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
