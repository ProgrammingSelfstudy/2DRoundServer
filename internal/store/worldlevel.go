package store

import "time"

const WorldLevelModule = "worldlev"

type WorldLevelOverride struct {
	Override int   `json:"override"`
	Until    int64 `json:"until"`
}
type WorldLevelInfo struct {
	WorldLevelOverride
	Computed  int  `json:"computed"`
	Effective int  `json:"effective"`
	Active    bool `json:"active"`
}

// WorldLevel retains its existing callers and uses the Store clock. Game packets
// use WorldLevelAt with the same Session clock as the rest of their calendars.
func (s *Store) WorldLevel(srv string) (int, error) { return s.WorldLevelAt(srv, s.now()) }
func (s *Store) WorldLevelAt(srv string, now time.Time) (int, error) {
	var o WorldLevelOverride
	if _, err := s.LoadGlobal(srv, WorldLevelModule, &o); err != nil {
		return 0, err
	}
	if o.Override > 0 && (o.Until == 0 || now.Unix() < o.Until) {
		return o.Override, nil
	}
	return s.ComputedWorldLevel(srv)
}
func (s *Store) WorldLevelInfoAt(srv string, now time.Time) (WorldLevelInfo, error) {
	out := WorldLevelInfo{}
	if _, err := s.LoadGlobal(srv, WorldLevelModule, &out.WorldLevelOverride); err != nil {
		return out, err
	}
	computed, err := s.ComputedWorldLevel(srv)
	if err != nil {
		return out, err
	}
	out.Computed, out.Effective = computed, computed
	out.Active = out.Override > 0 && (out.Until == 0 || now.Unix() < out.Until)
	if out.Active {
		out.Effective = out.Override
	}
	return out, nil
}

// SaveWorldLevelOverride is called by the owning gateway while holding its
// world-level lock (and joined to the admin transaction/audit).
func (s *Store) SaveWorldLevelOverride(srv string, value WorldLevelOverride) error {
	return s.SaveGlobal(srv, WorldLevelModule, value)
}
