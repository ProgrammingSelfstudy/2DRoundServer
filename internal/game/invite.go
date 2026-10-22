package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// invite_tasks.json is the client's invite reward table (tools/extract_invite_tasks.py).
//
//go:embed data/invite_tasks.json
var inviteTasksJSON []byte

type inviteTask struct {
	ID    int64      `json:"id"`
	Max   int64      `json:"max"`  // how many times the reward can be claimed
	Cond  [2]any     `json:"cond"` // "power" or "vip", and the level a invited role must reach
	Items [][2]int64 `json:"items"`
}

var inviteTasks = func() []inviteTask {
	var f struct {
		Steps []inviteTask `json:"steps"`
	}
	if err := json.Unmarshal(inviteTasksJSON, &f); err != nil || len(f.Steps) != 14 {
		panic("game: bad invite_tasks.json")
	}
	return f.Steps
}()

// maxInvitees is how many roles may enter one code (Config.InviteCodeData.data_const.bind_num).
const maxInvitees = 20

// InviteMetrics gives the power and VIP level of a role. The server has neither
// system yet, so by default both are 0, and the invite rewards, which depend on
// them, cannot be reached until there are.
type InviteMetrics func(rid int64) (power, vip int64)

// InviteService implements invite codes: a role's own code (19800), entering
// someone else's (19801), the list of invited roles (19802, pushed on a new
// one as 19803), and the reward steps (19804 progress, 19805 claim). The return
// rewards (19811/19812) and 19807 are not implemented.
type InviteService struct {
	Store   *store.Store
	SrvID   string
	Hub     *gateway.Hub
	Log     *log.Logger
	Metrics InviteMetrics
}

func (v *InviteService) Register(r *gateway.Router) {
	r.Handle(19800, v.myCode)
	r.Handle(19801, v.bind)
	r.Handle(19802, v.invitees)
	r.Handle(19804, v.progress)
	r.Handle(19805, v.claim)
}

func (v *InviteService) metrics(rid int64) (int64, int64) {
	if v.Metrics == nil {
		return 0, 0
	}
	return v.Metrics(rid)
}

func (v *InviteService) myCode(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	code, err := v.Store.InviteCode(me.RID)
	if err != nil {
		return err
	}
	return s.Send(19800, proto.Message{"code": code})
}

func (v *InviteService) entry(r *store.Role) proto.Message {
	power, vip := v.metrics(r.RID)
	return proto.Message{
		"rid": r.RID, "srv_id": r.SrvID, "name": r.Name, "lev": r.Lev, "vip": vip, "sex": r.Sex,
		"online": b2i(v.Hub.Online(r.RID) != nil), "power": power, "face_id": r.FaceID,
	}
}

func (v *InviteService) bind(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	reply := func(ok bool, msg string) error { return s.Send(19801, proto.Message{"code": b2i(ok), "msg": msg}) }
	// "only an account's first role may enter a code": the lowest role id of the account on this server
	mine, err := v.Store.RolesByAccount(me.AccountID, v.SrvID)
	if err != nil {
		return err
	}
	if len(mine) == 0 || mine[0].RID != me.RID {
		return reply(false, "只有账号的首个角色才可以填写邀请码")
	}
	if has, err := v.Store.HasInviter(me.RID); err != nil || has {
		if err != nil {
			return err
		}
		return reply(false, "你已经填写过邀请码了")
	}
	inviter, err := v.Store.RoleByInviteCode(req.Int("code"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		return reply(false, "邀请码不存在")
	case err != nil:
		return err
	case inviter.AccountID == me.AccountID:
		return reply(false, "不能填写自己账号的邀请码")
	case inviter.Status != 1:
		return reply(false, "邀请码不存在")
	}
	switch err := v.Store.BindInvite(me.RID, inviter.RID, maxInvitees); {
	case errors.Is(err, store.ErrInviteFull):
		return reply(false, "该邀请码的绑定数量已满")
	case errors.Is(err, store.ErrConflict):
		return reply(false, "你已经填写过邀请码了")
	case err != nil:
		return err
	}
	if sess := v.Hub.Online(inviter.RID); sess != nil {
		if err := sess.Send(19803, v.entry(me)); err != nil {
			v.Log.Printf("invite: push to role %d: %v", inviter.RID, err)
		}
	}
	return reply(true, "绑定成功")
}

func (v *InviteService) invitees(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rs, err := v.Store.Invitees(me.RID)
	if err != nil {
		return err
	}
	list := make([]proto.Message, 0, len(rs))
	for i := range rs {
		list = append(list, v.entry(&rs[i]))
	}
	return s.Send(19802, proto.Message{"list": list})
}

// reached counts the invited roles that meet a step's condition.
func (v *InviteService) reached(rs []store.Role, t inviteTask) int64 {
	kind, _ := t.Cond[0].(string)
	need, _ := t.Cond[1].(float64)
	var n int64
	for _, r := range rs {
		power, vip := v.metrics(r.RID)
		if (kind == "power" && float64(power) >= need) || (kind == "vip" && float64(vip) >= need) {
			n++
		}
	}
	return n
}

// claimable is how many times the role may claim a step so far: the invited
// roles that reached it, at most the step's limit.
func claimable(reached, max int64) int64 {
	if reached > max {
		return max
	}
	return reached
}

func (v *InviteService) progress(s *gateway.Session, _ proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	rs, err := v.Store.Invitees(me.RID)
	if err != nil {
		return err
	}
	had, err := v.Store.InviteClaims(me.RID)
	if err != nil {
		return err
	}
	list := make([]proto.Message, 0, len(inviteTasks))
	for _, t := range inviteTasks {
		list = append(list, proto.Message{"id": t.ID, "num": claimable(v.reached(rs, t), t.Max), "had": had[t.ID]})
	}
	return s.Send(19804, proto.Message{"list": list})
}

func (v *InviteService) claim(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	id := req.Int("id")
	var task *inviteTask
	for i := range inviteTasks {
		if inviteTasks[i].ID == id {
			task = &inviteTasks[i]
		}
	}
	reply := func(ok bool, msg string, num, had int64) error {
		return s.Send(19805, proto.Message{"code": b2i(ok), "msg": msg, "id": id, "num": num, "had": had})
	}
	if task == nil {
		return reply(false, "奖励不存在", 0, 0)
	}
	rs, err := v.Store.Invitees(me.RID)
	if err != nil {
		return err
	}
	allowed := claimable(v.reached(rs, *task), task.Max)
	claims, err := v.Store.InviteClaims(me.RID)
	if err != nil {
		return err
	}
	if claims[id] >= allowed {
		return reply(false, "没有可领取的奖励", allowed, claims[id])
	}
	gains := make([]sellGain, 0, len(task.Items))
	for _, it := range task.Items {
		gains = append(gains, sellGain{ID: it[0], Amount: it[1]})
	}
	assets, items := map[int64]int64{}, map[int64]int64{}
	splitGains(gains, 1, assets, items)
	switch err := v.Store.ClaimInvite(me.RID, id, allowed, assets, items); {
	case errors.Is(err, store.ErrConflict):
		return reply(false, "没有可领取的奖励", allowed, claims[id]) // claimed from another session in between
	case err != nil:
		return err
	}
	if err := reply(true, "领取成功", allowed, claims[id]+1); err != nil {
		return err
	}
	if err := sendAssets(s, v.Store, me.RID); err != nil {
		return err
	}
	return sendBackpack(s, v.Store)
}
