// Package stablelaunch dispatches an explicitly enrolled stable executable to
// its verified selected member. It never enrolls, migrates, downloads or starts
// a background agent. Installation locators are not authority.
package stablelaunch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cercano/source/server/internal/updatecoord/activation"
	"cercano/source/server/internal/updatecoord/enrollment"
	"cercano/source/server/internal/updatecoord/exclusion"
	"cercano/source/server/internal/updatecoord/selection"
	"cercano/source/server/internal/updatecoord/state"
	"cercano/source/server/pkg/installcontext"
)

// Startup runs after private locator parsing/verification, before ordinary
// initialization. A verified child must never dispatch again. Absent enrollment
// leaves the old startup path unchanged. Invalid existing enrollment fails closed.
// Unix success replaces this process; Windows success returns the child's status.
func Startup(args []string, client bool, verified installcontext.Verified) (handled bool, code int, err error) {
	if verified.Present() {
		return false, 0, nil
	}
	executable, e := os.Executable()
	if e != nil {
		return true, 1, e
	}
	// Check presence first: unenrolled copies must not derive/open updater state.
	if _, e = os.Lstat(filepath.Join(filepath.Dir(executable), enrollment.FileName)); os.IsNotExist(e) {
		return false, 0, nil
	} else if e != nil {
		return true, 1, e
	}
	root, e := installcontext.DefaultStateRoot()
	if e != nil {
		return true, 1, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, e := prepare(ctx, executable, root, args, client)
	if e != nil {
		return true, 1, e
	}
	defer p.lock.Close()
	code, e = execute(p)
	return true, code, e
}

type prepared struct {
	target string
	args   []string
	lock   *exclusion.Handle
}

func prepare(ctx context.Context, executable, stateRoot string, args []string, client bool) (*prepared, error) {
	if ctx == nil {
		return nil, errors.New("stablelaunch: missing context")
	}
	// Do not accept a caller injecting a second installation identity.
	_, found, _, err := installcontext.Parse(args)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, errors.New("stablelaunch: child locators cannot be redispatched")
	}
	enrolled, err := enrollment.DiscoverStableLauncher(ctx, executable)
	if err != nil {
		return nil, err
	}
	name := enrolled.Agent
	if client {
		name = enrolled.Client
	}
	if filepath.Base(executable) != name {
		return nil, enrollment.ErrUnsafe
	}
	store, err := state.OpenExistingReadOnly(ctx, stateRoot, enrolled.InstallID)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	lock, err := exclusion.Acquire(ctx, store.Directory(), exclusion.Launch)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = lock.Close()
		}
	}()
	selectedPath := filepath.Join(store.Directory(), selection.FileName)
	selected, err := activation.ReadSelection(selectedPath)
	if err != nil {
		return nil, err
	}
	if selected.InstallID != enrolled.InstallID {
		return nil, enrollment.ErrUnsafe
	}
	target := state.SelectedVersion{Version: selected.SelectedVersion, Directory: selected.StagedVersionDir, ArtifactSHA256: selected.VerifiedArtifactSHA256, Generation: selected.Generation}
	manifest, err := store.LoadCompletedSelectionManifest(ctx, target)
	if errors.Is(err, state.ErrRecordNotFound) {
		manifest, err = store.LoadCandidateSelectionManifest(ctx, target)
	}
	if err != nil {
		return nil, err
	}
	path := ""
	for _, member := range manifest.Members {
		if filepath.Base(filepath.FromSlash(member.Path)) == name {
			if path != "" {
				return nil, enrollment.ErrUnsafe
			}
			path = filepath.Join(enrolled.Root, "versions", filepath.FromSlash(manifest.StagedVersionDir), filepath.FromSlash(member.Path))
		}
	}
	if path == "" || path == executable {
		return nil, enrollment.ErrUnsafe
	}
	locators := installcontext.Locators{InstallRoot: enrolled.Root, InstallID: enrolled.InstallID}
	checked, err := installcontext.VerifyChild(ctx, locators, stateRoot, path, client)
	if err != nil {
		return nil, err
	}
	if checked != enrolled {
		return nil, enrollment.ErrUnsafe
	}
	after, err := activation.ReadSelection(selectedPath)
	if err != nil {
		return nil, err
	}
	if after != selected {
		return nil, enrollment.ErrUnsafe
	}
	current, err := enrollment.DiscoverStableLauncher(ctx, executable)
	if err != nil {
		return nil, err
	}
	if current != enrolled {
		return nil, enrollment.ErrUnsafe
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	private, err := installcontext.LocatorArgs(enrolled.Root, enrolled.InstallID)
	if err != nil {
		return nil, err
	}
	// Prepend locators so an ordinary -- terminator never hides them.
	childArgs := append(private, args...)
	keep = true
	return &prepared{target: path, args: childArgs, lock: lock}, nil
}

func launchError(err error) error {
	return fmt.Errorf("stablelaunch: selected executable could not start: %w", err)
}
