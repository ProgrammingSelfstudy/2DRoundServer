package game

import (
	"encoding/json"
	"fmt"
	"strconv"

	"2droundserver/internal/store"
)

// AdminChargeView separates exact game-window messages (by protocol command)
// from the in-memory state projection and optional explanatory fields. Neither
// projection nor protocol construction is allowed to save state or grant rewards.
type AdminChargeView struct {
	Protocols map[string]any `json:"protocols"`
	State     any            `json:"state"`
	Details   any            `json:"details,omitempty"`
}

func adminChargeState[T any](st *store.Store, id, key string) (*store.Role, *T, error) {
	rid, err := strconv.ParseInt(id, 10, 64)
	if err != nil || rid <= 0 {
		return nil, nil, fmt.Errorf("invalid role id")
	}
	role, err := st.RoleByID(rid)
	if err != nil {
		return nil, nil, err
	}
	raw, exists, err := st.RoleModule(rid, key)
	if err != nil {
		return nil, nil, err
	}
	state := new(T)
	if exists {
		if err := json.Unmarshal(raw, state); err != nil {
			return nil, nil, err
		}
	}
	return role, state, nil
}

// AdminChargeKeys enumerates concrete keys for the role charge summary. Returning
// a new slice prevents callers from changing the first-charge configuration.
func AdminChargeKeys() []string {
	keys := make([]string, 0, len(firstSets)+5)
	for _, fs := range firstSets {
		keys = append(keys, "firstcharge_"+fs.key)
	}
	return append(keys, dayChargeModule, monthCardModule, monthFundModule, limitGiftModule, cycleGiftModule)
}
