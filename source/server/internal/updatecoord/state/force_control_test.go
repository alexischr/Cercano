package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestForceControlSaveFailuresAPI(t *testing.T) {
	root, s := readonlyFixture(t)
	s.Close()
	ctx := context.Background()
	control, e := OpenExistingForceControl(ctx, root, "test-install")
	if e != nil {
		t.Fatal(e)
	}
	defer control.Close()

	// Test that save failures APIs fail on older schema due to missing tables
	a, _ := NewForceAgent(123)
	receiptID := "test-receipt"
	expires := time.Now().Add(10 * time.Second)

	// These should fail because the force_save_failures table doesn't exist in older schemas
	if e := control.PublishSaveFailure(ctx, 1, a, receiptID, expires, time.Now); e == nil {
		t.Fatal("save failure API available on old schema")
	}

	if sf, found, e := control.PendingSaveFailure(ctx, 1); e == nil {
		if found {
			t.Fatal("pending save failure API available on old schema - found record:", sf)
		} else {
			t.Logf("PendingSaveFailure: not found (expected for old schema)")
		}
	} else {
		t.Logf("PendingSaveFailure error: %v", e)
	}

	if e := control.SubmitSaveDecision(ctx, receiptID, 0, false, time.Now); e == nil {
		t.Fatal("submit decision API available on old schema")
	}

	if e := control.ConsumeSaveDecision(ctx, receiptID, a, time.Now); e == nil {
		t.Fatal("consume decision API available on old schema")
	}
}
