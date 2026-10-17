package forcesave_test

// DiscoverVerified coverage. The verified context can only be minted by
// installcontext's verification constructor (opaque, unexported fields), so
// these tests build a real enrolled installation with the shared
// installcontexttest fixture — the same throwaway enrolled installation the
// startup-gate tests use — and verify through the real path. No network, no
// live installation, no migration, no agent launch.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/state"
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

// verifiedFixture builds a real enrolled installation whose selected members
// are byte-for-byte copies of this test binary, then verifies the agent
// member through the real constructor, returning the fixture and the
// verified context.
func verifiedFixture(t *testing.T) (*installcontexttest.Install, installcontext.Verified) {
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

func TestDiscoverVerifiedRefusesZeroVerified(t *testing.T) {
	// The zero verified context carries no proof; it must never mint a
	// local decision surface.
	if l, err := forcesave.DiscoverVerified(context.Background(), installcontext.Verified{}); err == nil || l != nil {
		t.Fatalf("zero verified context accepted: local=%v err=%v", l, err)
	}
}

func TestDiscoverVerifiedSucceedsOnVerifiedTemporaryInstallation(t *testing.T) {
	ctx := context.Background()
	install, verified := verifiedFixture(t)
	fixtureEnv(t, install.Home)

	l, err := forcesave.DiscoverVerified(ctx, verified)
	if err != nil || l == nil {
		t.Fatalf("verified installation refused: local=%v err=%v", l, err)
	}
	// The verified role's stable launcher was derived from the verified
	// record, and the protected state opens read-only without error.
	if _, _, err = l.Pending(ctx); err != nil {
		t.Fatal("verified installation could not read its protected state:", err)
	}
}

func TestDiscoverVerifiedRefusesChangedEnrollment(t *testing.T) {
	ctx := context.Background()
	install, verified := verifiedFixture(t)
	fixtureEnv(t, install.Home)

	if _, err := forcesave.DiscoverVerified(ctx, verified); err != nil {
		t.Fatal("pre-tamper revalidation failed:", err)
	}
	// Replace the on-disk record with a valid record for a different install
	// ID (same root binding). Exact revalidation must refuse the verified
	// context: the enrollment changed after verification.
	changed := verified.Record()
	changed.InstallID = strings.Repeat("b", 32)
	data, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(install.Root, enrollment.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = forcesave.DiscoverVerified(ctx, verified); err == nil {
		t.Fatal("changed enrollment record accepted after exact revalidation")
	}
}

func TestDiscoverVerifiedDoesNotCreateMissingState(t *testing.T) {
	ctx := context.Background()
	_, verified := verifiedFixture(t)

	// Point the existing per-user state-root derivation at an empty home:
	// discovery itself touches no state, but any use must refuse without
	// provisioning anything.
	home := t.TempDir()
	fixtureEnv(t, home)
	l, err := forcesave.DiscoverVerified(ctx, verified)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := l.Pending(ctx); err == nil {
		t.Fatalf("missing state accepted (ok=%t)", ok)
	}
	root, err := state.StateRoot(runtime.GOOS, home, filepath.Join(home, "localappdata"), filepath.Join(home, "xdg-state"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("missing state was provisioned:", err)
	}
}
