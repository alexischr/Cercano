package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExistingForceControlDoesNotCreateOrMigrate(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	if c, e := OpenExistingForceControl(ctx, root, "test-install"); e == nil {
		c.Close()
		t.Fatal("missing state opened")
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 0 {
		t.Fatal("control created state", entries, e)
	}
	root, s := readonlyFixture(t)
	path := filepath.Join(s.Directory(), stateFileName)
	s.Close()
	db := rawDB(t, path)
	if _, e = db.Exec(`DROP TABLE force_agent_sessions; DROP TABLE force_stop_intents; PRAGMA user_version=3; UPDATE state_meta SET value='3' WHERE key='schema_version';`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	if c, e := OpenExistingForceControl(ctx, root, "test-install"); !errors.Is(e, ErrReadNeedsUpgrade) {
		if c != nil {
			c.Close()
		}
		t.Fatal(e)
	}
	db = rawDB(t, path)
	defer db.Close()
	var v int
	if e = db.QueryRow(`PRAGMA user_version`).Scan(&v); e != nil || v != 3 {
		t.Fatal("control migrated database", v, e)
	}
}
func TestExistingForceControlAndReadOnlyRefusal(t *testing.T) {
	root, s := readonlyFixture(t)
	s.Close()
	ctx := context.Background()
	control, e := OpenExistingForceControl(ctx, root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer control.Close()
	a, e := NewForceAgent(123)
	if e != nil {
		t.Fatal(e)
	}
	if e = control.RegisterAgent(ctx, a); e != nil {
		t.Fatal(e)
	}
	got, found, e := control.Agent(ctx)
	if e != nil || !found || got != a {
		t.Fatal(got, found, e)
	}
	read, e := OpenExistingReadOnly(ctx, root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer read.Close()
	other, _ := NewForceAgent(124)
	if e = read.RegisterForceAgent(ctx, other); e == nil {
		t.Fatal("reader gained control write authority")
	}
	got, found, e = control.Agent(ctx)
	if e != nil || !found || got != a {
		t.Fatal("refused write changed session", e)
	}
}
