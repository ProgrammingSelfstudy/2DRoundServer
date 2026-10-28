package store

// HolyModule is the module_state name of a role's holy equipment (神装).
const HolyModule = "holyeqm"

// HolyAttr is one attribute of a piece: its position among the random ones (0 for a main attribute), the client's attribute id
// and the value.
type HolyAttr struct{ Pos, ID, Val int64 }

// HolyPiece is one holy equipment piece. Like the artifacts, the item ledger counts the pieces in the bag and the piece itself
// lives here.
type HolyPiece struct {
	ID      int64 // 1e9 + serial
	Base    int64
	Main    []HolyAttr // the fixed attributes (attack, hp, speed)
	Rand    []HolyAttr // the random ones, Pos 1..n
	Washes  int64      // 洗练次数
	Partner int64      // the hero wearing it (0 = in the bag)
	Slot    int64      // 1~4 when worn
}

// HolyPlan is one saved set (方案) of pieces.
type HolyPlan struct {
	ID      int64
	Partner int64
	Name    string
	Items   []HolyPlanItem
}

// HolyPlanItem is one piece of a plan and the hero it was taken from.
type HolyPlanItem struct{ Partner, Item int64 }

// HolyDoc is the whole module state.
type HolyDoc struct {
	Serial int64
	Pieces []HolyPiece
	Plans  []HolyPlan
	Opened int64 // plan slots bought beyond the free ones
}

// HolyOf loads a role's holy equipment state.
func (s *Store) HolyOf(rid int64) (*HolyDoc, error) {
	d := &HolyDoc{}
	if _, err := s.LoadState(rid, HolyModule, d); err != nil {
		return nil, err
	}
	return d, nil
}

// SaveHoly writes a role's holy equipment state.
func (s *Store) SaveHoly(rid int64, d *HolyDoc) error { return s.SaveState(rid, HolyModule, d) }
