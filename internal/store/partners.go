package store

import (
	"database/sql"
	"errors"
)

// Partner is one hero of a role.
type Partner struct {
	PartnerID, RID, BID  int64
	Lev, Star, BreakLev  int
	UseSkin              int64        // the skin worn (0 = none)
	Locked               bool         // locked by the owner: not to be consumed
	Equips               []Equip      // worn pieces, by slot
	Talents              []Talent     // learned talent skills, by slot
	ResSlot              int64        // the resonance crystal slot it sits in (0 = none)
	ShownLev, ShownBreak int          // level and break step it shows while it sits in a slot
	Artifacts            []ArtPiece   // worn artifacts (战技), by position
	Holy                 []HolyPiece  // worn holy equipment (神装), by slot
	Horcrux              HorcruxState // the hero's horcrux (魂器)
	EndTime              int64        // when an empowered hero ends (0 = it stays)
	GuildSkillLevels     map[int]int  // ordinary guild skill steps by career for an H5 role
}

// Equip is one worn piece of equipment.
type Equip struct{ Slot, BaseID int64 }

// PartnerSeed is a hero to hand out: base id and star.
type PartnerSeed struct {
	BID  int64
	Star int
}

// PartnersOf lists a role's heroes, oldest first.
func (s *Store) PartnersOf(rid int64) ([]Partner, error) {
	rows, err := s.db.Query(`SELECT partner_id,rid,bid,lev,star,break_lev,locked,use_skin FROM partners WHERE rid=? ORDER BY partner_id`, rid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Partner
	for rows.Next() {
		var p Partner
		if err := rows.Scan(&p.PartnerID, &p.RID, &p.BID, &p.Lev, &p.Star, &p.BreakLev, &p.Locked, &p.UseSkin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	eq, err := s.equipsOf(`partner_id IN (SELECT partner_id FROM partners WHERE rid=?)`, rid)
	if err != nil {
		return nil, err
	}
	tl, err := s.talentsOf(`partner_id IN (SELECT partner_id FROM partners WHERE rid=?)`, rid)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Equips = eq[out[i].PartnerID]
		out[i].Talents = tl[out[i].PartnerID]
	}
	if err := s.attachArtifacts(rid, out); err != nil {
		return nil, err
	}
	if err := s.attachGuildSkills(map[int64][]Partner{rid: out}, "r.rid=?", rid); err != nil {
		return nil, err
	}
	return out, s.applyResonance(rid, out)
}

// GrantStarterPartners gives the role the seeds once: it does nothing when the
// role already got them (roles.partners_init). It reports whether it granted.
func (s *Store) GrantStarterPartners(rid int64, seeds []PartnerSeed) (bool, error) {
	return s.GrantStarterPartnersBy(rid, func(string) []PartnerSeed { return seeds })
}

// GrantStarterPartnersBy is GrantStarterPartners with the seeds chosen by the platform of the role's account; pick runs only when it grants.
func (s *Store) GrantStarterPartnersBy(rid int64, pick func(platform string) []PartnerSeed) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE roles SET partners_init=1 WHERE rid=? AND partners_init=0`, rid)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	var platform string
	if err := tx.QueryRow(`SELECT a.platform FROM roles r JOIN accounts a ON a.id=r.account_id WHERE r.rid=?`, rid).Scan(&platform); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	for _, sd := range pick(platform) {
		if _, err := tx.Exec(`INSERT INTO partners(rid,bid,lev,star,created_at) VALUES(?,?,1,?,?)`, rid, sd.BID, sd.Star, s.unix()); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// StarterGranted reports whether the role has already received its starter heroes.
func (s *Store) StarterGranted(rid int64) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT partners_init FROM roles WHERE rid=?`, rid).Scan(&n); err != nil {
		return false, wrapNoRows(err)
	}
	return n != 0, nil
}

// PartnerByID returns one hero of the role, or ErrNotFound.
func (s *Store) PartnerByID(rid, partnerID int64) (*Partner, error) {
	var p Partner
	err := s.db.QueryRow(`SELECT partner_id,rid,bid,lev,star,break_lev,locked,use_skin FROM partners WHERE partner_id=? AND rid=?`, partnerID, rid).
		Scan(&p.PartnerID, &p.RID, &p.BID, &p.Lev, &p.Star, &p.BreakLev, &p.Locked, &p.UseSkin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	eq, err := s.equipsOf(`partner_id=?`, partnerID)
	p.Equips = eq[partnerID]
	if err != nil {
		return &p, err
	}
	tl, err := s.talentsOf(`partner_id=?`, partnerID)
	p.Talents = tl[partnerID]
	if err != nil {
		return &p, err
	}
	one := []Partner{p}
	if err := s.attachArtifacts(rid, one); err != nil {
		return &p, err
	}
	if slots, err := s.slotsOf(rid); err != nil || slots[partnerID] == 0 {
		if err != nil {
			return &one[0], err
		}
		err = s.attachGuildSkills(map[int64][]Partner{rid: one}, "r.rid=?", rid)
		return &one[0], err
	}
	all, err := s.PartnersOf(rid) // a hero in a slot: the crystal level comes from all of them
	if err != nil {
		return &one[0], err
	}
	for _, q := range all {
		if q.PartnerID == partnerID {
			return &q, nil
		}
	}
	return &one[0], nil
}

// UpgradePartner pays for a hero's new level / break step and stores it, in one
// transaction. It changes nothing and returns ErrConflict when the hero is no
// longer at (fromLev, fromBreak) or the role cannot pay the assets / items.
func (s *Store) UpgradePartner(rid, partnerID int64, fromLev, fromBreak, toLev, toBreak int, assets, items map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE partners SET lev=?, break_lev=? WHERE partner_id=? AND rid=? AND lev=? AND break_lev=?`,
		toLev, toBreak, partnerID, rid, fromLev, fromBreak)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return err
	}
	return tx.Commit()
}

// spendTx takes currencies (by label) and items (by base id) out of a role's
// ledgers, or returns ErrConflict having taken what it could (the caller rolls
// back). It also drops emptied item rows.
func spendTx(tx *sql.Tx, rid int64, assets, items map[int64]int64) error {
	for label, n := range assets {
		res, err := tx.Exec(`UPDATE role_assets SET val=val-? WHERE rid=? AND label=? AND val>=?`, n, rid, label, n)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrConflict
		}
	}
	for id, n := range items {
		res, err := tx.Exec(`UPDATE role_items SET num=num-? WHERE rid=? AND item_id=? AND num>=?`, n, rid, id, n)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrConflict
		}
	}
	if _, err := tx.Exec(`DELETE FROM role_items WHERE rid=? AND num<=0`, rid); err != nil {
		return err
	}
	return recordDaysRankSpending(tx, rid, assets[3], items[72001])
}

// AscendPartner raises a hero's star by one, deletes the consumed heroes, takes the
// cost and pays what the consumed heroes give back, in one transaction. It returns ErrConflict, changing nothing,
// when the hero is no longer at fromStar, a consumed hero is gone (or is the
// hero itself) or the role cannot pay.
func (s *Store) AscendPartner(rid, partnerID int64, fromStar int, consumed []int64, assets, items, gainAssets, gainItems map[int64]int64) error {
	_, err := s.ascendPartner(rid, partnerID, fromStar, consumed, assets, items, gainAssets, gainItems, nil)
	return err
}

// AscendH5Partner verifies the planned heroes and also refunds artifacts and
// holy equipment in the transaction. It returns their counts for the notice.
func (s *Store) AscendH5Partner(rid, partnerID int64, fromStar int, consumed []int64, assets, items, gainAssets, gainItems map[int64]int64, roster map[int64]Partner) (map[int64]int64, error) {
	if _, ok := roster[partnerID]; !ok {
		return nil, ErrConflict
	}
	return s.ascendPartner(rid, partnerID, fromStar, consumed, assets, items, gainAssets, gainItems, roster)
}

func (s *Store) ascendPartner(rid, partnerID int64, fromStar int, consumed []int64, assets, items, gainAssets, gainItems map[int64]int64, roster map[int64]Partner) (map[int64]int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	const notResonating = ` AND NOT EXISTS (SELECT 1 FROM resonance_slots WHERE partner_id=partners.partner_id)`
	update := `UPDATE partners SET star=star+1 WHERE partner_id=? AND rid=? AND star=?`
	updateArgs := []any{partnerID, rid, fromStar}
	if roster != nil {
		update += ` AND bid=?` + notResonating
		updateArgs = append(updateArgs, roster[partnerID].BID)
	}
	res, err := tx.Exec(update, updateArgs...)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrConflict
	}
	for _, id := range consumed {
		if id == partnerID {
			return nil, ErrConflict
		}
		if err := releaseEquipsTx(tx, rid, id); err != nil {
			return nil, err
		}
		del := `DELETE FROM partners WHERE partner_id=? AND rid=?`
		delArgs := []any{id, rid}
		if roster != nil {
			planned := roster[id]
			del += ` AND locked=0 AND bid=? AND star=? AND lev=? AND break_lev=?` + notResonating
			delArgs = append(delArgs, planned.BID, planned.Star, planned.Lev, planned.BreakLev)
		}
		res, err := tx.Exec(del, delArgs...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrConflict
		}
	}
	if err := spendTx(tx, rid, assets, items); err != nil {
		return nil, err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return nil, err
	}
	var worn map[int64]int64
	if roster != nil {
		worn, err = releaseAscensionGear(tx, rid, consumed)
		if err != nil {
			return nil, err
		}
		if err := creditTx(tx, rid, nil, worn); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return worn, nil
}

// FreeSummonUsed reports whether the role already used the group's free summon on that day.
func (s *Store) FreeSummonUsed(rid, group, day int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM summon_free WHERE rid=? AND group_id=? AND day=?`, rid, group, day).Scan(&n)
	return n > 0, err
}

func (s *Store) AdvancedSummonMisses(rid int64) (int, error) {
	var misses int
	err := s.db.QueryRow(`SELECT advanced_summon_misses FROM roles WHERE rid=?`, rid).Scan(&misses)
	return misses, wrapNoRows(err)
}

// SummonResult is what one summon changes: the heroes it hands out, the cost, the
// currencies it pays back and, for a free summon, the group and day to mark used.
type SummonResult struct {
	Seeds              []PartnerSeed
	SpendAssets        map[int64]int64
	SpendItems         map[int64]int64
	GainAssets         map[int64]int64
	GainItems          map[int64]int64
	FreeGroup, FreeDay int64        // FreeGroup 0 = not a free summon
	Elite              *EliteSummon // the elite summon state after the draws, saved with them
	// AdvancedDraw runs after payment inside the write transaction, so simultaneous draws use successive counters.
	AdvancedDraw func(misses int) (seeds []PartnerSeed, gainItems map[int64]int64, next int, err error)
	Capacity     int
}

// Summon applies a summon in one transaction and returns the new heroes in order.
// It returns ErrConflict, changing nothing, when the role cannot pay or the free
// summon was already used that day.
func (s *Store) Summon(rid int64, r SummonResult) ([]Partner, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if r.FreeGroup != 0 {
		if _, err := tx.Exec(`INSERT INTO summon_free(rid,group_id,day) VALUES(?,?,?)`, rid, r.FreeGroup, r.FreeDay); err != nil {
			if isUnique(err) {
				return nil, ErrConflict
			}
			return nil, err
		}
	}
	if err := spendTx(tx, rid, r.SpendAssets, r.SpendItems); err != nil {
		return nil, err
	}
	if r.AdvancedDraw != nil {
		var misses int
		if err := tx.QueryRow(`SELECT advanced_summon_misses FROM roles WHERE rid=?`, rid).Scan(&misses); err != nil {
			return nil, err
		}
		seeds, items, next, err := r.AdvancedDraw(misses)
		if err != nil {
			return nil, err
		}
		r.Seeds, r.GainItems = seeds, items
		if next != misses {
			if _, err := tx.Exec(`UPDATE roles SET advanced_summon_misses=? WHERE rid=?`, next, rid); err != nil {
				return nil, err
			}
		}
	}
	if r.Capacity > 0 && len(r.Seeds) > 0 {
		var owned int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM partners WHERE rid=?`, rid).Scan(&owned); err != nil {
			return nil, err
		}
		if owned+len(r.Seeds) > r.Capacity {
			return nil, ErrHeroCapacity
		}
	}
	if err := creditTx(tx, rid, r.GainAssets, r.GainItems); err != nil {
		return nil, err
	}
	out, err := insertPartnersTx(tx, rid, s.unix(), r.Seeds)
	if err != nil {
		return nil, err
	}
	if r.Elite != nil {
		if err := saveEliteTx(tx, rid, *r.Elite); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// PartnersOfServer lists the heroes of every role of one server, by role id.
func (s *Store) PartnersOfServer(srvID string) (map[int64][]Partner, error) {
	rows, err := s.db.Query(`SELECT p.partner_id,p.rid,p.bid,p.lev,p.star,p.break_lev,p.locked,p.use_skin FROM partners p JOIN roles r ON r.rid=p.rid WHERE r.srv_id=? ORDER BY p.partner_id`, srvID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Partner{}
	for rows.Next() {
		var p Partner
		if err := rows.Scan(&p.PartnerID, &p.RID, &p.BID, &p.Lev, &p.Star, &p.BreakLev, &p.Locked, &p.UseSkin); err != nil {
			return nil, err
		}
		out[p.RID] = append(out[p.RID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	eq, err := s.equipsOf(`partner_id IN (SELECT p.partner_id FROM partners p JOIN roles r ON r.rid=p.rid WHERE r.srv_id=?)`, srvID)
	if err != nil {
		return nil, err
	}
	for rid, ps := range out {
		for i := range ps {
			ps[i].Equips = eq[ps[i].PartnerID]
		}
		out[rid] = ps
	}
	return out, s.attachGuildSkills(out, "r.srv_id=?", srvID)
}

// RankRole is a role with what the rankings sort on.
type RankRole struct {
	RID      int64
	Name     string
	Lev      int
	FaceID   int
	MaxDunID int64 // 0 for a role that never fought
	DramaAt  int64 // when its campaign progress last changed
}

// RankRoles lists the normal roles of one server with their campaign progress.
func (s *Store) RankRoles(srvID string) ([]RankRole, error) {
	rows, err := s.db.Query(`SELECT r.rid,r.name,r.lev,r.face_id,COALESCE(d.max_dun_id,0),COALESCE(d.updated_at,0)
		FROM roles r LEFT JOIN role_drama d ON d.rid=r.rid WHERE r.srv_id=? AND r.status=1`, srvID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankRole
	for rows.Next() {
		var r RankRole
		if err := rows.Scan(&r.RID, &r.Name, &r.Lev, &r.FaceID, &r.MaxDunID, &r.DramaAt); err != nil {
			return nil, err
		}
		r.FaceID = faceOrDefault(r.FaceID)
		out = append(out, r)
	}
	return out, rows.Err()
}

// HeroSlotBuys is how many times the role bought hero slots.
func (s *Store) HeroSlotBuys(rid int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT hero_slot_buys FROM roles WHERE rid=?`, rid).Scan(&n)
	return n, wrapNoRows(err)
}

// BuyHeroSlot counts one more slot purchase and takes its price, in one
// transaction. It returns ErrConflict, changing nothing, when the purchase count is
// no longer `from` (another session bought first) or the role cannot pay.
func (s *Store) BuyHeroSlot(rid int64, from int, spendAssets map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE roles SET hero_slot_buys=hero_slot_buys+1 WHERE rid=? AND hero_slot_buys=?`, rid, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	if err := spendTx(tx, rid, spendAssets, nil); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPartnerLocked locks or unlocks a hero of the role; ErrNotFound for a hero that is not its own.
func (s *Store) SetPartnerLocked(rid, partnerID int64, locked bool) error {
	v := 0
	if locked {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE partners SET locked=? WHERE partner_id=? AND rid=?`, v, partnerID, rid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ConsumePartners deletes heroes (releasing their equipment), takes a cost and pays a reward, in one transaction. It
// returns ErrConflict, changing nothing, when a hero is gone, locked or in a resonance slot, or the role cannot pay.
func (s *Store) ConsumePartners(rid int64, ids []int64, spendAssets, spendItems, gainAssets, gainItems map[int64]int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		var locked bool
		if err := tx.QueryRow(`SELECT locked FROM partners WHERE partner_id=? AND rid=?`, id, rid).Scan(&locked); err != nil {
			return ErrConflict
		}
		if locked {
			return ErrConflict
		}
		if err := releaseEquipsTx(tx, rid, id); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM partners WHERE partner_id=? AND rid=?`, id, rid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrConflict
		}
	}
	if err := spendTx(tx, rid, spendAssets, spendItems); err != nil {
		return err
	}
	if err := creditTx(tx, rid, gainAssets, gainItems); err != nil {
		return err
	}
	return tx.Commit()
}

// PartnerOwner returns the role a hero belongs to (partner ids are unique across roles), or ErrNotFound.
func (s *Store) PartnerOwner(partnerID int64) (int64, error) {
	var rid int64
	err := s.db.QueryRow(`SELECT rid FROM partners WHERE partner_id=?`, partnerID).Scan(&rid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return rid, err
}
