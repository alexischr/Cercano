package main

// Behavioral coverage for startInstalledTUINotices' verified-context
// binding. The verified context can only be minted by installcontext's
// verification constructor (opaque, unexported fields), so these tests build
// a real enrolled installation with the shared installcontexttest fixture —
// the same throwaway enrolled installation the startup-gate tests use — and
// verify the AGENT member through the real path. No network, no live
// installation, no migration, no real agent startup.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cercano/source/server/internal/server"
	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/pkg/installcontext"
	"cercano/source/server/pkg/installcontexttest"
)

// fixtureEnv points this process's per-user state-root probes at the fixture
// home, so the observer's existing environment derivation resolves to the
// fixture state root. Same probes as installcontexttest Install.ChildEnv.
func fixtureEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
}

// verifiedAgentFixture builds a real enrolled installation whose selected
// members are byte-for-byte copies of this test binary, then verifies the
// AGENT member through the real constructor, returning the fixture and the
// verified context — the same proof runServerMode hands the observer.
func verifiedAgentFixture(t *testing.T) (*installcontexttest.Install, installcontext.Verified) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	install := installcontexttest.Build(t, exe, exe)
	verified, err := installcontext.VerifyChildVerified(context.Background(), install.Locators(), install.StateRoot, install.AgentExe, false)
	if err != nil {
		t.Fatal(err)
	}
	return install, verified
}

// captureLogs swaps the standard logger for a synchronized buffer and
// returns a snapshot function. The observer reports observer-side
// unavailability through this logger only; agent operation is unaffected.
type syncedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncedLogBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncedLogBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func captureLogs(t *testing.T) func() string {
	t.Helper()
	buf := &syncedLogBuffer{}
	old := log.Writer()
	flags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(old)
		log.SetFlags(flags)
	})
	return buf.String
}

func TestStartInstalledTUINoticesBindsVerifiedStableLauncher(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	snapshot := captureLogs(t)
	// A zero server has no event hub, so the bound watch loop fails closed
	// through its diagnostic-only path instead of affecting anything.
	stop := startInstalledTUINotices(verified, &server.Server{})
	if stop == nil {
		t.Fatal("observer returned no stop function for a verified installation")
	}
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(snapshot(), "installed TUI notification unavailable") {
			return // the watch bound and started observing
		}
		if time.Now().After(deadline) {
			t.Fatal("observer never bound to the verified stable launcher")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStartInstalledTUINoticesStaleStartupRecordRefusesRebind(t *testing.T) {
	install, verified := verifiedAgentFixture(t)
	fixtureEnv(t, install.Home)

	// Replace the on-disk record with a valid record for a different install
	// ID (same root binding): the installation changed after this agent
	// verified. The startup record is stale and must NOT rebind the watch
	// to the new on-disk identity. A nil server panics if the observer
	// wrongly binds (a bound watch dereferences the server), so returning
	// cleanly proves refusal.
	changed := verified.Record()
	changed.InstallID = strings.Repeat("b", 32)
	data, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(install.Root, enrollment.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := captureLogs(t)
	stop := startInstalledTUINotices(verified, nil)
	if stop == nil {
		t.Fatal("observer returned no stop function for a refused observer")
	}
	stop()
	// Give a wrongly bound watch loop ample time to reach its first server
	// dereference and crash this process; absence proves refusal.
	time.Sleep(200 * time.Millisecond)
	if strings.Contains(snapshot(), "installed TUI notification unavailable") {
		t.Fatal("stale startup record rebound the observer to the changed installation")
	}
}

func TestStartInstalledTUINoticesAbsentVerifiedUnchanged(t *testing.T) {
	// No verified context (agent not launched as a verified installed
	// member — no locators): unchanged pre-verification behavior. The
	// running test binary has no adjacent enrollment, so the optional
	// observer does nothing and returns a stop function without touching
	// the server.
	snapshot := captureLogs(t)
	stop := startInstalledTUINotices(installcontext.Verified{}, nil)
	if stop == nil {
		t.Fatal("observer returned no stop function without a verified context")
	}
	stop()
	time.Sleep(100 * time.Millisecond)
	if out := snapshot(); out != "" {
		t.Fatalf("no-context observer produced diagnostics %q", out)
	}
}
