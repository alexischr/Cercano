package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func forceLegacy3(t *testing.T, root string) string {
	t.Helper()
	path := legacyV2Database(t, root, "test-install")
	db := rawDB(t, path)
	defer db.Close()
	if _, e := db.Exec(schema3AddendumSQL + `; PRAGMA user_version=3; UPDATE state_meta SET value='3' WHERE key='schema_version';`); e != nil {
		t.Fatal(e)
	}
	j := journalFor(1, "1.2.3")
	b, _ := json.Marshal(j)
	if _, e := db.Exec(`INSERT INTO activation_journals(op_id,install_id,revision,journal_json) VALUES(1,'test-install',1,?)`, string(b)); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestForceControlMigrationPreservesVersion3AndRollsBack(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			root := t.TempDir()
			path := forceLegacy3(t, root)
			db := rawDB(t, path)
			var before string
			if e := db.QueryRow(`SELECT journal_json FROM activation_journals`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			db.Close()
			if fault {
				migrationFault = func() error { return errors.New("injected force-table migration failure") }
				t.Cleanup(func() { migrationFault = nil })
			}
			s, e := Open(root, "test-install")
			if fault {
				if e == nil {
					s.Close()
					t.Fatal("fault ignored")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				s.Close()
			}
			db = rawDB(t, path)
			defer db.Close()
			var after string
			if e = db.QueryRow(`SELECT journal_json FROM activation_journals`).Scan(&after); e != nil || after != before {
				t.Fatal("journal changed", e)
			}
			var version int64
			if e = db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil {
				t.Fatal(e)
			}
			want := int64(SchemaVersion)
			if fault {
				want = 3
			}
			if version != want {
				t.Fatal("partial/wrong migration", version)
			}
			if e = validateExistingDB(context.Background(), db, "test-install"); e != nil {
				t.Fatal(e)
			}
			var count int
			if e = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('force_agent_sessions','force_stop_intents')`).Scan(&count); e != nil {
				t.Fatal(e)
			}
			expected := 2
			if fault {
				expected = 0
			}
			if count != expected {
				t.Fatal("partial force schema", count)
			}
		})
	}
}
