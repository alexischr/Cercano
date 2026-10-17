package telemetry

import (
	"path/filepath"
	"testing"
	"time"
)

// A second InitializeAccounting pass only reads the existing schema (the
// CREATE IF NOT EXISTS statements are no-ops) and then upgrades to a write
// for the metadata inserts. With a deferred transaction, a legacy writer
// committing between that read and the upgrade aborts the upgrade
// immediately with SQLITE_BUSY before busy_timeout is consulted, which is
// the TestCollectorOwnsAccountingLane flake. The initializer must reserve
// the writer first (BEGIN IMMEDIATE) and wait for the competing writer to
// finish instead of failing immediately.
func TestAccountingInitializeWaitsForWriterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	primary, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	if err = primary.InitializeAccounting(t.Context()); err != nil {
		t.Fatal(err)
	}
	blocker, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	tx, err := blocker.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(t.Context(), `INSERT INTO events (timestamp, tool_name, model, input_tokens, output_tokens, duration_ms, was_escalated, cloud_provider, cloud_model, token_saving, session_id, content_tokens_avoided) VALUES ('2026-01-01','wait','fake',0,0,0,0,'','',1,'',0)`); err != nil {
		t.Fatal(err)
	}
	initialized := make(chan error, 1)
	go func() { initialized <- primary.InitializeAccounting(t.Context()) }()
	// The pre-fix deferred transaction failed within microseconds; a short
	// quiet window proves the initializer is waiting on the writer lock.
	select {
	case err := <-initialized:
		t.Fatalf("initializer did not wait for writer: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-initialized:
		if err != nil {
			t.Fatalf("initializer failed after writer release: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("initializer never finished after writer release")
	}
	if _, err = primary.AccountingAttempt(t.Context(), "absent"); err == nil {
		t.Fatal("unexpected attempt row")
	}
}
