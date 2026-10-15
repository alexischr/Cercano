package enrollment

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"cercano/source/server/internal/updatecoord/activation"
	"cercano/source/server/internal/updatecoord/privdir"
	"cercano/source/server/internal/updatecoord/selection"
	"cercano/source/server/internal/updatecoord/state"
)

// VerifyInstalledChild obtains fingerprints independently from protected existing
// state for a completed, healthy selection. It is NOT a first-launch/candidate
// verifier: a candidate awaiting health must use its separate activation path.
// handoff is only a locator. stateRoot and versionsDirectory must come from
// trusted local installation policy, never the handoff/argv/PATH; executable
// must be os.Executable() in production. client selects the fixed launcher role.
// No files are provisioned or migrated, and no image is executed. SQLite may
// use its normal WAL/SHM coordination files even on a read-only connection.
func VerifyInstalledChild(ctx context.Context, handoff ChildContext, stateRoot, versionsDirectory, executable string, client bool) (Record, error) {
	if ctx == nil {
		return Record{}, ErrUnsafe
	}
	enrolled, err := Read(ctx, handoff.Root)
	if err != nil {
		return Record{}, err
	}
	if enrolled.InstallID != handoff.InstallID {
		return Record{}, ErrUnsafe
	}
	if !filepath.IsAbs(versionsDirectory) || filepath.Clean(versionsDirectory) != versionsDirectory {
		return Record{}, ErrUnsafe
	}
	canonical, err := filepath.EvalSymlinks(versionsDirectory)
	if err != nil || canonical != versionsDirectory {
		return Record{}, ErrUnsafe
	}
	relative, err := filepath.Rel(enrolled.Root, versionsDirectory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Record{}, ErrUnsafe
	}
	if err := privdir.VerifyExisting(versionsDirectory); err != nil {
		return Record{}, err
	}
	store, err := state.OpenExistingReadOnly(ctx, stateRoot, enrolled.InstallID)
	if err != nil {
		return Record{}, err
	}
	defer store.Close()
	path := filepath.Join(store.Directory(), selection.FileName)
	selected, err := activation.ReadSelection(path)
	if err != nil {
		return Record{}, err
	}
	if selected.InstallID != enrolled.InstallID {
		return Record{}, ErrUnsafe
	}
	target := state.SelectedVersion{Version: selected.SelectedVersion, Directory: selected.StagedVersionDir, ArtifactSHA256: selected.VerifiedArtifactSHA256, Generation: selected.Generation}
	manifest, err := store.LoadCompletedSelectionManifest(ctx, target)
	if err != nil {
		return Record{}, err
	}
	agentName, clientName := names()
	name := agentName
	if client {
		name = clientName
	}
	// Match the actual image against a unique persisted member of the expected
	// role. No caller-supplied digest or member path is accepted as authority.
	matched := false
	var result Record
	for _, member := range manifest.Members {
		expected := filepath.Join(versionsDirectory, filepath.FromSlash(manifest.StagedVersionDir), filepath.FromSlash(member.Path))
		if executable != expected || filepath.Base(expected) != name {
			continue
		}
		if matched {
			return Record{}, ErrUnsafe
		}
		info, err := os.Lstat(expected)
		if err != nil || info.Size() != member.Length {
			return Record{}, ErrUnsafe
		}
		result, err = VerifyChildContext(ctx, handoff, executable, expected, member.SHA256, member.Length)
		if err != nil {
			return Record{}, err
		}
		matched = true
	}
	if !matched {
		return Record{}, ErrUnsafe
	}
	after, err := activation.ReadSelection(path)
	if err != nil {
		return Record{}, err
	}
	if after != selected {
		return Record{}, ErrUnsafe
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	return result, nil
}
