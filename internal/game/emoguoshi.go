package game

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

// emoguoshi.json: the devil fruit collection of the client (tools/extract_emoguoshi.py).
//
//go:embed data/emoguoshi.json
var emoguoshiJSON []byte

const (
	emoModule    = "emoguoshi"
	emoSeed      = 42    // 恶魔之种, the currency of the shop and the lottery
	emoSource    = 19502 // 力量源质
	emoUniversal = 19500 // 万能碎片: what an awakening asks for besides the fruit's own shards
	emoDiamond   = 3
	emoCycle     = 15 * 24 * 3600
)

type emoItems = [][]int64

type emoLevelRow struct {
	NeedItems emoItems `json:"need_items"`
	NeedStar  int64    `json:"need_star"`
}

type emoStarRow struct {
	NeedItems   emoItems          `json:"need_items"`
	NeedPartner emoItems          `json:"need_partner"`
	MaxLevel    int64             `json:"max_level"`
	Effect      string            `json:"effect"`
	EffectCond  []json.RawMessage `json:"effect_cond"`
	EffectParam []json.RawMessage `json:"effect_param"`
}

type emoJuexingRow struct {
	Name       string            `json:"name"`
	NeedItems1 emoItems          `json:"need_items1"`
	NeedItems2 emoItems          `json:"need_items2"`
	NeedStar   int64             `json:"need_star"`
	Effect     string            `json:"effect"`
	EffectCond []json.RawMessage `json:"effect_cond"`
}

type emoBox struct {
	Bid           int64    `json:"bid"`
	NeedJifen     int64    `json:"need_jifen"`
	OneAddItems   emoItems `json:"one_add_items"`
	OneNeedItems1 emoItems `json:"one_need_items1"`
	OneNeedItems2 emoItems `json:"one_need_items2"`
	TenAddItems   emoItems `json:"ten_add_items"`
	TenNeedItems1 emoItems `json:"ten_need_items1"`
	TenNeedItems2 emoItems `json:"ten_need_items2"`
}

type emoDo struct {
	GroupID      int64 `json:"group_id"`
	Weight       int64 `json:"weight"`
	IsTenGet     int64 `json:"is_ten_get"`
	MustGetN     int64 `json:"must_get_n"`
	NotGetN      int64 `json:"not_get_n"`
	FirstGetN    int64 `json:"first_get_n"`
	FirstNotGetN int64 `json:"first_not_get_n"`
}

type emoGroupItem struct {
	ID          int64 `json:"id"`
	ItemID      int64 `json:"item_id"`
	ItemNum     int64 `json:"item_num"`
	Weight      int64 `json:"weight"`
	UpAddWeight int64 `json:"up_add_weight"`
}

type emoGood struct {
	ID        int64 `json:"id"`
	ItemID    int64 `json:"item_id"`
	ItemNum   int64 `json:"item_num"`
	Limit     int64 `json:"limit"`
	NeedJifen int64 `json:"need_jifen"`
	NeedLevel int64 `json:"need_level"`
	PayType   int64 `json:"pay_type"`
	Price     int64 `json:"price"`
}

type emoGift struct {
	ChargeID    int64    `json:"charge_id"`
	Items1      emoItems `json:"items1"`
	Items2      emoItems `json:"items2"`
	Process     int64    `json:"process"`
	UnlockItems emoItems `json:"unlock_items"`
}

type emoConf struct {
	Const   map[string]json.RawMessage `json:"const"`
	Fruits  map[string]string          `json:"fruits"`
	Level   map[string]emoLevelRow     `json:"level"`
	Star    map[string]emoStarRow      `json:"star"`
	Juexing map[string]emoJuexingRow   `json:"juexing"`
	Boxes   map[string]emoBox          `json:"boxes"`
	Do      map[string][]emoDo         `json:"do"`
	Groups  map[string][]emoGroupItem  `json:"groups"`
	Shop    map[string]emoGood         `json:"shop"`
	Gift    [][]emoGift                `json:"gift"`

	shardOf map[int64]int64 // shard item -> fruit id
}

func (c *emoConf) num(name string) int64 {
	var n int64
	_ = json.Unmarshal(c.Const[name], &n)
	return n
}

var emoCfg = func() *emoConf {
	c := &emoConf{}
	if err := json.Unmarshal(emoguoshiJSON, c); err != nil {
		panic("game: bad emoguoshi.json: " + err.Error())
	}
	if len(c.Fruits) != 31 || len(c.Level) == 0 || len(c.Star) == 0 || len(c.Gift) == 0 {
		panic("game: bad emoguoshi.json")
	}
	c.shardOf = map[int64]int64{}
	for k, row := range c.Level { // the item the first level asks for is the fruit's shard
		if len(k) > 2 && k[len(k)-2:] == "_1" && len(row.NeedItems) > 0 {
			id, _ := strconv.ParseInt(k[:len(k)-2], 10, 64)
			c.shardOf[row.NeedItems[0][0]] = id
		}
	}
	return c
}()

type emoFruit struct{ Level, Star, Juexing int64 }

type emoState struct {
	Fruits    map[string]emoFruit
	Bought    map[string]int64
	Jifen     int64
	DrawDay   int64
	DrawToday int64
	Total     map[string]int64 // pulls of a box
	Since     map[string]int64 // pulls since a group last hit, "box:group"
	Hits      map[string]int64
	Lucky     map[string]int64 // the wish (entry id) of a group, "group"
	Layer     int64
	GiftStart int64
	GiftValue int64
	GiftOpen  bool
	Tag1      map[string]bool
}

type emoSvc struct{ st *store.Store }

// RegisterEmoguoshi registers the devil fruit collection (31101-31130): every fruit can be levelled, starred up and
// awakened with its shards; the shop sells shards for 恶魔之种, the lottery boxes give them; the fruit gift is a battle
// pass over the day's activity. The fruits' attribute bonuses and effects are sent to the client (31127) but are not
// added to the heroes' stats, and the client has no daily activity points here, so the pass fills by our own rule.
func RegisterEmoguoshi(r *gateway.Router, db *store.Store) {
	e := &emoSvc{st: db}
	r.Handle(31101, e.levelUp)
	r.Handle(31102, e.one)
	r.Handle(31103, e.all)
	r.Handle(31104, e.starUp)
	r.Handle(31105, e.awaken)
	r.Handle(31106, e.buy)
	r.Handle(31108, e.shopList)
	r.Handle(31109, e.jifen)
	r.Handle(31115, e.draw)
	r.Handle(31116, e.drawTimes)
	r.Handle(31120, e.giftInfo)
	r.Handle(31122, e.giftTake)
	r.Handle(31123, e.giftUnlock)
	r.Handle(31130, e.decompose)
}

func (e *emoSvc) with(s *gateway.Session, fn func(role *store.Role, st *emoState) error) error {
	role := s.Role()
	if role == nil {
		return nil
	}
	if fresh, err := e.st.RoleByID(role.RID); err == nil {
		role = fresh
	}
	st := &emoState{}
	if _, err := e.st.LoadState(role.RID, emoModule, st); err != nil {
		return err
	}
	if st.Fruits == nil {
		st.Fruits = map[string]emoFruit{}
	}
	for _, m := range []*map[string]int64{&st.Bought, &st.Total, &st.Since, &st.Hits, &st.Lucky} {
		if *m == nil {
			*m = map[string]int64{}
		}
	}
	if st.Tag1 == nil {
		st.Tag1 = map[string]bool{}
	}
	if day := summonDay(s.Now()); st.DrawDay != day {
		st.DrawDay, st.DrawToday = day, 0
	}
	if st.GiftStart == 0 || s.Now().Unix() >= st.GiftStart+emoCycle { // a new cycle: the gift and the shop limits start over
		st.GiftStart, st.GiftValue, st.GiftOpen, st.Tag1, st.Bought = s.Now().Unix(), 0, false, map[string]bool{}, map[string]int64{}
		st.Layer = max(1, e.unlockedBoxes(role, st))
	}
	if err := fn(role, st); err != nil {
		return err
	}
	return e.st.SaveState(role.RID, emoModule, st)
}

func key2(a, b int64) string { return fmt.Sprintf("%d_%d", a, b) }

func (e *emoSvc) fruit(st *emoState, id int64) emoFruit { return st.Fruits[strconv.FormatInt(id, 10)] }

func (e *emoSvc) setFruit(st *emoState, id int64, f emoFruit) {
	st.Fruits[strconv.FormatInt(id, 10)] = f
}

func (e *emoSvc) maxStar(id int64) int64 {
	n := int64(0)
	for ; ; n++ {
		if _, ok := emoCfg.Star[key2(id, n+1)]; !ok {
			return n
		}
	}
}

func (e *emoSvc) fruitMessage(id int64, f emoFruit) proto.Message {
	return proto.Message{"id": id, "level": f.Level, "star": f.Star, "juexing": f.Juexing}
}

func (e *emoSvc) pay(role *store.Role, items [][]int64, times int64) (bool, error) {
	sa, si := map[int64]int64{}, map[int64]int64{}
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	splitGains(guildGains(l), times, sa, si)
	err := e.st.Exchange(role.RID, sa, si, map[int64]int64{}, map[int64]int64{})
	if errors.Is(err, store.ErrConflict) {
		return false, nil
	}
	return err == nil, err
}

func (e *emoSvc) give(role *store.Role, items [][]int64) error {
	var l [][2]int64
	for _, p := range items {
		if len(p) > 1 && p[1] > 0 {
			l = append(l, [2]int64{p[0], p[1]})
		}
	}
	ga, gi := map[int64]int64{}, map[int64]int64{}
	splitGains(guildGains(l), 1, ga, gi)
	return e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{}, ga, gi)
}

func (e *emoSvc) refresh(s *gateway.Session, role *store.Role) error {
	if err := sendAssets(s, e.st, role.RID); err != nil {
		return err
	}
	return sendBackpack(s, e.st)
}

// effects totals what the fruits do besides the attributes: every star row names an effect (by effect_cond[0]) and its
// strength; awakening adds its own.
func (e *emoSvc) effects(st *emoState) []proto.Message {
	sum := map[string]int64{}
	var order []string
	add := func(name string, v int64) {
		if _, ok := sum[name]; !ok {
			order = append(order, name)
		}
		sum[name] += v
	}
	for k, f := range st.Fruits {
		id, _ := strconv.ParseInt(k, 10, 64)
		if f.Level <= 0 {
			continue
		}
		if row, ok := emoCfg.Star[key2(id, f.Star)]; ok && len(row.EffectCond) > 0 && len(row.EffectParam) > 0 {
			var name string
			var v int64
			if json.Unmarshal(row.EffectCond[0], &name) == nil && json.Unmarshal(row.EffectParam[0], &v) == nil {
				add(name, v)
			}
		}
	}
	sort.Strings(order)
	out := []proto.Message{}
	for _, n := range order {
		out = append(out, proto.Message{"type": n, "val": sum[n]})
	}
	return out
}

func (e *emoSvc) pushEffects(s *gateway.Session, st *emoState) error {
	return s.Send(31127, proto.Message{"effect": e.effects(st)})
}

func (e *emoSvc) one(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *emoState) error {
		return s.Send(31102, e.fruitMessage(id, e.fruit(st, id)))
	})
}

func (e *emoSvc) all(s *gateway.Session, _ proto.Message) error {
	return e.with(s, func(role *store.Role, st *emoState) error {
		var ids []int64
		for k := range st.Fruits {
			id, _ := strconv.ParseInt(k, 10, 64)
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		list := []proto.Message{}
		for _, id := range ids {
			list = append(list, e.fruitMessage(id, e.fruit(st, id)))
		}
		if err := s.Send(31103, proto.Message{"list": list}); err != nil {
			return err
		}
		return e.pushEffects(s, st)
	})
}

// tick adds to the fruit gift's activity (our rule: the client counts daily-task points, which we do not have).
func (st *emoState) tick(n int64) { st.GiftValue += n }

func (e *emoSvc) levelUp(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31101, proto.Message{"result": 0, "msg": msg}) }
		f := e.fruit(st, id)
		row, ok := emoCfg.Level[key2(id, f.Level+1)]
		switch {
		case !ok:
			return refuse("已达最高等级")
		case f.Level > 0 && row.NeedStar > f.Star:
			return refuse("星级不足")
		}
		paid, err := e.pay(role, row.NeedItems, 1)
		if err != nil {
			return err
		}
		if !paid {
			return refuse("材料不足")
		}
		f.Level++
		if f.Level == 1 {
			f.Star = 1
		}
		e.setFruit(st, id, f)
		st.tick(10)
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(31102, e.fruitMessage(id, f)); err != nil {
			return err
		}
		if err := e.pushEffects(s, st); err != nil {
			return err
		}
		return s.Send(31101, proto.Message{"result": 1, "msg": "升级成功"})
	})
}

// starUp (31104): the shards of the fruit and heroes of the stars the row names (use_list), the heroes are consumed.
func (e *emoSvc) starUp(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31104, proto.Message{"result": 0, "msg": msg}) }
		f := e.fruit(st, id)
		row, ok := emoCfg.Star[key2(id, f.Star+1)]
		switch {
		case f.Level <= 0:
			return refuse("请先激活该果实")
		case !ok:
			return refuse("已达最高星级")
		}
		heroes, err := e.st.PartnersOf(role.RID)
		if err != nil {
			return err
		}
		byID := map[int64]store.Partner{}
		for _, h := range heroes {
			byID[h.PartnerID] = h
		}
		used := map[int64]bool{}
		var consumed []int64
		for _, need := range row.NeedPartner { // [hero bid (0 = any), star, count]
			if len(need) < 3 {
				continue
			}
			got := int64(0)
			for _, u := range req.Arr("use_list") {
				h, ok := byID[u.Int("use_id")]
				if !ok || used[h.PartnerID] || h.Locked || h.ResSlot > 0 || int64(h.Star) != need[1] || (need[0] != 0 && h.BID != need[0]) || got == need[2] {
					continue
				}
				used[h.PartnerID] = true
				consumed = append(consumed, h.PartnerID)
				got++
			}
			if got < need[2] {
				return refuse("消耗的英雄不足")
			}
		}
		sa, si := map[int64]int64{}, map[int64]int64{}
		var l [][2]int64
		for _, p := range row.NeedItems {
			l = append(l, [2]int64{p[0], p[1]})
		}
		splitGains(guildGains(l), 1, sa, si)
		if err := e.st.ConsumePartners(role.RID, consumed, sa, si, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("材料不足")
		} else if err != nil {
			return err
		}
		f.Star++
		e.setFruit(st, id, f)
		st.tick(10)
		if len(consumed) > 0 {
			gone := []proto.Message{}
			for _, c := range consumed {
				gone = append(gone, proto.Message{"partner_id": c})
			}
			if err := s.Send(11006, proto.Message{"expend2": gone}); err != nil {
				return err
			}
		}
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(31102, e.fruitMessage(id, f)); err != nil {
			return err
		}
		if err := e.pushEffects(s, st); err != nil {
			return err
		}
		return s.Send(31104, proto.Message{"result": 1, "msg": "升星成功"})
	})
}

// awaken (31105): at the top star, with the fruit's shards and beli, and shards of other fruits that are at their
// top star (use_list: item and count) worth the universal amount.
func (e *emoSvc) awaken(s *gateway.Session, req proto.Message) error {
	id := req.Int("id")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31105, proto.Message{"result": 0, "msg": msg}) }
		f := e.fruit(st, id)
		row, ok := emoCfg.Juexing[strconv.FormatInt(id, 10)]
		switch {
		case !ok:
			return refuse("该果实不能觉醒")
		case f.Juexing > 0:
			return refuse("已经觉醒")
		case f.Level <= 0 || f.Star < e.maxStar(id) || f.Star < row.NeedStar:
			return refuse("星级未满")
		}
		var needUniversal int64
		for _, p := range row.NeedItems2 {
			needUniversal += p[1]
		}
		spendA, spendI := map[int64]int64{}, map[int64]int64{}
		var l [][2]int64
		for _, p := range row.NeedItems1 {
			l = append(l, [2]int64{p[0], p[1]})
		}
		splitGains(guildGains(l), 1, spendA, spendI)
		var got int64
		for _, u := range req.Arr("use_list") {
			fruit, ok := emoCfg.shardOf[u.Int("use_id")]
			if !ok || e.fruit(st, fruit).Star < e.maxStar(fruit) || e.fruit(st, fruit).Level <= 0 || u.Int("use_num") <= 0 {
				return refuse("只有满星果实的碎片可以代替")
			}
			spendI[u.Int("use_id")] += u.Int("use_num")
			got += u.Int("use_num")
		}
		if got < needUniversal {
			return refuse("碎片数量不足")
		}
		if err := e.st.Exchange(role.RID, spendA, spendI, map[int64]int64{}, map[int64]int64{}); errors.Is(err, store.ErrConflict) {
			return refuse("材料不足")
		} else if err != nil {
			return err
		}
		f.Juexing = 1
		e.setFruit(st, id, f)
		st.tick(20)
		if err := e.refresh(s, role); err != nil {
			return err
		}
		if err := s.Send(31102, e.fruitMessage(id, f)); err != nil {
			return err
		}
		if err := e.pushEffects(s, st); err != nil {
			return err
		}
		return s.Send(31105, proto.Message{"result": 1, "msg": "觉醒成功"})
	})
}

// decompose (31130): shards of an awakened fruit turn into 力量源质 (25 for a purple fruit, 50 for an orange one).
func (e *emoSvc) decompose(s *gateway.Session, req proto.Message) error {
	item, num := req.Int("id"), req.Int("num")
	return e.with(s, func(role *store.Role, st *emoState) error {
		refuse := func(msg string) error { return s.Send(31130, proto.Message{"result": 0, "msg": msg}) }
		fruit, ok := emoCfg.shardOf[item]
		if !ok || num <= 0 || e.fruit(st, fruit).Juexing <= 0 {
			return refuse("只有已觉醒果实的碎片可以分解")
		}
		var yield []int64
		_ = json.Unmarshal(emoCfg.Const["emo_fenjie_zi"], &yield)
		if fruit < 100 { // the orange fruits
			_ = json.Unmarshal(emoCfg.Const["emo_fenjie_cheng"], &yield)
		}
		if len(yield) != 2 {
			return refuse("无法分解")
		}
		ga, gi := map[int64]int64{}, map[int64]int64{}
		splitGains(guildGains([][2]int64{{yield[0], yield[1] * num}}), 1, ga, gi)
		if err := e.st.Exchange(role.RID, map[int64]int64{}, map[int64]int64{item: num}, ga, gi); errors.Is(err, store.ErrConflict) {
			return refuse("碎片不足")
		} else if err != nil {
			return err
		}
		if err := e.refresh(s, role); err != nil {
			return err
		}
		return s.Send(31130, proto.Message{"result": 1, "msg": "分解成功"})
	})
}
