package game

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strconv"

	"2droundserver/internal/store"
)

//go:embed data/combat_speed.json
var combatSpeedJSON []byte

// Flat and Per are the two original attributes 106 and 129. Level and Caster
// convert effective hero level or the caster's speed into flat points, per mille.
type speedBonus struct {
	Flat, Per, Level, Caster int64
}

func (a speedBonus) plus(b speedBonus) speedBonus {
	return speedBonus{Flat: a.Flat + b.Flat, Per: a.Per + b.Per, Level: a.Level + b.Level, Caster: a.Caster + b.Caster}
}
func (a speedBonus) apply(base, level int64) int64 {
	return max(0, (base+a.Flat+level*a.Level/1000)*max(0, 1000+a.Per)/1000)
}

type nativeStatus struct {
	Stats                                 speedBonus
	Group, Rank                           int64
	Type, Rounds, Cap                     int
	Keep, Negative                        bool
	Atk, Crit, Mitigation, Damage, Marine int64
}

var initiative = func() (c struct {
	Male                  map[int64]bool
	Skills, Skins, Titles map[int64]speedBonus
	First                 map[int64]bool
	Refs                  map[int64][]int64
	Slots                 map[string]int64
	Sets                  map[int64]map[int]speedBonus
	ItemSets              map[int64]int64 `json:"item_sets"`
	Fruits                map[string]map[string]speedBonus
	Buffs                 map[int64]nativeStatus
}) {
	if err := json.Unmarshal(combatSpeedJSON, &c); err != nil {
		panic(err)
	}
	return
}()

func skillSpeed(skills []int64) (total speedBonus) {
	for _, id := range skills {
		total = total.plus(initiative.Skills[id])
	}
	return
}

// A learned talent and the identical skill engraved on a worn artifact use
// the highest grade of their common group. Preview rolls live in other fields.
func wornSpeed(h store.Partner) (total speedBonus) {
	best := map[int64]int64{}
	consider := func(id int64) {
		if _, ok := initiative.Skills[id]; ok {
			best[id/10] = max(best[id/10], id)
		}
	}
	for _, skill := range h.Talents {
		consider(skill.SkillID)
	}
	for _, art := range h.Artifacts {
		for _, id := range art.Skills {
			consider(id)
		}
	}
	for _, id := range best {
		total = total.plus(initiative.Skills[id])
	}
	count := map[int64]int{}
	for _, p := range h.Equips {
		count[initiative.ItemSets[p.BaseID]]++
	}
	for set, n := range count {
		for need, attr := range initiative.Sets[set] {
			if n >= need {
				total = total.plus(attr)
			}
		}
	}
	// Native HeroModel.getHolyEquipSuitDes sorts each family by star. Each
	// threshold uses its own lowest qualifying piece (second or fourth best).
	holy := map[int64][]int64{}
	for _, p := range h.Holy {
		if set := initiative.ItemSets[p.Base]; set != 0 {
			holy[set/100] = append(holy[set/100], set)
		}
	}
	for _, tiers := range holy {
		slices.Sort(tiers)
		for _, need := range []int{2, 4} {
			if len(tiers) >= need {
				total = total.plus(initiative.Sets[tiers[len(tiers)-need]][need])
			}
		}
	}

	return
}

type campaignTraining struct {
	all   speedBonus
	camp  map[int64]speedBonus
	skins map[int64]bool
}

func (c campaignTraining) forHero(h store.Partner) speedBonus {
	a := c.all.plus(c.camp[int64(partnerTable[h.BID].Camp)])
	if c.skins[h.UseSkin] && skinTable[h.UseSkin].BID == h.BID {
		a = a.plus(initiative.Skins[h.UseSkin])
	}
	return a
}

// Read the current role once per stage. The tree's altar attributes belong to
// the sprite unit. Only the hero help attributes of equipped helpers apply here.
func campaignTrainingOf(st *store.Store, rid int64, form store.Formation, now int64) (c campaignTraining, err error) {
	var halls hwState
	var camps hdState
	var fruits emoState
	var elves elfinState
	if _, err = st.LoadState(rid, hallowsModule, &halls); err != nil {
		return c, err
	}
	if _, err = st.LoadState(rid, halidomModule, &camps); err != nil {
		return c, err
	}
	if _, err = st.LoadState(rid, emoModule, &fruits); err != nil {
		return c, err
	}
	if _, err = st.LoadState(rid, elfinModule, &elves); err != nil {
		return c, err
	}
	if h := halls.hallow(form.HallowsID); h != nil && h.ID > 0 && h.ID <= int64(len(hallowsCfg.SkillAttr)) {
		for _, r := range hallowsCfg.SkillAttr[h.ID-1] {
			if h.Step >= r.Level {
				for _, v := range r.Attr {
					if v.Key == "speed" {
						c.all.Flat += v.Val
					}
				}
			}
		}
	}
	c.camp = map[int64]speedBonus{}
	for i, row := range halidomCfg.Base {
		if v := camps.Halidoms[strconv.FormatInt(row.ID, 10)]; v != nil {
			a := c.camp[row.Camp]
			for _, stat := range hdAttrs(i, v) {
				switch stat.Int("name") {
				case 106:
					a.Flat += stat.Int("val")
				case 129:
					a.Per += stat.Int("val")
				}
			}
			c.camp[row.Camp] = a
		}
	}
	for id, f := range fruits.Fruits {
		if f.Level <= 0 {
			continue
		}
		if f.Juexing > 0 {
			c.all = c.all.plus(initiative.Fruits["juexing"][id])
		}
		c.all = c.all.plus(initiative.Fruits["level"][id+"_"+strconv.FormatInt(f.Level, 10)])
		c.all = c.all.plus(initiative.Fruits["star"][id+"_"+strconv.FormatInt(f.Star, 10)])
	}
	if team := elves.Teams[strconv.FormatInt(form.Type, 10)]["1"]; len(team.Sprites) > 0 {
		bag, err := st.RoleItems(rid)
		if err != nil {
			return c, err
		}
		c.all.Flat += campaignHelperSpeed(elves, team.Sprites, bag)
	}
	owned, err := st.SkinsOf(rid, now)
	if err != nil {
		return c, err
	}
	c.skins = map[int64]bool{}
	for _, skin := range owned {
		c.skins[skin.ID] = true
	}
	prefs, err := st.SettingsOf(rid)
	if err != nil {
		return c, err
	}
	if prefs.TitleID != 0 {
		looks, err := st.LooksOf(rid, store.LookTitle, now)
		if err != nil {
			return c, err
		}
		for _, t := range looks {
			if t.ID == prefs.TitleID {
				c.all = c.all.plus(initiative.Titles[t.ID])
				break
			}
		}
	}
	return c, nil
}

func campaignHelperSpeed(elves elfinState, slots []elfinSlot, owned map[int64]int64) int64 {
	step, _ := elfinCfg.step(elves.Break)
	usedType, usedPos := map[int64]bool{}, map[int64]bool{}
	for _, s := range slots {
		if s.Pos >= 1 && s.Pos <= step.SkillNum && owned[s.Bid] > 0 {
			usedType[elfinCfg.sprite[s.Bid].Type] = true
		}
	}
	var total int64
	for _, s := range slots {
		sp, ok := elfinCfg.sprite[s.Bid]
		if !ok || s.Pos <= elfinTreeSlots || s.Pos > elfinTreeSlots+step.HelpfightNum || owned[s.Bid] < 1 || usedPos[s.Pos] || usedType[sp.Type] {
			continue
		}
		usedPos[s.Pos], usedType[sp.Type] = true, true
		total += sp.Help["speed"]
	}
	return total
}

func (u *fightUnit) addTraining(a speedBonus) {
	u.training = u.training.plus(a)
	u.speed = u.training.apply(u.baseSpeed, u.level)
	u.power = heroPower(u.atk, u.maxHP, u.def, u.speed)
}
