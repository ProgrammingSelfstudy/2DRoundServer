package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// holidayconvert.json is the client's hero conversion table (tools/extract_holidayconvert.py): per star and
// camp what a hero of 7 stars or more costs to turn into another hero of its camp, and the hero groups.
//
//go:embed data/holidayconvert.json
var holidayConvertJSON []byte

type convertRow struct {
	Star        int        `json:"star"`
	Camp        int        `json:"camp"`
	Expend      int        `json:"expend"`
	SrcGroup    int64      `json:"src_group"`
	TargetGroup int64      `json:"target_group"`
	Loss        [][2]int64 `json:"loss"`
}

var holidayConvert = func() (c struct {
	Rows   []convertRow
	Src    map[string][]int64
	Target map[string][]int64
}) {
	var f struct {
		Rows   []convertRow       `json:"rows"`
		Src    map[string][]int64 `json:"src"`
		Target map[string][]int64 `json:"target"`
	}
	if err := json.Unmarshal(holidayConvertJSON, &f); err != nil || len(f.Rows) == 0 {
		panic("game: bad holidayconvert.json")
	}
	c.Rows, c.Src, c.Target = f.Rows, f.Src, f.Target
	return
}()

func convertRowOf(star, camp int) (convertRow, bool) {
	for _, r := range holidayConvert.Rows {
		if r.Star == star && r.Camp == camp {
			return r, true
		}
	}
	return convertRow{}, false
}

// RegisterHolidayConvert registers the hero conversion (16686): a hero of 7 stars or more turns into another hero
// of its camp, given the number the table names of 5 star heroes of the target hero and the cost. The new hero
// keeps the level, star, break step, equipment and formation places; the consumed heroes give back what they
// were raised with and their equipment (the soul tool / horcrux is not kept, the client says so).
func RegisterHolidayConvert(r *gateway.Router, st *store.Store) {
	r.Handle(16686, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(code int, msg string) error { return s.Send(16686, proto.Message{"code": code, "msg": msg}) }
		src, err := st.PartnerByID(role.RID, req.Int("partner_id"))
		if errors.Is(err, store.ErrNotFound) {
			return reply(0, "英雄不存在")
		}
		if err != nil {
			return err
		}
		target := req.Int("partner_bid")
		row, ok := convertRowOf(src.Star, partnerTable[src.BID].Camp)
		switch {
		case !ok || !slices.Contains(holidayConvert.Src[strconv.FormatInt(row.SrcGroup, 10)], src.BID):
			return reply(0, "这个英雄不能置换")
		case target == src.BID || !slices.Contains(holidayConvert.Target[strconv.FormatInt(row.TargetGroup, 10)], target):
			return reply(0, "目标英雄不对")
		case src.ResSlot > 0:
			return reply(0, "共鸣中的英雄不能置换")
		}
		all, err := st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		owned := map[int64]store.Partner{}
		for _, h := range all {
			owned[h.PartnerID] = h
		}
		var consumed []int64
		assets, items := map[int64]int64{}, map[int64]int64{}
		for _, a := range req.Arr("expend") {
			id := a.Int("partner_id")
			h, has := owned[id]
			switch {
			case !has || slices.Contains(consumed, id) || id == src.PartnerID:
				return reply(0, "材料英雄不对")
			case h.BID != target || h.Star != 5:
				return reply(0, "材料英雄要是目标英雄的 5 星")
			case h.Locked || h.ResSlot > 0:
				return reply(0, "材料英雄已锁定或在共鸣中")
			}
			consumed = append(consumed, id)
			ra, ri := refund(h)
			for _, e := range h.Equips { // the store gives the equipment back itself
				ri[e.BaseID]--
			}
			for id, n := range ra {
				assets[id] += n
			}
			for id, n := range ri {
				if n > 0 {
					items[id] += n
				}
			}
		}
		if len(consumed) != row.Expend {
			return reply(0, "材料数量不对")
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		for _, l := range row.Loss {
			splitGains([]sellGain{{l[0], l[1]}}, 1, spendA, spendI)
		}
		np, err := st.ConvertPartner(role.RID, src.PartnerID, target, consumed, spendA, spendI, assets, items)
		if errors.Is(err, store.ErrConflict) {
			return reply(0, "时光之力不足")
		}
		if err != nil {
			return err
		}
		if err := reply(1, "置换成功"); err != nil {
			return err
		}
		gone := []proto.Message{{"partner_id": src.PartnerID}}
		for _, id := range consumed {
			gone = append(gone, proto.Message{"partner_id": id})
		}
		if err := s.Send(11006, proto.Message{"expend2": gone}); err != nil {
			return err
		}
		if err := s.Send(11001, proto.Message{"partners": []proto.Message{partnerMessage(*np)}}); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
