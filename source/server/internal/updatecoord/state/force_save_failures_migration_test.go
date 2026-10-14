package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func forceLegacy4(t *testing.T, root string) string {
	t.Helper()
	path := forceLegacy3(t, root)
	db := rawDB(t, path)
	defer db.Close()
	if _, e := db.Exec(schema4AddendumSQL + `; PRAGMA user_version=4; UPDATE state_meta SET value='4' WHERE key='schema_version';`); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestForceSaveFailuresMigrationPreservesVersion4AndRollsBack(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			root := t.TempDir()
			path := forceLegacy4(t, root)
			db := rawDB(t, path)
			boot := strings.Repeat("a", 64)
			if _, e := db.Exec(`INSERT INTO force_agent_sessions VALUES('test-install',?,123)`, boot); e != nil {
				t.Fatal(e)
			}
			if _, e := db.Exec(`INSERT INTO force_stop_intents VALUES(1,'test-install',?,123,1000,2000,1,1)`, boot); e != nil {
				t.Fatal(e)
			}
			var before string
			if e := db.QueryRow(`SELECT journal_json FROM activation_journals`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			db.Close()
			if fault {
				migrationFault = func() error { return errors.New("injected save-failures-table migration failure") }
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
			var agentBoot, intentBoot, intentInstall string
			var agentPID, intentPID, issued, expires, consumed, confirmed int64
			if e = db.QueryRow(`SELECT boot_id,pid FROM force_agent_sessions WHERE install_id='test-install'`).Scan(&agentBoot, &agentPID); e != nil || agentBoot != boot || agentPID != 123 {
				t.Fatal("agent changed", e)
			}
			if e = db.QueryRow(`SELECT install_id,boot_id,pid,issued_ms,expires_ms,consumed,confirmed FROM force_stop_intents WHERE op_id=1`).Scan(&intentInstall, &intentBoot, &intentPID, &issued, &expires, &consumed, &confirmed); e != nil || intentInstall != "test-install" || intentBoot != boot || intentPID != 123 || issued != 1000 || expires != 2000 || consumed != 1 || confirmed != 1 {
				t.Fatal("original intent changed", e)
			}
			var version int64
			if e = db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil {
				t.Fatal(e)
			}
			want := int64(SchemaVersion)
			if fault {
				want = 4
			}
			if version != want {
				t.Fatal("partial/wrong migration", version)
			}
			if e = validateExistingDB(context.Background(), db, "test-install"); e != nil {
				t.Fatal(e)
			}
			var count int
			if e = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='force_save_failures'`).Scan(&count); e != nil {
				t.Fatal(e)
			}
			expected := 1
			if fault {
				expected = 0
			}
			if count != expected {
				t.Fatal("partial save-failures schema", count)
			}
		})
	}
}

func TestForceSaveMigrationRefusesAlteredVersion4(t *testing.T) {
	for _, alter := range []string{
		`DROP TABLE force_stop_intents`,
		`CREATE TABLE foreign_commands(command TEXT)`,
		`PRAGMA user_version=99; UPDATE state_meta SET value='99' WHERE key='schema_version'`,
	} {
		t.Run(alter, func(t *testing.T) {
			root := t.TempDir()
			path := forceLegacy4(t, root)
			db := rawDB(t, path)
			if _, e := db.Exec(alter); e != nil {
				t.Fatal(e)
			}
			db.Close()
			s, e := Open(root, "test-install")
			if e == nil {
				s.Close()
				t.Fatal("altered/future schema migrated")
			}
			db = rawDB(t, path)
			defer db.Close()
			var count int
			if e = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='force_save_failures'`).Scan(&count); e != nil || count != 0 {
				t.Fatal("refusal mutated schema", count, e)
			}
		})
	}
}
