package store

// HorcruxModule is the module_state name of a role's horcruxes (魂器).
const HorcruxModule = "horcrux"

// HorcruxState is the horcrux of one hero: Status 0 not awakened, 1 awakened, 2 at the top level; Lev is the strengthening level.
type HorcruxState struct{ Status, Lev int64 }

// HorcruxDoc is the module state: the horcrux of each hero by partner id.
type HorcruxDoc struct{ Heroes map[int64]HorcruxState }

// HorcruxOf loads a role's horcruxes.
func (s *Store) HorcruxOf(rid int64) (*HorcruxDoc, error) {
	d := &HorcruxDoc{}
	if _, err := s.LoadState(rid, HorcruxModule, d); err != nil {
		return nil, err
	}
	if d.Heroes == nil {
		d.Heroes = map[int64]HorcruxState{}
	}
	return d, nil
}

// SaveHorcrux writes a role's horcruxes.
func (s *Store) SaveHorcrux(rid int64, d *HorcruxDoc) error {
	return s.SaveState(rid, HorcruxModule, d)
}

// EmpowerModule is the module_state name of a role's empowered heroes (赋能).
const EmpowerModule = "empower"

// EmpowerDoc: the heroes made by empowerment with the time each ends, and the day's count.
type EmpowerDoc struct {
	Heroes map[int64]int64
	Day    int64
	Used   int64
}

// EmpowerOf loads a role's empowerment state.
func (s *Store) EmpowerOf(rid int64) (*EmpowerDoc, error) {
	d := &EmpowerDoc{}
	if _, err := s.LoadState(rid, EmpowerModule, d); err != nil {
		return nil, err
	}
	if d.Heroes == nil {
		d.Heroes = map[int64]int64{}
	}
	return d, nil
}

// SaveEmpower writes a role's empowerment state.
func (s *Store) SaveEmpower(rid int64, d *EmpowerDoc) error {
	return s.SaveState(rid, EmpowerModule, d)
}

// GrantEmpowered pays and adds one hero with the given star, level and break step and talents, in one transaction; the hero is locked
// (it cannot be eaten by an ascend or an offering).
func (s *Store) GrantEmpowered(rid, bid int64, star, lev, brk int, talents []Talent, spendA, spendI map[int64]int64) (*Partner, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := spendTx(tx, rid, spendA, spendI); err != nil {
		return nil, err
	}
	res, err := tx.Exec(`INSERT INTO partners(rid,bid,lev,star,break_lev,locked,created_at) VALUES(?,?,?,?,?,1,?)`, rid, bid, lev, star, brk, s.unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	for _, t := range talents {
		if _, err := tx.Exec(`INSERT INTO partner_talents(partner_id,pos,skill_id) VALUES(?,?,?)`, id, t.Pos, t.SkillID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Partner{PartnerID: id, RID: rid, BID: bid, Lev: lev, Star: star, BreakLev: brk, Locked: true, Talents: talents}, nil
}
