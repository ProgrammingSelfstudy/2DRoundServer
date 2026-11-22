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
	power, err := rosterPowerOf(st, rid, isH5RoleSession(s))
	if err != nil {
		return err
	}
	return sendRolePowerValue(s, st, rid, previous, power)
}

func rosterPowerOf(st *store.Store, rid int64, h5 bool) (int64, error) {
	heroes, err := st.PartnersOf(rid)
	if err != nil {
		return 0, err
	}
	return rolePower(playablePartners(heroes, h5)), nil
}

func sendRolePowerValue(s *gateway.Session, st *store.Store, rid, previous, power int64) error {
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

// sendPowerIfHigh sends 10306 only when the total power now passes the retained high-water
// mark: the captures of 2026-11-02 and 2026-11-03 sent it after the 7->8 star-up that set a
// new high and not after star-ups that used up a levelled hero. previous is the power before
// the operation.
func sendPowerIfHigh(s *gateway.Session, st *store.Store, rid, previous int64) error {
	power, err := rosterPowerOf(st, rid, isH5RoleSession(s))
	if err != nil {
		return err
	}
	state := rolePowerState{}
	if _, err := st.LoadState(rid, "role_power", &state); err != nil {
		return err
	}
	if power <= max(state.Max, previous) {
		return nil
	}
	return sendRolePower(s, st, rid, previous)
}
