package installcontext

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/state"
)

// DefaultStateRoot derives the per-user updater state base directory exactly
// the way the agent's installed-TUI startup does (cmd/cercano/
// tui_update_notices.go): the home directory plus the platform-specific
// environment probes. It is pure derivation from probes; the locators never
// influence it.
func DefaultStateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("installcontext: home directory: %w", err)
	}
	root, err := state.StateRoot(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getenv("XDG_STATE_HOME"))
	if err != nil {
		return "", fmt.Errorf("installcontext: updater state root: %w", err)
	}
	return root, nil
}

// VerifyChild verifies a versioned child process launched with the approved
// locators against the installation's protected selection of record. It first
// verifies the completed, health-selected installation exactly as before via
// enrollment.VerifyInstalledChild (which is reused unchanged); fallback to the
// selected-candidate verifier happens ONLY on that completed lookup's normal
// not-found, and then only through enrollment.VerifySelectedChild's exact
// gate: the current operation restarting or health-checking and its journal at
// the selected checkpoint. Any other failure — corrupt or inconsistent state,
// mismatched facts, wrong role or unverified image — is refused unchanged, and
// a candidate whose lifecycle/checkpoint does not match is never accepted by
// either path.
//
// The locators are only a pointer; the enrollment record, updater state and
// selected manifest are read independently by the existing internal verifier,
// which is reused unchanged and never weakened here. stateRoot must be the
// trusted per-user derivation (DefaultStateRoot in production); the versions
// root is fixed at <install root>/versions; executable must be os.Executable()
// in production; client selects the fixed launcher role (agent vs cercano-cli).
//
// No files are provisioned or migrated and nothing is executed.
func VerifyChild(ctx context.Context, locators Locators, stateRoot, executable string, client bool) (enrollment.Record, error) {
	if err := locators.Validate(); err != nil {
		return enrollment.Record{}, err
	}
	versionsDirectory := filepath.Join(locators.InstallRoot, "versions")
	handoff := enrollment.ChildContext{Root: locators.InstallRoot, InstallID: locators.InstallID}
	record, err := enrollment.VerifyInstalledChild(ctx, handoff, stateRoot, versionsDirectory, executable, client)
	if err == nil {
		return record, nil
	}
	// Completed lookup says "no completed selection for this exact selection":
	// a selected-but-unverified candidate may still be at its approved startup
	// window. Only that normal not-found falls through to the candidate
	// verifier; every other failure is refused unchanged.
	if !errors.Is(err, state.ErrRecordNotFound) {
		return enrollment.Record{}, err
	}
	return enrollment.VerifySelectedChild(ctx, handoff, stateRoot, versionsDirectory, executable, client)
}

// VerifyStartupVerified is the early main() gate for a child launched with
// the approved locators: it validates the ACTUAL running executable
// (os.Executable(), never a caller- or locator-supplied path) and the fixed
// launcher role against the installation's protected completed selection,
// using the per-user state root derived from the environment. It must run
// before any normal flag parsing, config load, DB open, provider or agent
// start. It performs exactly ONE verification pass and returns the opaque
// verified installation context for consumers that need it (stable launcher,
// enrollment identity, role); on failure the returned context is absent and
// paired with the error, so a caller cannot keep a half-verified identity.
// An error means the process must refuse startup; without locators startup
// is unchanged and this is never called.
func VerifyStartupVerified(ctx context.Context, locators Locators, client bool) (Verified, error) {
	executable, err := os.Executable()
	if err != nil {
		return Verified{}, fmt.Errorf("installcontext: running executable: %w", err)
	}
	stateRoot, err := DefaultStateRoot()
	if err != nil {
		return Verified{}, err
	}
	return VerifyChildVerified(ctx, locators, stateRoot, executable, client)
}

// VerifyStartup keeps the bare verified record for existing callers; it is
// the same single-pass startup gate as VerifyStartupVerified.
func VerifyStartup(ctx context.Context, locators Locators, client bool) (enrollment.Record, error) {
	verified, err := VerifyStartupVerified(ctx, locators, client)
	if err != nil {
		return enrollment.Record{}, err
	}
	return verified.Record(), nil
}
