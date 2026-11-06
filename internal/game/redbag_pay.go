package game

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// The red bag of this client is paid for (capture 2026-11-06, testdata/captures/original-2026-11-06-guild): the three buttons
// "12元/50元/648元 发红包" ask "确定要充值吗？" and call pay_web with the products 501, 502 and 503, there is no 13535 and no red
// packet token. The sender gets the bag's `reward` (diamonds and guild contribution) at once, the bag appears for the whole guild.
// (13535 with the token stays for clients that send it.)

// redbagProducts maps the charge products to the red bag kinds of redbag.json.
var redbagProducts = map[int64]int64{501: 1, 502: 2, 503: 3}

// ErrPayMsg is a refusal of a payment that has a reason to show the player; it is an ErrNotPayable.
type ErrPayMsg struct{ Msg string }

func (e *ErrPayMsg) Error() string        { return e.Msg }
func (e *ErrPayMsg) Is(target error) bool { return target == ErrNotPayable }

// PayMsg returns the reason of a refusal that carries one.
func PayMsg(err error) (string, bool) {
	var m *ErrPayMsg
	if errors.As(err, &m) {
		return m.Msg, true
	}
	return "", false
}

func init() {
	for pid, kid := range redbagProducts {
		pid, kid := pid, kid
		RegisterChargeGuard(pid, func(st *store.Store, role *store.Role) error {
			_, err := redbagMayBeSent(st, role, redbagCfg[kid], chargeClock())
			return err
		})
		RegisterChargeHook(pid, func(st *store.Store, role *store.Role, _ ChargeProduct, _ int64) (map[int64]int64, map[int64]int64, error) {
			kind := redbagCfg[kid]
			now := chargeClock()
			redbagMu.Lock()
			defer redbagMu.Unlock()
			m, err := redbagMayBeSent(st, role, kind, now)
			if err != nil {
				return nil, nil, err
			}
			d, err := loadRedbagDay(st, role.RID, now)
			if err != nil {
				return nil, nil, err
			}
			book, err := loadRedbagBook(st, role.SrvID, now)
			if err != nil {
				return nil, nil, err
			}
			face := int64(role.FaceID)
			if face == 0 {
				face = store.DefaultFaceID
			}
			book.Next++
			book.Bags = append(book.Bags, &redbag{ID: book.Next, GID: m.GID, Kind: kind.ID, RID: role.RID, Name: role.Name, Face: face, Post: m.Post, Val: kind.Val, Max: kind.Num, Sent: now.Unix()})
			if err := st.SaveGlobal(role.SrvID, redbagModule, book); err != nil {
				return nil, nil, err
			}
			d.Sent++
			d.Kind[kind.ID]++
			if err := st.SaveState(role.RID, redbagDayMod, d); err != nil {
				return nil, nil, err
			}
			assets, items := map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(kind.Reward), 1, assets, items)
			return assets, items, nil
		})
	}
}

// redbagMayBeSent says whether the role may send a bag of the kind now: it is in a guild and the daily limits are not used up.
func redbagMayBeSent(st *store.Store, role *store.Role, kind redbagKind, now time.Time) (*store.GuildMember, error) {
	_, m, err := st.GuildOfRole(role.RID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &ErrPayMsg{"你还没有加入公会"}
	}
	if err != nil {
		return nil, err
	}
	d, err := loadRedbagDay(st, role.RID, now)
	if err != nil {
		return nil, err
	}
	switch {
	case d.Sent >= guildCfg.Const["red_packet_send"]:
		return nil, &ErrPayMsg{"今日发红包次数已达上限"}
	case d.Kind[kind.ID] >= kind.Limit:
		return nil, &ErrPayMsg{"该红包今日发放次数已达上限"}
	}
	return m, nil
}

// RedbagOfPayment is the id of the red bag a pay_web payment of a red bag product just put into the guild (0 = none).
func RedbagOfPayment(st *store.Store, role *store.Role, productID int64, now time.Time) int64 {
	if _, ok := redbagProducts[productID]; !ok {
		return 0
	}
	book, err := loadRedbagBook(st, role.SrvID, now)
	if err != nil {
		return 0
	}
	for i := len(book.Bags) - 1; i >= 0; i-- {
		if book.Bags[i].RID == role.RID {
			return book.Bags[i].ID
		}
	}
	return 0
}

// PushRedbagSent shows a new red bag to the online members of its guild: the list line (13534), the guild channel call (12761,
// channel 256) and the log line (13578), and tells the sender how many of each kind it sent today (13546).
func (m *MailService) PushRedbagSent(bagID int64) (int, error) {
	redbagMu.Lock()
	defer redbagMu.Unlock()
	book, err := loadRedbagBook(m.Store, m.SrvID, m.now())
	if err != nil {
		return 0, err
	}
	var b *redbag
	for _, x := range book.Bags {
		if x.ID == bagID {
			b = x
		}
	}
	if b == nil {
		return 0, store.ErrNotFound
	}
	kind := redbagCfg[b.Kind]
	rows, err := m.Store.MembersOf(b.GID)
	if err != nil {
		return 0, err
	}
	gd, err := m.Store.GuildByID(b.GID)
	if err != nil {
		return 0, err
	}
	now := m.now().Unix()
	logMsg := fmt.Sprintf("在公会中发出了一个%s。", kind.Name)
	if err := m.Store.AddGuildLog(store.GuildLog{GID: b.GID, Type: 4, RID: b.RID, RoleName: b.Name, Msg: logMsg, Time: now}); err != nil {
		return 0, err
	}
	var logEntry proto.Message
	if logs, err := m.Store.GuildLogs(b.GID, 1); err == nil && len(logs) > 0 {
		logEntry = proto.Message{"id": logs[0].ID % 256, "gid": b.GID, "gsrv_id": gd.SrvID, "type": 4, "time": now, "rid": b.RID, "srv_id": gd.SrvID, "role_name": b.Name, "msg": logMsg}
	}
	chat := proto.Message{"role_list": []proto.Message{}, "channel": chatGuildSystem, "len": 0, "tick": now, "msg": fmt.Sprintf(
		"<div click=3|%d|%s|%s fontcolor=#13a5e2>%s</div>慷慨解囊，发出一个<div fontcolor=#ef3a3a>%s</div>，大家快来抢，手快有手慢无！<div href=7|%d fontcolor=#c26706>【抢红包】</div>",
		b.RID, b.Name, gd.SrvID, b.Name, kind.Name, b.ID)}
	n := 0
	for _, r := range rows {
		sess := m.Hub.Online(r.RID)
		if sess == nil {
			continue
		}
		send := func(cmd uint16, msg proto.Message) {
			if err := sess.Send(cmd, msg); err != nil && m.Log != nil {
				m.Log.Printf("redbag: push %d to role %d: %v", cmd, r.RID, err)
			}
		}
		rb := *b
		send(13534, proto.Message{"type": 1, "list": []proto.Message{redbagRowOf(&rb, r.RID)}})
		if r.RID == b.RID {
			d, err := loadRedbagDay(m.Store, r.RID, m.now())
			if err == nil {
				ids := make([]int64, 0, len(d.Kind))
				for id := range d.Kind {
					ids = append(ids, id)
				}
				sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
				list := []proto.Message{}
				for _, id := range ids {
					list = append(list, proto.Message{"id": id, "num": d.Kind[id]})
				}
				send(13546, proto.Message{"list": list})
			}
		}
		send(12761, chat)
		if logEntry != nil {
			send(13578, proto.Message{"guild_log_info_list": []proto.Message{logEntry}})
		}
		n++
	}
	return n, nil
}

func redbagRowOf(b *redbag, rid int64) proto.Message {
	flag := int64(0)
	if b.claimedBy(rid) {
		flag = 1
	}
	return proto.Message{"id": b.ID, "type": b.Kind, "name": b.Name, "post": b.Post, "num": len(b.Claims), "max_num": b.Max, "max_val": b.Val,
		"time": b.Sent + redbagLife, "flag": flag, "msg_id": 0}
}

// chatGuildSystem is the channel of the guild's own calls (a red bag, a new member), not the speaking channel chatGuild.
const chatGuildSystem = 256

func (m *MailService) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
