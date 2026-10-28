package store

// ArtifactModule is the module_state name of a role's artifacts (战技).
const ArtifactModule = "artifact"

// ArtAttr is one random attribute of an artifact: the client's attribute id and its value.
type ArtAttr struct{ ID, Val int64 }

// ArtPiece is one artifact of a role. The item ledger only counts the pieces that sit in the bag (a worn piece is taken
// out of it, like equipment); the piece itself, with its attributes and skills, lives here.
type ArtPiece struct {
	ID       int64     // base*10000 + serial, what the client calls artifact_id
	Base     int64     // 10451~10457
	Star     int64     // the client's enchant
	Attr     []ArtAttr // random attributes (attr)
	Skills   []int64   // skills, by position (extra_k 1, 2, 8)
	Pending  []int64   // skills of a recast that is not saved yet (extra_k 3, 4, 9)
	PendAttr []ArtAttr // attributes of a recast that is not saved yet (extra_attr)
	Partner  int64     // the hero wearing it (0 = in the bag)
	Pos      int64     // 1 or 2 when worn
}

// ArtifactDoc is the whole module state.
type ArtifactDoc struct {
	Next    map[int64]int64 // base -> last serial given
	Pieces  []ArtPiece
	Lucky   int64           // 熔炼值: what failed compositions have gathered
	Recasts map[int64]int64 // base -> recasts since the last guaranteed rare skill
}

// ArtifactsOf loads a role's artifact state.
func (s *Store) ArtifactsOf(rid int64) (*ArtifactDoc, error) {
	d := &ArtifactDoc{}
	if _, err := s.LoadState(rid, ArtifactModule, d); err != nil {
		return nil, err
	}
	if d.Next == nil {
		d.Next = map[int64]int64{}
	}
	if d.Recasts == nil {
		d.Recasts = map[int64]int64{}
	}
	return d, nil
}

// SaveArtifacts writes a role's artifact state.
func (s *Store) SaveArtifacts(rid int64, d *ArtifactDoc) error {
	return s.SaveState(rid, ArtifactModule, d)
}

// attachArtifacts puts the worn artifacts and holy equipment on the heroes of one role.
func (s *Store) attachArtifacts(rid int64, ps []Partner) error {
	d, err := s.ArtifactsOf(rid)
	if err != nil {
		return err
	}
	h, err := s.HolyOf(rid)
	if err != nil {
		return err
	}
	hx, err := s.HorcruxOf(rid)
	if err != nil {
		return err
	}
	em, err := s.EmpowerOf(rid)
	if err != nil {
		return err
	}
	for i := range ps {
		ps[i].EndTime = em.Heroes[ps[i].PartnerID]
		ps[i].Artifacts, ps[i].Holy = nil, nil
		ps[i].Horcrux = hx.Heroes[ps[i].PartnerID]
		for _, a := range d.Pieces {
			if a.Partner == ps[i].PartnerID && a.Pos > 0 {
				ps[i].Artifacts = append(ps[i].Artifacts, a)
			}
		}
		for _, x := range h.Pieces {
			if x.Partner == ps[i].PartnerID && x.Slot > 0 {
				ps[i].Holy = append(ps[i].Holy, x)
			}
		}
	}
	return nil
}
