package game

import (
	"2droundserver/internal/gateway"
	"2droundserver/internal/proto"
	"2droundserver/internal/store"
)

type rolePowerState struct {
	Max int64 `json:"max"`
}

// sendRolePower retains the observed high-water mark when equipment is removed.
// previous includes the pre-operation power when tracking an existing role for
// the first time. The power formula itself remains the existing local model.
func sendRolePower(s *gateway.Session, st *store.Store, rid, previous int64) error {
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return err
	}
	power := rolePower(heroes)
	state := rolePowerState{}
	if _, err := st.LoadState(rid, "role_power", &state); err != nil {
		return err
	}
	state.Max = max(state.Max, previous, power)
	if err := st.SaveState(rid, "role_power", &state); err != nil {
		return err
	}
	return s.Send(10306, proto.Message{"power": power, "max_power": state.Max})
}

func init() {
	registerAdminModule(AdminModule{Key: "role_power", Title: "角色历史战力", Window: "10306", Scope: "role", New: adminState[rolePowerState]})
}
