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
