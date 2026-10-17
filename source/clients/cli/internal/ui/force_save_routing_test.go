package ui

// Routing coverage for the verified startup context in the force-save
// startup path. The verified context can only be minted by installcontext's
// verification constructor (opaque, unexported fields), so these tests build
// a real enrolled installation with the shared installcontexttest fixture —
// the same throwaway enrolled installation the startup-gate tests use — and
// verify the CLIENT member through the real path. No network, no live
// installation, no migration, no agent launch.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cercano/source/server/pkg/forcesave"
	"cercano/source/server/pkg/installcontext"
	"cercano/source/server/pkg/installcontexttest"
)

// fixtureEnv points this process's per-user state-root probes at the fixture
// home, so forcesave's existing environment derivation resolves to the
// fixture state root. Same probes as installcontexttest Install.ChildEnv.
func fixtureEnv(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
}

// verifiedClientFixture builds a real enrolled installation whose selected
// members are byte-for-byte copies of this test binary, then verifies the
// CLIENT member through the real constructor, returning the fixture and the
// verified context — the same proof the CLI main hands the UI model.
func verifiedClientFixture(t *testing.T) (*installcontexttest.Install, installcontext.Verified) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	install := installcontexttest.Build(t, exe, exe)
	verified, err := installcontext.VerifyChildVerified(context.Background(), install.Locators(), install.StateRoot, install.ClientExe, true)
	if err != nil {
		t.Fatal(err)
	}
	return install, verified
}

// enrollmentFile locates the fixture's enrollment record file. The builder
// writes it plus the installed member binaries into the root; the record is
// the only JSON file, which locates it without hard-coding its schema name.
func enrollmentFile(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	if len(files) != 1 {
		t.Fatalf("fixture install root %s has %d JSON files (%v), want exactly the enrollment record", root, len(files), files)
	}
	return filepath.Join(root, files[0])
}

func TestDiscoverForceSaveRoutesVerifiedStableLauncher(t *testing.T) {
	ctx := context.Background()
	install, verified := verifiedClientFixture(t)
	fixtureEnv(t, install.Home)

	local, err := discoverForceSaveLocal(ctx, verified)
	if err != nil || local == nil {
		t.Fatalf("verified client context refused by startup discovery: local=%v err=%v", local, err)
	}
	// The stable launcher came from the verified record's client role, and
	// the protected state opens read-only without error.
	if _, _, err = local.Pending(ctx); err != nil {
		t.Fatal("verified client could not read its protected state:", err)
	}
}

func TestDiscoverForceSaveStaleVerifiedRecordRefusesRebind(t *testing.T) {
	ctx := context.Background()
	install, verified := verifiedClientFixture(t)
	fixtureEnv(t, install.Home)

	if _, err := discoverForceSaveLocal(ctx, verified); err != nil {
		t.Fatal("pre-tamper discovery failed:", err)
	}
	// Replace the on-disk record with a valid record for a different install
	// ID (same root binding): the installation changed after this TUI
	// verified. The startup record is stale and must yield NO client rather
	// than silently rebinding to the new on-disk identity.
	changed := verified.Record()
	changed.InstallID = strings.Repeat("b", 32)
	data, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(enrollmentFile(t, install.Root), data, 0o600); err != nil {
		t.Fatal(err)
	}
	local, err := discoverForceSaveLocal(ctx, verified)
	if err == nil || local != nil {
		t.Fatalf("stale startup record rebind accepted: local=%v err=%v", local, err)
	}
	if err != forcesave.ErrUnavailable {
		t.Fatalf("stale refusal error = %v, want %v", err, forcesave.ErrUnavailable)
	}
}

func TestDiscoverForceSaveZeroVerifiedUnchanged(t *testing.T) {
	// No verified context (TUI not launched as a verified installed
	// member): the pre-verification Discover path is unchanged. The running
	// test binary has no adjacent enrollment, so discovery yields no
	// client and no error.
	local, err := discoverForceSaveLocal(context.Background(), installcontext.Verified{})
	if err != nil || local != nil {
		t.Fatalf("absent verified context changed no-context behavior: local=%v err=%v", local, err)
	}
}

func TestModelStoresAndPropagatesVerifiedContext(t *testing.T) {
	_, verified := verifiedClientFixture(t)
	m := Model{}
	if m.verifiedContext.Present() {
		t.Fatal("fresh model unexpectedly holds a verified context")
	}
	m = m.WithVerifiedContext(verified)
	if !m.verifiedContext.Present() || !m.verifiedContext.IsClient() {
		t.Fatal("WithVerifiedContext did not store the verified client context")
	}
	// The stored context is what startup discovery sees — same value, never
	// a re-minted or process-global one.
	if m.verifiedContext.Record() != verified.Record() {
		t.Fatal("stored verified context diverged from the verified record")
	}
}

func TestDiscoverForceSaveCmdAbsentVerifiedYieldsNoClient(t *testing.T) {
	// End-to-end through the startup command: without a verified context
	// the unchanged Discover path runs and yields an empty poll message.
	m := Model{}
	msg, ok := m.discoverForceSaveCmd()().(forceSavePollMsg)
	if !ok {
		t.Fatalf("discoverForceSaveCmd returned %T, want forceSavePollMsg", msg)
	}
	if msg.client != nil {
		t.Fatalf("absent verified context produced a client %v", msg.client)
	}
}
