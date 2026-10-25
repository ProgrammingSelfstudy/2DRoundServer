package game

import (
	"sort"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
)

// profileHeroes is how many heroes the profile card of another role shows. The
// original number is unknown, so this is our policy (the five of a formation).
const profileHeroes = 5

// registerProfile hooks the profile card: the client sends it when a role taps
// another role's name or avatar (chat, friends, rankings).
func (f *FriendService) registerProfile(r *gateway.Router) {
	r.Handle(10315, f.profile)
}

// profile answers 10315 with the public data of a role of this server. A role
// that does not exist, is banned or lives on another server gets no reply: the
// client opens its panel only on an answer, and the original refusal is unknown.
// Fields without a source (vip, guild, honor, badges, fans, space, sprites) are 0
// or empty, like in the friend entries.
func (f *FriendService) profile(s *gateway.Session, req proto.Message) error {
	me := s.Role()
	if me == nil {
		return nil
	}
	t, _ := f.target(req.Int("rid"), req.Str("srv_id"))
	if t == nil {
		return nil
	}
	heroes, err := f.Store.PartnersOf(t.RID)
	if err != nil {
		return err
	}
	shown := []proto.Message{}
	known := make([]proto.Message, 0, len(heroes))
	for _, h := range heroes {
		if _, ok := partnerTable[h.BID]; ok {
			known = append(known, partnerMessage(h))
		}
	}
	sort.SliceStable(known, func(i, j int) bool { return known[i].Int("power") > known[j].Int("power") })
	var power int64
	for i := 0; i < len(known) && i < profileHeroes; i++ {
		m := known[i]
		power += m.Int("power")
		shown = append(shown, proto.Message{
			"id": m.Int("partner_id"), "bid": m.Int("bid"), "lev": m.Int("lev"), "star": m.Int("star"),
			"quality": 0, "awaken_lev": 0, "use_skin": 0, "end_time": 0, "resonate_lev": 0,
		})
	}
	isFriend := false
	if t.RID != me.RID {
		if isFriend, err = f.Store.AreFriends(me.RID, t.RID); err != nil {
			return err
		}
	}
	gname := ""
	if gd, _, err := f.Store.GuildOfRole(t.RID); err == nil {
		gname = gd.Name
	}
	return s.Send(10315, proto.Message{
		"rid": t.RID, "srv_id": t.SrvID, "name": t.Name, "gname": gname, "lev": t.Lev,
		"face_id": t.FaceID, "power": power, "sex": t.Sex,
		"partner_list": shown, "room_partner_list": []proto.Message{},
		"honor_list": []proto.Message{}, "use_badges": []proto.Message{}, "sprites": []proto.Message{},
		"is_friend": b2i(isFriend),
	})
}
