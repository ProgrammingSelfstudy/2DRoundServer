package game

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// klbLabel is the asset label of "klb" (10303); the ledger holds it like any currency.
const klbLabel = 41

// worshipKey is the counter of one worship of a role by a role on a day, per kind.
func worshipKey(day, kind, rid int64) string {
	return worshipPerDay + strconv.FormatInt(day, 10) + "_" + strconv.FormatInt(kind, 10) + "_" + strconv.FormatInt(rid, 10)
}

const (
	worshipPerDay  = "worship_"       // counters key prefix: one worship of a role per day and kind
	worshipGot     = "worship_got"    // how many times the role was worshipped
	worshipHome    = 5                // WorshipType.home: the like of a home
	redSeen        = "red_seen_"      // counters key prefix: red points the role has looked at
	reportMsgMax   = 200              // characters of a report text (our policy)
	redeemAttempts = 10               // wrong codes a role may type per day (our policy)
	redeemFailKey  = "redeem_failed_" // counters key prefix, per day
)

// RegisterRoleMisc registers the small role commands: 10303 klb, 10380 register / open time, 10905
// world level, 10316 worship (10317 / 10319 how often the role was worshipped), 10395 age
// check-in (always passed), 10345-10347 role models, 10945 redeem code, 10947 recharge (refused:
// there is no recharge), 10960 / 10961 real-name reward (shown as taken: no real-name system),
// 10985 / 10986 red points seen, 12770 / 12771 reports.
func RegisterRoleMisc(r *gateway.Router, st *store.Store) {
	registerRoleExtra(r)
	r.Handle(10303, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		ledger, err := st.RoleAssets(role.RID)
		if err != nil {
			return err
		}
		return s.Send(10303, proto.Message{"klb": ledger[klbLabel]})
	})
	r.Handle(10380, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		srv, err := st.Server(role.SrvID)
		if err != nil {
			return err
		}
		open := srv.OpenTime
		if open == 0 {
			open = srv.CreatedAt
		}
		return s.Send(10380, proto.Message{"reg_time": role.CreatedAt, "open_time": open})
	})
	r.Handle(10905, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		lev, err := st.WorldLevel(role.SrvID)
		if err != nil {
			return err
		}
		return s.Send(10905, proto.Message{"world_lev": lev})
	})
	r.Handle(10395, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(10395, proto.Message{"status": 1, "end_time": 0, "down_sec": 0})
	})

	// worship: once a day for each role and kind (our policy); the target counts how often it was worshipped
	r.Handle(10316, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		kind, rid, srv, idx := req.Int("type"), req.Int("rid"), req.Str("srv_id"), req.Int("idx")
		reply := func(code int, msg string) error {
			return s.Send(10316, proto.Message{"code": code, "msg": msg, "type": kind, "rid": rid, "srv_id": srv, "idx": idx})
		}
		target, err := st.RoleByID(rid)
		if errors.Is(err, store.ErrNotFound) || (err == nil && (target.SrvID != srv || target.Status != 1)) {
			return reply(0, "玩家不存在")
		}
		if err != nil {
			return err
		}
		key := worshipKey(dayKey(s.Now()), kind, rid)
		if n, err := st.Counter(role.RID, key); err != nil {
			return err
		} else if n > 0 {
			return reply(0, "今天已经膜拜过了")
		}
		if kind == worshipHome { // a like of a home has its own limit and rewards (homeworld.go)
			if msg, err := homeLike(st, s, role, target); err != nil {
				return err
			} else if msg != "" {
				return reply(0, msg)
			}
		}
		if _, err := st.AddCounter(role.RID, key, 1); err != nil {
			return err
		}
		if _, err := st.AddCounter(rid, worshipGot, 1); err != nil {
			return err
		}
		if kind == worshipCrossArena { // the honour list of the cross-server arena pays for it
			ga, gi := map[int64]int64{}, map[int64]int64{}
			like := caCfg.pair("like_reward")
			splitGains([]sellGain{{like[0], like[1]}}, 1, ga, gi)
			if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
				return err
			}
			if err := reply(1, "膜拜成功"); err != nil {
				return err
			}
			return sendAssets(s, st, role.RID)
		}
		return reply(1, "膜拜成功")
	})
	r.Handle(10317, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		n, err := st.Counter(role.RID, worshipGot)
		if err != nil {
			return err
		}
		return s.Send(10317, proto.Message{"worship": n})
	})
	r.Handle(10319, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(10319, proto.Message{"worship": 0}) // the cross-server champion's worship: there is no cross-server play
	})

	// role models: owning the hero at the model's star (or being at its level) opens it; skin models need the skin
	modelOpen := func(s *gateway.Session, id int64) (bool, error) {
		row, ok := looksModels[id]
		if !ok {
			return false, nil
		}
		role := s.Role()
		if row.Skin != 0 {
			skins, err := st.SkinsOf(role.RID, s.Now().Unix())
			if err != nil {
				return false, err
			}
			for _, k := range skins {
				if k.ID == row.Skin {
					return true, nil
				}
			}
			return false, nil
		}
		ps, err := st.PartnersOf(role.RID)
		if err != nil {
			return false, err
		}
		best := 0
		for _, p := range ps {
			if p.BID == row.Partner && p.Star > best {
				best = p.Star
			}
		}
		fresh, err := st.RoleByID(role.RID)
		if err != nil {
			return false, err
		}
		return best >= row.Star && best > 0 && fresh.Lev >= row.Lev, nil
	}
	currentModel := func(s *gateway.Session) (int64, error) {
		cfg, err := st.SettingsOf(s.Role().RID)
		if err != nil {
			return 0, err
		}
		if cfg.LookID != 0 {
			return cfg.LookID, nil
		}
		fresh, err := st.RoleByID(s.Role().RID)
		if err != nil {
			return 0, err
		}
		return defaultLookID(fresh.FaceID), nil
	}
	r.Handle(10345, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		use, err := currentModel(s)
		if err != nil {
			return err
		}
		list := []proto.Message{}
		for _, id := range sortedModelIDs() {
			if open, err := modelOpen(s, id); err != nil {
				return err
			} else if open {
				list = append(list, proto.Message{"id": id})
			}
		}
		return s.Send(10345, proto.Message{"use_id": use, "list": list})
	})
	r.Handle(10346, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		id := req.Int("id")
		open, err := modelOpen(s, id)
		if err != nil {
			return err
		}
		if !open {
			return s.Send(10346, proto.Message{"code": 0, "msg": "尚未获得该形象", "id": id})
		}
		if err := st.SetLookID(role.RID, id); err != nil {
			return err
		}
		return s.Send(10346, proto.Message{"code": 1, "msg": "", "id": id})
	})
	r.Handle(10347, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		id := req.Int("id")
		open, err := modelOpen(s, id)
		if err != nil {
			return err
		}
		if !open {
			return s.Send(10347, proto.Message{"code": 0, "msg": "未满足激活条件", "id": id})
		}
		return s.Send(10347, proto.Message{"code": 1, "msg": "", "id": id}) // every model of the client's table is free
	})

	// redeem codes: ten wrong ones a day, then the role waits for tomorrow
	r.Handle(10945, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(code int, msg string) error { return s.Send(10945, proto.Message{"code": code, "msg": msg}) }
		failKey := redeemFailKey + strconv.FormatInt(dayKey(s.Now()), 10)
		if n, err := st.Counter(role.RID, failKey); err != nil {
			return err
		} else if n >= redeemAttempts {
			return reply(0, "今日输入错误次数过多")
		}
		got, err := st.Redeem(role.RID, req.Str("card_id"), s.Now().Unix())
		switch {
		case err == nil:
		case errors.Is(err, store.ErrCodeUnknown):
			st.AddCounter(role.RID, failKey, 1)
			return reply(0, "兑换码无效")
		case errors.Is(err, store.ErrCodeExpired):
			return reply(0, "兑换码已过期")
		case errors.Is(err, store.ErrCodeUsedUp):
			return reply(0, "兑换码已被领完")
		case errors.Is(err, store.ErrCodeTaken):
			return reply(0, "你已经使用过该兑换码")
		default:
			return err
		}
		_ = got
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		if err := sendBackpack(s, st); err != nil {
			return err
		}
		return reply(1, "兑换成功")
	})
	r.Handle(10947, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(10947, proto.Message{"code": 0, "url": ""}) // there is no recharge
	})
	r.Handle(10960, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(10960, proto.Message{"code": 1}) // "已领取": there is no real-name system, nothing to claim
	})
	r.Handle(10961, func(s *gateway.Session, _ proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(10961, proto.Message{"code": 0, "msg": "暂未开放实名认证"})
	})

	// red points the role has looked at
	r.Handle(10985, func(s *gateway.Session, _ proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		list := []proto.Message{}
		for id := int64(1); id <= 10; id++ { // RoleConst.red_point_1 .. red_point_10
			if n, err := st.Counter(role.RID, redSeen+strconv.FormatInt(id, 10)); err != nil {
				return err
			} else if n > 0 {
				list = append(list, proto.Message{"id": id})
			}
		}
		return s.Send(10985, proto.Message{"id_list": list})
	})
	r.Handle(10986, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		if id := req.Int("id"); id >= 1 && id <= 10 {
			key := redSeen + strconv.FormatInt(id, 10)
			if n, err := st.Counter(role.RID, key); err != nil {
				return err
			} else if n == 0 {
				_, err = st.AddCounter(role.RID, key, 1)
				return err
			}
		}
		return nil
	})

	// reports: kept for the admin page; the chat history the client attaches is not stored (it only sends line ids)
	r.Handle(12770, func(s *gateway.Session, req proto.Message) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		reply := func(code int, msg string) error { return s.Send(12770, proto.Message{"code": code, "msg": msg}) }
		text := strings.TrimSpace(req.Str("msg"))
		if utf8.RuneCountInString(text) > reportMsgMax {
			return reply(0, "举报内容过长")
		}
		target, err := st.RoleByID(req.Int("rid"))
		if errors.Is(err, store.ErrNotFound) || (err == nil && target.RID == role.RID) {
			return reply(0, "举报对象不存在")
		}
		if err != nil {
			return err
		}
		if err := st.AddReport(store.Report{RID: role.RID, TargetRID: target.RID, TargetSrvID: target.SrvID, Kind: int(req.Int("type")), Msg: text}); err != nil {
			return err
		}
		return reply(1, "举报成功，我们会尽快处理")
	})
	r.Handle(12771, func(s *gateway.Session, req proto.Message) error {
		if s.Role() == nil {
			return nil
		}
		return s.Send(12771, proto.Message{"rid": req.Int("rid"), "srv_id": req.Str("srv_id"), "history": []proto.Message{}})
	})
}
