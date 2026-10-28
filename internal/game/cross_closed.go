package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The plays below need what a single server cannot give: the peak champion needs 256 players of the cross-server arena in zones
// (a season "cannot open" with fewer, the client's own rule), the cross champion the same across servers, the god battle a field
// that moves everyone at once, the merge-server voting a merge. They are answered as "not open", so that the windows show that
// instead of waiting, and no state is made.
func closedMsg(msg string) proto.Message {
	return proto.Message{"code": 0, "result": 0, "flag": 0, "msg": msg}
}

var closedReplies = map[uint16]proto.Message{
	// peak champion (27700~27731); a closed season has step_status 0 (unopened), no zone and no lists
	27700: {"start_time": 0, "end_time": 0, "step": 0, "step_status": 0, "step_status_time": 0, "round": 0, "round_status": 0, "round_status_time": 0, "flag": 0, "period": 0, "zone_id": 0, "max_zone_id": 0},
	27701: {"rank": 0, "best_rank": 0, "can_bet": 0, "group_256": 0, "group_64": 0},
	27702: {}, 27703: {}, 27705: {"list": []proto.Message{}}, 27706: {}, 27707: {}, 27708: {"list": []proto.Message{}},
	27709: {"list": []proto.Message{}}, 27710: {"pos_list": []proto.Message{}}, 27711: {}, 27712: {}, 27713: {"arena_peak_rank": []proto.Message{}},
	27714: {"rank": 0, "worship": 0, "power": 0, "score": 0, "rank_list": []proto.Message{}}, 27715: {"time": 0, "rank_list": []proto.Message{}}, 27716: {},
	27720: {"zone_info": []proto.Message{}}, 27730: {"point_info": []proto.Message{}}, 27731: {},
	// cross champion: 26200~26215 are answered by RegisterCrossChampion (arenachampion.go); only the empty 26216 stays
	26216: {},
	// god battle
	21410: {"type": 0, "role_list": []proto.Message{}},
	// cross show, the list of the servers that take part
	22150: {"start_time": 0, "end_time": 0, "srv_list": []proto.Message{}},
	// merge-server voting
	10990: {"flag": 0, "cmd": 0, "img": ""},
	10991: {"status": 0, "is_vote": 0, "flag": 0, "last_time": 0},
	24950: {}, 10342: {},
	// plays of the client whose rules are not in the client package (see OPEN_QUESTIONS): the window opens empty or closed
	11041: {"like": 0, "like_num": 0, "partner_comments": []proto.Message{}},
	11070: {"partner_bid": 0, "partner_score": []proto.Message{}, "stronger_partner_score": []proto.Message{}},
	11330: {"quality": 0, "luck": 0, "end_count": 0, "ref_count": 0, "day_gold_count": 0, "flag": 0},
	13601: {"flag": 0, "msg": "活动未开启", "period": 0, "charge": 0, "cur_day": 0, "num": 0, "end_time": 0, "welfare_list": []proto.Message{}, "grow_list": []proto.Message{}},
	16630: {"code": 1, "items": []proto.Message{}}, // the download reward is told as taken: the icon goes
	16653: {"is_buy": 0, "end_time": 0, "charge_id": 0, "award_list": []proto.Message{}},
	23507: {"free_ids": []proto.Message{}, "cells": []proto.Message{}}, 23508: {"item_base_id": 0, "price": 0}, 23509: {"refresh_time": 0, "data": []proto.Message{}},
	24810: {"quest_list": []proto.Message{}, "camp_id": 0}, 24813: {"quest_list": []proto.Message{}, "camp_id": 0},
	24815: {"quest_list": []proto.Message{}, "camp_id": 0}, 24817: {"quest_list": []proto.Message{}, "camp_id": 0},
	25500: {"period": 0, "day_time": 0, "week_time": 0, "month_time": 0, "list": []proto.Message{}},
	26521: {"camp_id": 0, "free_time": 0, "times": 0, "start_time": 0, "end_time": 0, "item_id": 0, "item_num": 0, "must_count": 0, "reward_list": []proto.Message{}, "award_list": []proto.Message{}},
	28000: {"gifts": []proto.Message{}}, 28100: {"start_time": 0, "end_time": 0, "rsidue_gold": 0}, 28900: {"gift_list": []proto.Message{}},
	10989: {"id": 0, "start_unixtime": 0}, 16666: {},
}

// closedRefusals are the requests that ask for something the closed play could give: they are told why not.
var closedRefusals = map[uint16]proto.Message{
	27704: closedMsg("巅峰冠军赛未开启"),
	21401: closedMsg("战场未开启"), 21403: {"code": 0}, 21421: closedMsg("战场未开启"), 21427: closedMsg("战场未开启"),
	10992: closedMsg("没有进行中的合服投票"), 10993: closedMsg("没有进行中的合服投票"),
	20204: closedMsg("暂不能查看"),
	11042: closedMsg("图鉴评论暂未开放"), 11043: closedMsg("图鉴评论暂未开放"), 11044: closedMsg("图鉴评论暂未开放"), 11047: closedMsg("图鉴暂未开放"),
	11071: closedMsg("限时重生暂未开放"), 11072: closedMsg("限时重生暂未开放"), 11074: closedMsg("限时重生暂未开放"),
	11331: closedMsg("占卜暂未开放"), 11332: closedMsg("占卜暂未开放"), 11333: closedMsg("暂时不能分享录像"), 13602: closedMsg("活动未开启"),
	16654: closedMsg("活动未开启"), 16665: closedMsg("活动未开启"), 16695: closedMsg("活动未开启"),
	21009: closedMsg("暂未开放"), 16631: closedMsg("没有可领取的下载奖励"), 10971: closedMsg("暂未开放"), 26809: closedMsg("暂未开放"),
	23504: closedMsg("交易行暂未开放"), 23505: closedMsg("交易行暂未开放"), 23506: closedMsg("交易行暂未开放"), 23511: closedMsg("交易行暂未开放"),
	23512: closedMsg("交易行暂未开放"), 23513: closedMsg("交易行暂未开放"), 23514: closedMsg("交易行暂未开放"),
	24812: closedMsg("活动未开启"), 24814: closedMsg("活动未开启"), 24816: closedMsg("活动未开启"), 24818: closedMsg("活动未开启"),
	26522: closedMsg("活动未开启"), 26523: closedMsg("活动未开启"), 26530: closedMsg("暂未开放"), 27916: closedMsg("活动未开启"),
	28101: closedMsg("活动未开启"), 28102: closedMsg("活动未开启"), 28103: closedMsg("活动未开启"), 23203: {"code": 0, "msg": "暂未开放", "is_share": 0, "is_day_share": 0},
}

type peakFormations struct {
	Items []struct {
		Order, Type, Hallows int64
		Pos                  [][2]int64 // place and hero
	}
}

// RegisterCrossClosed registers the answers of the plays that cannot be open here, and the saving of the peak champion's line-ups
// (27725, 27726), which needs nothing but the role's own state.
func RegisterCrossClosed(r *gateway.Router, st *store.Store) {
	for _, set := range []map[uint16]proto.Message{closedReplies, closedRefusals} {
		for cmd, m := range set {
			cmd, m := cmd, m
			r.Handle(cmd, func(s *gateway.Session, _ proto.Message) error {
				cp := proto.Message{}
				for k, v := range m {
					cp[k] = v
				}
				return s.Send(cmd, cp)
			})
		}
	}
	r.Handle(27725, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d := &peakFormations{}
		for _, f := range req.Arr("formations") {
			it := struct {
				Order, Type, Hallows int64
				Pos                  [][2]int64
			}{Order: f.Int("order"), Type: f.Int("formation_type"), Hallows: f.Int("hallows_id")}
			for _, p := range f.Arr("pos_info") {
				it.Pos = append(it.Pos, [2]int64{p.Int("pos"), p.Int("id")})
			}
			d.Items = append(d.Items, it)
		}
		if err := st.SaveState(role.RID, "peakformations", d); err != nil {
			return err
		}
		return s.Send(27725, proto.Message{"code": 1, "msg": ""})
	})
	r.Handle(27726, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		d := &peakFormations{}
		if _, err := st.LoadState(role.RID, "peakformations", d); err != nil {
			return err
		}
		list := []proto.Message{}
		for _, it := range d.Items {
			pos := []proto.Message{}
			for _, p := range it.Pos {
				pos = append(pos, proto.Message{"pos": p[0], "id": p[1]})
			}
			list = append(list, proto.Message{"order": it.Order, "formation_type": it.Type, "pos_info": pos, "hallows_id": it.Hallows})
		}
		return s.Send(27726, proto.Message{"formations": list})
	})
}
