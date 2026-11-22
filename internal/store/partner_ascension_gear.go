package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Artifacts and holy equipment have both an instance document and a bag
// balance. Update them in the same transaction as the consumed heroes.
func releaseAscensionGear(tx *sql.Tx, rid int64, consumed []int64) (map[int64]int64, error) {
	refund := map[int64]int64{}
	if len(consumed) == 0 {
		return refund, nil
	}
	gone := make(map[int64]bool, len(consumed))
	for _, id := range consumed {
		gone[id] = true
	}
	load := func(module string, doc any) error {
		var data []byte
		err := tx.QueryRow(`SELECT data FROM module_state WHERE rid=? AND module=?`, rid, module).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(data, doc)
	}
	save := func(module string, doc any) error {
		data, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE module_state SET data=? WHERE rid=? AND module=?`, string(data), rid, module)
		return err
	}
	var artifacts ArtifactDoc
	if err := load(ArtifactModule, &artifacts); err != nil {
		return nil, err
	}
	changed := false
	for i, a := range artifacts.Pieces {
		if gone[a.Partner] && a.Pos > 0 {
			artifacts.Pieces[i].Partner, artifacts.Pieces[i].Pos = 0, 0
			refund[a.Base]++
			changed = true
		}
	}
	if changed {
		if err := save(ArtifactModule, &artifacts); err != nil {
			return nil, err
		}
	}
	var holy HolyDoc
	if err := load(HolyModule, &holy); err != nil {
		return nil, err
	}
	changed = false
	for i, h := range holy.Pieces {
		if gone[h.Partner] && h.Slot > 0 {
			holy.Pieces[i].Partner, holy.Pieces[i].Slot = 0, 0
			refund[h.Base]++
			changed = true
		}
	}
	if changed {
		if err := save(HolyModule, &holy); err != nil {
			return nil, err
		}
	}
	return refund, nil
}
