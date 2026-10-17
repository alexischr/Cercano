package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/server"
	"cercano/source/server/internal/updatecoord/enrollment"
)

// TestTUIUpdateIdentityContinuityGap demonstrates the concrete identity continuity gap:
// Current implementation validates current==verified.Record then goroutine calls WatchEnrolledTUIUpdates;
// internal/server/tui_update_watch.go rediscovers enrolled without expected binding, so changed enrollment
// between accepts foreign installation. Also loop calls SyncEnrolledTUIUpdate which rediscovers again.
func TestTUIUpdateIdentityContinuityGap(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	// Create a different valid enrollment record (same root, different install ID)
	foreignRecord := verified.Record()
	foreignRecord.InstallID = strings.Repeat("f", 32) // Different but valid install ID
	
	// Write the foreign record beside the launcher
	foreignData, err := json.Marshal(foreignRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install.Root, enrollment.FileName), foreignData, 0o600); err != nil {
		t.Fatal(err)
	}

	// Current implementation: startInstalledTUINotices validates current==verified.Record
	// This should fail because current != verified.Record
	snapshot := captureLogs(t)
	stop := startInstalledTUINotices(verified, &server.Server{})
	if stop == nil {
		t.Fatal("observer returned no stop function for mismatched enrollment")
	}
	defer stop()

	// Wait to see if the observer incorrectly binds to the foreign installation
	deadline := time.Now().Add(1 * time.Second)
	for {
		logOutput := snapshot()
		if strings.Contains(logOutput, "installed TUI notification unavailable") {
			// This indicates the observer bound and started observing - this is the bug!
			t.Fatal("observer incorrectly bound to mismatched foreign installation")
		}
		if time.Now().After(deadline) {
			// Good: observer refused to bind to mismatched enrollment
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTUIUpdateIdentityContinuityGapWatchLoop demonstrates the gap in the watch loop:
// WatchEnrolledTUIUpdates rediscovers enrolled without expected binding, so changed
// enrollment between accepts foreign installation.
func TestTUIUpdateIdentityContinuityGapWatchLoop(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	// Start with correct enrollment
	snapshot := captureLogs(t)
	srv := server.NewServer(nil, nil, nil, nil, nil)
	stop := startInstalledTUINotices(verified, srv)
	if stop == nil {
		t.Fatal("observer returned no stop function for correct enrollment")
	}

	// Wait for observer to bind
	deadline := time.Now().Add(3 * time.Second)
	for {
		logOutput := snapshot()
		if strings.Contains(logOutput, "installed TUI notification unavailable") {
			break // bound successfully
		}
		if time.Now().After(deadline) {
			t.Fatal("observer never bound to the verified stable launcher")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Now change the enrollment on disk while the observer is running
	foreignRecord := verified.Record()
	foreignRecord.InstallID = strings.Repeat("w", 32) // Different install ID
	foreignData, err := json.Marshal(foreignRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install.Root, enrollment.FileName), foreignData, 0o600); err != nil {
		t.Fatal(err)
	}

	// The watch loop should NOT rebind to the foreign installation
	// Give it time to potentially misbehave
	time.Sleep(500 * time.Millisecond)
	
	// The observer should still be running and not have rebound
	// If it rebound, we'd see "installed TUI notification unavailable" again
	// or the process would crash if it tried to use the server with wrong install ID
	logOutput := snapshot()
	if strings.Count(logOutput, "installed TUI notification unavailable") > 1 {
		t.Fatal("watch loop incorrectly rebound to changed foreign installation")
	}
}

// TestTUIUpdateIdentityContinuityGapSync demonstrates the gap in SyncEnrolledTUIUpdate:
// It rediscovers enrollment without expected binding.
func TestTUIUpdateIdentityContinuityGapSync(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	// Create a server with minimal setup to test SyncEnrolledTUIUpdate
	srv := &server.Server{}

	// Test SyncEnrolledTUIUpdate with mismatched enrollment
	// This should fail because it rediscovers enrollment without expected binding
	foreignRecord := verified.Record()
	foreignRecord.InstallID = strings.Repeat("s", 32) // Different install ID
	
	// Write foreign record but use verified launcher path
	foreignData, err := json.Marshal(foreignRecord)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install.Root, enrollment.FileName), foreignData, 0o600); err != nil {
		t.Fatal(err)
	}

	// SyncEnrolledTUIUpdate should discover the foreign record and fail
	// because it doesn't match what we expect (verified.Record())
	ok, err := srv.SyncEnrolledTUIUpdate(context.Background(), verified.Record().Agent, install.StateRoot)
	if ok || err == nil {
		t.Fatal("SyncEnrolledTUIUpdate incorrectly succeeded with mismatched enrollment")
	}
}