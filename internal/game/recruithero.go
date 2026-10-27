package game

import (
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

const (
	recruitModule  = "recruithero"
	recruitMaxDays = 3  // max_reg_day: the offer runs for the first three days of a role
	recruitDunMax  = 75 // dun_max_id: dungeons to clear
	recruitRepeat  = 50 // the number of each prize the window shows
)

// recruitFinal is what all three quests together give (data_welfare_const.award).
var recruitFinal = [2]int64{26907, 50}

type recruitState struct {
	Claimed []int64 // the quests taken
	Final   bool
}

// recruitQuests are the three quests of the new-role offer (the client's recruit window, ids as it numbers them): 1 clear 75
// dungeons of the campaign, 2 log in on the second day, 3 activate the month card. Each pays 50 of an item.
func recruitQuests(st *store.Store, role *store.Role, now time.Time) (val [4]int64, done [4]bool, err error) {
	d, err := dramaState(st, role.RID)
	if err != nil {
		return
	}
	id := d.MaxDunID
	if id == 0 {
		id = dramaChain.First
	}
	val[1] = int64(dungeonIndex[id] + 1)
	done[1] = val[1] >= recruitDunMax
	val[2], done[2] = 0, loginDayReached(role, now) >= 2
	mc := &monthCardState{}
	if _, err = st.LoadState(role.RID, monthCardModule, mc); err != nil {
		return
	}
	done[3] = mc.Start[0] != 0
	return
}

func recruitPrize(quest int64) [2]int64 {
	switch quest {
	case 1:
		return [2]int64{29905, recruitRepeat}
	case 2:
		return [2]int64{loginDays[1][0][0], recruitRepeat}
	}
	return [2]int64{26903, recruitRepeat}
}

// RegisterRecruitHero registers the new-role offer: 25100 the window (state, the quests and when it ends), 25101 take a quest's
// prize (id 1~3) or, with id 0, the prize of all three. Our rules, not captured: it runs for the first three days (the client's
// min and max registration days); the two prizes of the second and third quests are those the window shows.
func RegisterRecruitHero(r *gateway.Router, st *store.Store) {
	window := func(role *store.Role, d *recruitState, now time.Time) (proto.Message, error) {
		val, done, err := recruitQuests(st, role, now)
		if err != nil {
			return nil, err
		}
		quests := []proto.Message{}
		all := true
		for q := int64(1); q <= 3; q++ {
			status := int64(0)
			switch {
			case has(d.Claimed, q):
				status = 2
			case done[q]:
				status = 1
			}
			all = all && done[q]
			quests = append(quests, proto.Message{"id": q, "val": val[q], "status": status})
		}
		state := int64(0)
		switch {
		case d.Final:
			state = 2
		case all && len(d.Claimed) == 3:
			state = 1
		}
		end := dayStart(time.Unix(role.CreatedAt, 0).In(dayZone)).AddDate(0, 0, recruitMaxDays).Unix()
		return proto.Message{"state": state, "quests": quests, "end_time": end}, nil
	}
	load := func(s *gateway.Session) (*store.Role, *recruitState, error) {
		role := s.Role()
		if role == nil {
			return nil, nil, nil
		}
		d := &recruitState{}
		_, err := st.LoadState(role.RID, recruitModule, d)
		return role, d, err
	}
	r.Handle(25100, func(s *gateway.Session, _ proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		m, err := window(role, d, s.Now())
		if err != nil {
			return err
		}
		return s.Send(25100, m)
	})
	r.Handle(25101, func(s *gateway.Session, req proto.Message) error {
		role, d, err := load(s)
		if role == nil {
			return err
		}
		refuse := func(msg string) error { return s.Send(25101, proto.Message{"code": 0, "msg": msg}) }
		now := s.Now()
		m, err := window(role, d, now)
		if err != nil {
			return err
		}
		if now.Unix() >= m.Int("end_time") {
			return refuse("活动已结束")
		}
		id := req.Int("id")
		var prize [][2]int64
		switch {
		case id == 0:
			if m.Int("state") != 1 {
				return refuse("任务还没有全部完成")
			}
			prize = [][2]int64{recruitFinal}
			d.Final = true
		case id >= 1 && id <= 3:
			status := m.Arr("quests")[id-1].Int("status")
			if status == 2 {
				return refuse("已经领取过了")
			} else if status == 0 {
				return refuse("任务还没有完成")
			}
			prize = [][2]int64{recruitPrize(id)}
			d.Claimed = append(d.Claimed, id)
		default:
			return refuse("任务不存在")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		for _, it := range prize {
			splitGains([]sellGain{{it[0], it[1]}}, 1, ga, gi)
		}
		if err := st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi); err != nil {
			return err
		}
		if err := st.SaveState(role.RID, recruitModule, d); err != nil {
			return err
		}
		if err := s.Send(25101, proto.Message{"code": 1, "msg": "领取成功"}); err != nil {
			return err
		}
		if m, err = window(role, d, now); err != nil {
			return err
		}
		if err := s.Send(25100, m); err != nil {
			return err
		}
		if err := sendAssets(s, st, role.RID); err != nil {
			return err
		}
		return sendBackpack(s, st)
	})
}
