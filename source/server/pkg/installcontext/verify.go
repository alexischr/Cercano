package installcontext

import (
	"context"
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
// locators against the protected completed selection of its installation.
//
// The locators are only a pointer; the enrollment record, updater state and
// selected manifest are read independently by the existing internal verifier
// (enrollment.VerifyInstalledChild), which is reused unchanged and never
// weakened here. stateRoot must be the trusted per-user derivation
// (DefaultStateRoot in production); the versions root is fixed at
// <install root>/versions; executable must be os.Executable() in production;
// client selects the fixed launcher role (agent vs cercano-cli).
//
// No files are provisioned or migrated and nothing is executed. It is
// completed-installation verification only, not first-launch/candidate
// authorization.
func VerifyChild(ctx context.Context, locators Locators, stateRoot, executable string, client bool) (enrollment.Record, error) {
	if err := locators.Validate(); err != nil {
		return enrollment.Record{}, err
	}
	versionsDirectory := filepath.Join(locators.InstallRoot, "versions")
	return enrollment.VerifyInstalledChild(ctx,
		enrollment.ChildContext{Root: locators.InstallRoot, InstallID: locators.InstallID},
		stateRoot, versionsDirectory, executable, client)
}

// VerifyStartup is the early main() gate for a child launched with the
// approved locators: it validates the ACTUAL running executable
// (os.Executable(), never a caller- or locator-supplied path) and the fixed
// launcher role against the installation's protected completed selection,
// using the per-user state root derived from the environment. It must run
// before any normal flag parsing, config load, DB open, provider or agent
// start. An error means the process must refuse startup; without locators
// startup is unchanged and this is never called.
func VerifyStartup(ctx context.Context, locators Locators, client bool) (enrollment.Record, error) {
	executable, err := os.Executable()
	if err != nil {
		return enrollment.Record{}, fmt.Errorf("installcontext: running executable: %w", err)
	}
	stateRoot, err := DefaultStateRoot()
	if err != nil {
		return enrollment.Record{}, err
	}
	return VerifyChild(ctx, locators, stateRoot, executable, client)
}
