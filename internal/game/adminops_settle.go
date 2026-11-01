package game

import (
	"2droundserver/internal/store"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// This envelope is constructed by authenticated admin code. The loopback-only
// control port is a trusted service boundary, not an end-user authentication API.
type AdminOperation struct {
	Reason    string `json:"reason"`
	AdminID   int64  `json:"admin_id"`
	AdminName string `json:"admin_name"`
	IP        string `json:"ip"`
}

func validAdminReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && utf8.RuneCountInString(reason) <= 200 && strings.IndexFunc(reason, unicode.IsControl) < 0
}
func readAdminOperation(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		opsWrite(w, nil, &opsError{422, "invalid operation body"})
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		opsWrite(w, nil, &opsError{422, "expected one JSON object"})
		return false
	}
	return true
}
func (op AdminOperation) audit(st *store.Store, action, srv string, detail map[string]any) error {
	detail["reason"] = op.Reason
	name := op.AdminName
	if name == "" {
		name = "gateway"
	}
	return st.WriteAudit(op.AdminID, name, action, srv, detail, op.IP)
}

type adminSettleResult struct {
	Settled bool        `json:"settled"`
	Reason  string      `json:"reason"`
	Season  adminSeason `json:"season"`
}

// settleGlobal is deliberately narrower than a gameplay loader: it never forces
// future time, creates a new champion event, or makes a new guild war. The exact
// owner mutex is also held by the existing timer/request rollover path.
func settleGlobal(st *store.Store, srv, key string, now time.Time, op AdminOperation) (adminSettleResult, error) {
	result := adminSettleResult{}
	m, o, err := globalOwner(st, key)
	if err != nil {
		return result, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	err = st.AdminTransaction(func(tx *store.Store) error {
		raw, found, err := tx.GlobalModule(srv, key)
		if err != nil {
			return err
		}
		state := m.New()
		if found {
			if err = json.Unmarshal(raw, state); err != nil {
				return err
			}
		}
		status, err := globalSeason(tx, srv, state, o.service, now)
		if err != nil {
			return err
		}
		result.Season = status
		if !status.Supported {
			return &opsError{405, "module is read-only; no authorized settlement"}
		}
		result.Reason = "not_due"
		if status.Settled && !status.Due {
			result.Reason = "already"
		}
		if b, ok := state.(*atBoard); ok && !status.Due && b.AdminCatchup == status.Key {
			result.Reason = "already"
		}
		if status.Due {
			if err = runGlobalSettlement(tx, srv, state, o.service, now); err != nil {
				return err
			}
			result.Settled = true
			result.Reason = "settled"
			raw, _, err = tx.GlobalModule(srv, key)
			if err != nil {
				return err
			}
			state = m.New()
			if len(raw) > 0 {
				if err = json.Unmarshal(raw, state); err != nil {
					return err
				}
			}
			result.Season, err = globalSeason(tx, srv, state, o.service, now)
			if err != nil {
				return err
			}
		}
		return op.audit(tx, "server.season.settle", srv, map[string]any{"key": key, "result": result})
	})
	return result, err
}
func runGlobalSettlement(st *store.Store, srv string, state, service any, now time.Time) error {
	switch b := state.(type) {
	case *atBoard:
		live, ok := service.(*atSvc)
		if !ok {
			return &opsError{503, "team arena service unavailable"}
		}
		svc := &atSvc{st: st, hub: live.hub, rng: live.rng}
		updated, err := svc.board(srv, now)
		if err != nil {
			return err
		}
		updated.AdminCatchup, _, _ = atSeason(now)
		return st.SaveGlobal(srv, atModule, updated)
	case *amBoard:
		live, ok := service.(*amSvc)
		if !ok {
			return &opsError{503, "multi arena service unavailable"}
		}
		svc := &amSvc{st: st, at: live.at, rng: live.rng}
		updated, err := svc.board(srv, now)
		if err != nil {
			return err
		}
		key, _ := amSeason(now)
		updated.Settled = key
		return st.SaveGlobal(srv, amModule, updated)
	case *caBoard:
		live, ok := service.(*caSvc)
		if !ok {
			return &opsError{503, "cross arena service unavailable"}
		}
		svc := &caSvc{st: st, rng: live.rng}
		_, err := svc.board(srv, now)
		return err
	case *eliteWorld:
		live, ok := service.(*eliteSvc)
		if !ok {
			return &opsError{503, "elite service unavailable"}
		}
		return (&eliteSvc{st: st, rng: live.rng}).settleSeasons(srv, now)
	case *drWorld:
		if err := b.settle(st, srv); err != nil {
			return err
		}
		return st.SaveGlobal(srv, drModule, b)
	case *gwWar:
		live, ok := service.(*gwSvc)
		if !ok {
			return &opsError{503, "guild war service unavailable"}
		}
		svc := &gwSvc{st: st, fights: live.fights, hub: live.hub, rng: live.rng}
		if err := svc.settleAll(srv, b); err != nil {
			return err
		}
		return st.SaveGlobal(srv, guildWarModule, b)
	case *champState:
		live, ok := service.(*champSvc)
		if !ok {
			return &opsError{503, "champion service unavailable"}
		}
		svc := &champSvc{st: st, hub: live.hub, rng: live.rng, now: live.now, module: champModule, cmds: live.cmds, days: live.days, startHour: live.startHour, startMin: live.startMin, awards: live.awards, entrants: live.entrants, cross: live.cross}
		if live.cross {
			svc.module = "crosschamp"
		}
		if _, err := svc.advance(srv, b, live.now()); err != nil {
			return err
		}
		if live.cross {
			return st.SaveGlobal(srv, "crosschamp", b)
		}
		return st.SaveGlobal(srv, champModule, b)
	}
	return &opsError{405, "module is read-only; no authorized settlement"}
}
