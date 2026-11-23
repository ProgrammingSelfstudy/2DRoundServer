package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestReplayMigrationBackfillsOnlyKnownDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	schema, err := migrationFS.ReadFile("migrations/046_replays.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`CREATE TABLE schema_migrations(name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL);
		INSERT INTO schema_migrations VALUES('046_replays.sql', 1);`); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{`{"rounds":[{}, {}, {}]}`, `{"combat":3}`, `broken JSON`} {
		if _, err := legacy.Exec(`INSERT INTO replays(srv_id,combat_type,a_rid,b_rid,time,meta,data) VALUES('test_1',3,1,2,1,'{}',?)`, script); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for idx, rounds := range []int64{3, 0, 0} {
			replay, err := st.ReplayByID(int64(idx+1), 1)
			if err != nil || replay == nil || replay.RoundCount != rounds || replay.CombatMap != 0 || replay.Stage != 0 {
				st.Close()
				t.Fatalf("legacy replay %d: %+v, %v", idx+1, replay, err)
			}
		}
		st.Close()
	}
}
