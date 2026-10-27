package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// guildmarket.json: the guild treasury (公会宝库, 26900~26906) of the client (tools/extract_guildmarket.py).
//
//go:embed data/guildmarket.json
var guildMarketJSON []byte

const (
	gmRoleModule = "guildmarket"
	gmOpPut      = 1 // board operations
	gmOpBuy      = 2
	gmOpExpire   = 3
	gmTypePlay   = 1 // board types: a player's put or buy
	gmTypeSystem = 2 // expiry
	gmBackpack   = 1 // BackPackConst.Bag_Code.BACKPACK
	gmKeepHour   = 5 // an expired batch leaves the treasury at 05:00 of the next day
)

type gmGoods struct {
	ID       int64      `json:"id"`
	Name     string     `json:"name"`
	Price    [][2]int64 `json:"price"`
	Sell     [][2]int64 `json:"sell"`
	Radio    int64      `json:"radio"` // pieces in one group: goods are put and bought by groups
	Type     int64      `json:"type"`  // 0 hero shards, 1 equipment, 2 props, 3 special
	IsSell   int64      `json:"is_sell"`
	MaxPile  int64      `json:"max_pile"` // most pieces of it the treasury holds
	EndTime  int64      `json:"end_time"` // days kept
	DayBuy   int64      `json:"day_buy"`  // most pieces one role buys a day (0 = no limit)
	Capacity int64      `json:"capacity"`
}

var gmCfg = func() (c struct {
	Const map[string]int64
	Items map[string]*gmGoods
}) {
	if err := json.Unmarshal(guildMarketJSON, &c); err != nil || len(c.Items) == 0 {
		panic("game: bad guildmarket.json")
	}
	return
}()

func gmGoodsOf(base int64) *gmGoods { return gmCfg.Items[strconv.FormatInt(base, 10)] }

type gmBatch struct{ Num, End int64 }

type gmBoard struct {
	Type, Op int64
	Name     string
	Base     int64
	Num      int64
	At       int64
}

// gmGuild is the stock of one guild: pieces by goods, kept in batches (oldest first) with their end times.
type gmGuild struct {
	Stock map[string][]gmBatch
	Board []gmBoard
}

// gmRole is what a role did today.
type gmRole struct {
	Day    int64
	Puts   int64
	Bought map[string]int64
}

var gmMu sync.Mutex

func gmGuildModule(gid int64) string { return "guildmarket:" + strconv.FormatInt(gid, 10) }

func (g *gmGuild) total(base int64) int64 {
	var n int64
	for _, b := range g.Stock[strconv.FormatInt(base, 10)] {
		n += b.Num
	}
	return n
}

func (g *gmGuild) note(e gmBoard) {
	g.Board = append(g.Board, e)
	if keep := int(gmCfg.Const["guild_marketplace_borad_num"]); len(g.Board) > keep {
		g.Board = append([]gmBoard(nil), g.Board[len(g.Board)-keep:]...)
	}
}

// expire drops the batches that ran out and returns the goods that changed.
func (g *gmGuild) expire(now time.Time) []int64 {
	var changed []int64
	for k, batches := range g.Stock {
		base, _ := strconv.ParseInt(k, 10, 64)
		var keep []gmBatch
		for _, b := range batches {
			gone := dayStart(time.Unix(b.End, 0)).Add(24*time.Hour + gmKeepHour*time.Hour)
			if now.Before(gone) {
				keep = append(keep, b)
				continue
			}
			g.note(gmBoard{Type: gmTypeSystem, Op: gmOpExpire, Base: base, Num: b.Num, At: now.Unix()})
		}
		if len(keep) != len(batches) {
			changed = append(changed, base)
			if len(keep) == 0 {
				delete(g.Stock, k)
			} else {
				g.Stock[k] = keep
			}
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	return changed
}

func (g *gmGuild) item(base int64) proto.Message {
	ends := []proto.Message{}
	for _, b := range g.Stock[strconv.FormatInt(base, 10)] {
		ends = append(ends, proto.Message{"end_num": b.Num, "end_unixtime": b.End})
	}
	return proto.Message{"id": base, "base_id": base, "num": g.total(base), "attr": []proto.Message{}, "extra": []proto.Message{},
		"holy_eqm_attr": []proto.Message{}, "end_time": ends, "score": 0}
}

func (g *gmGuild) items() []proto.Message {
	var bases []int64
	for k := range g.Stock {
		b, _ := strconv.ParseInt(k, 10, 64)
		bases = append(bases, b)
	}
	sort.Slice(bases, func(i, j int) bool { return bases[i] < bases[j] })
	out := []proto.Message{}
	for _, b := range bases {
		out = append(out, g.item(b))
	}
	return out
}

func (r *gmRole) dayBuy() []proto.Message {
	var bases []string
	for k := range r.Bought {
		bases = append(bases, k)
	}
	sort.Strings(bases)
	out := []proto.Message{}
	for _, k := range bases {
		b, _ := strconv.ParseInt(k, 10, 64)
		out = append(out, proto.Message{"base_id": b, "num": r.Bought[k]})
	}
	return out
}

// RegisterGuildMarket registers the guild treasury (26900 the stock, 26901 put goods, 26902 buy goods, 26903 the board; 26904 is
// pushed to the online members when a stock changes, also when goods expire: the client takes a stack with num 0 away). Members of one guild share a stock; a put of the
// goods the table allows (is_sell, whole groups, props and shards; equipment is not taken) pays the "sell" price in guild coins
// (item 36), a buy pays the "price" in them, with the daily buy limit of the goods; a role may put 20 times a day; goods stay
// 30 days and leave at 05:00 of the next day. Not done: the system sources (guild secret area, guild war) do not fill the
// treasury, and equipment is neither put nor listed.
func RegisterGuildMarket(r *gateway.Router, db *store.Store, hub *gateway.Hub) {
	// with runs fn for a member of a guild that opened the treasury, with the guild's stock (expired goods already out).
	with := func(s *gateway.Session, cmd uint16, fn func(role *store.Role, gid int64, g *gmGuild, d *gmRole, fail func(string) error) error) error {
		role := s.Role()
		if role == nil {
			return nil
		}
		fail := func(msg string) error { return s.Send(cmd, proto.Message{"flag": 0, "msg": msg}) }
		guild, _, err := db.GuildOfRole(role.RID)
		if errors.Is(err, store.ErrNotFound) {
			return fail("您当前未加入任何公会")
		} else if err != nil {
			return err
		}
		if int64(guild.Lev) < gmCfg.Const["open_lev"] {
			return fail("公会等级不足")
		}
		gmMu.Lock()
		defer gmMu.Unlock()
		g := &gmGuild{}
		if _, err := db.LoadGlobal(role.SrvID, gmGuildModule(guild.GID), g); err != nil {
			return err
		}
		if g.Stock == nil {
			g.Stock = map[string][]gmBatch{}
		}
		d := &gmRole{}
		if _, err := db.LoadState(role.RID, gmRoleModule, d); err != nil {
			return err
		}
		if today := dayKey(s.Now()); d.Day != today {
			*d = gmRole{Day: today}
		}
		if d.Bought == nil {
			d.Bought = map[string]int64{}
		}
		gone := g.expire(s.Now())
		if len(gone) > 0 {
			if err := db.SaveGlobal(role.SrvID, gmGuildModule(guild.GID), g); err != nil {
				return err
			}
			_ = notifyChange(hub, db, guild.GID, g, gone, nil)
		}
		return fn(role, guild.GID, g, d, fail)
	}
	saveBoth := func(role *store.Role, gid int64, g *gmGuild, d *gmRole) error {
		if err := db.SaveGlobal(role.SrvID, gmGuildModule(gid), g); err != nil {
			return err
		}
		return db.SaveState(role.RID, gmRoleModule, d)
	}
	r.Handle(26900, func(s *gateway.Session, _ proto.Message) error {
		return with(s, 26900, func(_ *store.Role, _ int64, g *gmGuild, d *gmRole, _ func(string) error) error {
			return s.Send(26900, proto.Message{"item_list": g.items(), "day_buy": d.dayBuy()})
		})
	})
	r.Handle(26903, func(s *gateway.Session, _ proto.Message) error {
		return with(s, 26903, func(_ *store.Role, _ int64, g *gmGuild, _ *gmRole, _ func(string) error) error {
			list := []proto.Message{}
			for _, e := range g.Board {
				who := e.Name
				list = append(list, proto.Message{"type": e.Type, "name": who, "operation": e.Op,
					"reward_list": []proto.Message{{"base_id": e.Base, "num": e.Num}}, "unixtime": e.At})
			}
			return s.Send(26903, proto.Message{"board_list": list})
		})
	})
	r.Handle(26901, func(s *gateway.Session, req proto.Message) error {
		return with(s, 26901, func(role *store.Role, gid int64, g *gmGuild, d *gmRole, fail func(string) error) error {
			base, qty := req.Int("id"), req.Int("quantity")
			it := gmGoodsOf(base)
			switch {
			case it == nil || it.IsSell != 1 || len(it.Sell) == 0:
				return fail("该物品不能放入宝库")
			case it.Type == 1 || req.Int("storage") != gmBackpack:
				return fail("暂不支持放入装备")
			case qty <= 0 || qty%it.Radio != 0:
				return fail("物品数量不足一组，无法放入")
			case d.Puts >= gmCfg.Const["guild_marketplace_sell_time"]:
				return fail("今天放入的次数已用完")
			case g.total(base)+qty > it.MaxPile:
				return fail("宝库里该物品已达存储上限")
			}
			groups := qty / it.Radio
			ga := map[int64]int64{}
			for _, p := range it.Sell {
				ga[p[0]] += p[1] * groups
			}
			sa, si, ya, yi := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains([][2]int64{{base, qty}}), 1, sa, si)
			splitGains(guildGains(mapPairs(ga)), 1, ya, yi)
			if err := db.Exchange(role.RID, sa, si, ya, yi); errors.Is(err, store.ErrConflict) {
				return fail("物品数量不足")
			} else if err != nil {
				return err
			}
			now := s.Now()
			k := strconv.FormatInt(base, 10)
			g.Stock[k] = append(g.Stock[k], gmBatch{Num: qty, End: now.Add(time.Duration(it.EndTime) * 24 * time.Hour).Unix()})
			g.note(gmBoard{Type: gmTypePlay, Op: gmOpPut, Name: role.Name, Base: base, Num: qty, At: now.Unix()})
			d.Puts++
			if err := saveBoth(role, gid, g, d); err != nil {
				return err
			}
			if err := sendAssets(s, db, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, db); err != nil {
				return err
			}
			if err := s.Send(26901, proto.Message{"flag": 1, "msg": "放入成功"}); err != nil {
				return err
			}
			return notifyChange(hub, db, gid, g, []int64{base}, s)
		})
	})
	r.Handle(26902, func(s *gateway.Session, req proto.Message) error {
		return with(s, 26902, func(role *store.Role, gid int64, g *gmGuild, d *gmRole, fail func(string) error) error {
			base, qty := req.Int("id"), req.Int("quantity")
			it := gmGoodsOf(base)
			switch {
			case it == nil || it.Type == 1:
				return fail("物品不存在")
			case qty <= 0 || qty%it.Radio != 0:
				return fail("购买数量不对")
			case g.total(base) < qty:
				return fail("宝库里的库存不足")
			case it.DayBuy > 0 && d.Bought[strconv.FormatInt(base, 10)]+qty > it.DayBuy:
				return fail("已达到每日限购数量")
			}
			groups := qty / it.Radio
			pay := map[int64]int64{}
			for _, p := range it.Price {
				pay[p[0]] += p[1] * groups
			}
			sa, si, ya, yi := map[int64]int64{}, map[int64]int64{}, map[int64]int64{}, map[int64]int64{}
			splitGains(guildGains(mapPairs(pay)), 1, sa, si)
			splitGains(guildGains([][2]int64{{base, qty}}), 1, ya, yi)
			if err := db.Exchange(role.RID, sa, si, ya, yi); errors.Is(err, store.ErrConflict) {
				return fail("公会币不足")
			} else if err != nil {
				return err
			}
			k := strconv.FormatInt(base, 10)
			left := qty // the batches that end first go first
			batches := g.Stock[k]
			for i := range batches {
				take := min(left, batches[i].Num)
				batches[i].Num -= take
				left -= take
			}
			var keep []gmBatch
			for _, b := range batches {
				if b.Num > 0 {
					keep = append(keep, b)
				}
			}
			if len(keep) == 0 {
				delete(g.Stock, k)
			} else {
				g.Stock[k] = keep
			}
			d.Bought[k] += qty
			g.note(gmBoard{Type: gmTypePlay, Op: gmOpBuy, Name: role.Name, Base: base, Num: qty, At: s.Now().Unix()})
			if err := saveBoth(role, gid, g, d); err != nil {
				return err
			}
			if err := sendAssets(s, db, role.RID); err != nil {
				return err
			}
			if err := sendBackpack(s, db); err != nil {
				return err
			}
			if err := s.Send(26902, proto.Message{"flag": 1, "msg": "兑换成功"}); err != nil {
				return err
			}
			return notifyChange(hub, db, gid, g, []int64{base}, s)
		})
	})
}

func mapPairs(m map[int64]int64) [][2]int64 {
	var ids []int64
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([][2]int64, 0, len(ids))
	for _, id := range ids {
		out = append(out, [2]int64{id, m[id]})
	}
	return out
}

// members are the online sessions of a guild.
func gmOnline(hub *gateway.Hub, db *store.Store, gid int64) []*gateway.Session {
	rows, err := db.MembersOf(gid)
	if err != nil || hub == nil {
		return nil
	}
	var out []*gateway.Session
	for _, m := range rows {
		if sess := hub.Online(m.RID); sess != nil {
			out = append(out, sess)
		}
	}
	return out
}

// notifyChange pushes the changed stacks (26904; a stack that ran out has num 0, which the client removes) to the other online members.
func notifyChange(hub *gateway.Hub, db *store.Store, gid int64, g *gmGuild, bases []int64, except *gateway.Session) error {
	list := []proto.Message{}
	for _, b := range bases {
		list = append(list, g.item(b))
	}
	for _, sess := range gmOnline(hub, db, gid) {
		if sess != except {
			if err := sess.Send(26904, proto.Message{"item_list": list}); err != nil {
				continue
			}
		}
	}
	// the one who acted sees its own change too
	if except == nil {
		return nil
	}
	return except.Send(26904, proto.Message{"item_list": list})
}
