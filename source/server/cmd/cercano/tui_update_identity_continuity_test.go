package main

// Identity-continuity coverage for startInstalledTUINotices and the bound
// watch/sync path. These tests repro the concrete cross-install bug against
// REAL fixture data (a fully enrolled installation built by
// installcontexttest, verified through the real constructor): without the
// expected-record binding the observer re-discovers the on-disk enrollment
// and can bind a different installation and emit for it. With the binding
// the observer refuses a mismatched record outright (startup), never rebinds
// a running watch (loop), and the bound sync refuses a foreign record before
// any state read. No network, no live installation, no real agent startup.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/server"
	"cercano/source/server/internal/updatecoord/enrollment"
)

// writeForeignRecord replaces the on-disk enrollment beside the verified
// stable launcher with a valid record for a DIFFERENT install ID — a real
// second installation that a stale startup context must not rebind to.
func writeForeignRecord(t *testing.T, installRoot string, verified enrollment.Record) enrollment.Record {
	t.Helper()
	foreign := verified
	foreign.InstallID = strings.Repeat("f", 32)
	data, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, enrollment.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return foreign
}

// foreignStateMirror clones the expected installation's state directory under
// the foreign install ID, so a wrongly bound observer that syncs the foreign
// identity reaches a real state read that fails (the selection belongs to
// another installation), producing a diagnostic. Correct behavior exits the
// watch on mismatch before any state access and produces no diagnostic.
func foreignStateMirror(t *testing.T, stateRoot, expectedID, foreignID string) {
	t.Helper()
	org := "Cercano" // darwin and windows; matches state.StatePath
	if runtime.GOOS == "linux" {
		org = "cercano"
	}
	src := filepath.Join(stateRoot, org, "updater", expectedID)
	dst := filepath.Join(stateRoot, org, "updater", foreignID)
	if err := copyDir(src, dst); err != nil {
		t.Fatal(err)
	}
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(d, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func logCount(snapshot func() string, needle string) int {
	return strings.Count(snapshot(), needle)
}

func waitForLog(t *testing.T, snapshot func() string, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if strings.Contains(snapshot(), needle) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected diagnostic %q never appeared", needle)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

const diagnosticLine = "installed TUI notification unavailable"

// TestTUIUpdateIdentityContinuityStartupMismatchRefuses repros the concrete
// gap: the on-disk enrollment changed between verification and observation
// (a foreign install ID sits beside the verified stable launcher). The
// observer must refuse before binding, before force-owner creation and
// before any state access — a zero server panics on any hub use, and a
// wrongly bound watch with the mirrored foreign state would diagnose.
func TestTUIUpdateIdentityContinuityStartupMismatchRefuses(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	foreign := writeForeignRecord(t, install.Root, verified.Record())
	foreignStateMirror(t, install.StateRoot, verified.Record().InstallID, foreign.InstallID)

	// Install the log capture before the observer starts, so a wrongly
	// bound watch cannot slip its diagnostic past it.
	snapshot := captureLogs(t)
	stop := startInstalledTUINotices(verified, &server.Server{})
	if stop == nil {
		t.Fatal("observer returned no stop function for a mismatched enrollment")
	}
	defer stop()

	// One full probe interval plus margin: a wrongly bound watch syncs the
	// mirrored foreign state and diagnoses; correct refusal logs nothing.
	time.Sleep(6 * time.Second)
	if out := snapshot(); out != "" {
		t.Fatalf("observer bound or diagnosed a mismatched foreign enrollment: %q", out)
	}
}

// TestTUIUpdateIdentityContinuityWatchLoopNeverRebinds binds the observer to
// the verified record, then swaps the on-disk enrollment for a foreign one
// WITH a mirrored state chain while the watch runs. The loop must exit on
// the mismatched probe instead of rebinding and syncing the foreign
// installation.
func TestTUIUpdateIdentityContinuityWatchLoopNeverRebinds(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	snapshot := captureLogs(t)
	srv := &server.Server{}
	stop := startInstalledTUINotices(verified, srv)
	if stop == nil {
		t.Fatal("observer returned no stop function for a verified installation")
	}
	defer stop()

	// Binding confirmed: the first sync of the real state against a zero
	// server fails closed through the diagnostic-only path.
	waitForLog(t, snapshot, diagnosticLine, 10*time.Second)
	if n := logCount(snapshot, diagnosticLine); n != 1 {
		t.Fatalf("expected exactly one diagnostic after binding, got %d", n)
	}

	// Swap the enrollment for a foreign installation with a mirrored state
	// chain: a rebound loop would diagnose again (foreign selection belongs
	// to another installation); the correct loop exits silently.
	foreign := writeForeignRecord(t, install.Root, verified.Record())
	foreignStateMirror(t, install.StateRoot, verified.Record().InstallID, foreign.InstallID)

	time.Sleep(6 * time.Second)
	if n := logCount(snapshot, diagnosticLine); n != 1 {
		t.Fatalf("watch rebound to the changed foreign installation (%d diagnostics)", n)
	}
}

// TestTUIUpdateIdentityContinuityBoundSyncUsesFixedRecord covers the bound
// sync directly: it verifies the discovered enrollment against the expected
// record BEFORE opening state (refusal on mismatch), and with a matching
// record proceeds into the state read (a zero server fails closed at the
// unavailable event hub, never with a mismatch).
func TestTUIUpdateIdentityContinuityBoundSyncUsesFixedRecord(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	writeForeignRecord(t, install.Root, verified.Record())
	expected := verified.Record()
	srv := &server.Server{}
	launcher := filepath.Join(expected.Root, expected.Agent)

	// Mismatch: refusal error, nothing emitted.
	ok, err := srv.SyncBoundEnrolledTUIUpdate(context.Background(), launcher, install.StateRoot, &expected)
	if ok || err == nil {
		t.Fatal("bound sync accepted a foreign enrollment record", ok, err)
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected a mismatch refusal, got %v", err)
	}

	// Restore the matching record: the bound sync proceeds past identity
	// verification into the state read (zero server: source unavailable —
	// NOT a mismatch), proving the fixed record was used.
	data, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(install.Root, enrollment.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err = srv.SyncBoundEnrolledTUIUpdate(context.Background(), launcher, install.StateRoot, &expected)
	if ok {
		t.Fatal("zero server must not emit")
	}
	if err == nil || !strings.Contains(err.Error(), "unavailable") || strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("matching record did not proceed to state read: ok=%v err=%v", ok, err)
	}

	// The no-context sync wrapper is unchanged: same state, absent expected
	// record, still discovers and reads normally.
	ok, err = srv.SyncEnrolledTUIUpdate(context.Background(), launcher, install.StateRoot)
	if ok {
		t.Fatal("zero server must not emit via the unbound wrapper")
	}
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("no-context sync behavior changed: ok=%v err=%v", ok, err)
	}
}
