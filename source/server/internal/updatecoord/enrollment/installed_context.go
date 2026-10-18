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

// openChildSelection performs the shared protected-locator preamble for both
// child verifiers: read the enrollment record independently of the handoff,
// bind the install ID, check that the versions directory is a trusted,
// canonical path inside the enrolled root, verify the private state root and
// open the installation's read-only store, then read the persisted selection
// file (twice-checked content, returned to the caller for re-reading).
func openChildSelection(ctx context.Context, handoff ChildContext, stateRoot, versionsDirectory string) (Record, *state.Store, string, activation.Selection, error) {
	enrolled, err := Read(ctx, handoff.Root)
	if err != nil {
		return Record{}, nil, "", activation.Selection{}, err
	}
	if enrolled.InstallID != handoff.InstallID {
		return Record{}, nil, "", activation.Selection{}, ErrUnsafe
	}
	if !filepath.IsAbs(versionsDirectory) || filepath.Clean(versionsDirectory) != versionsDirectory {
		return Record{}, nil, "", activation.Selection{}, ErrUnsafe
	}
	canonical, err := filepath.EvalSymlinks(versionsDirectory)
	if err != nil || canonical != versionsDirectory {
		return Record{}, nil, "", activation.Selection{}, ErrUnsafe
	}
	relative, err := filepath.Rel(enrolled.Root, versionsDirectory)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Record{}, nil, "", activation.Selection{}, ErrUnsafe
	}
	if err := privdir.VerifyExisting(versionsDirectory); err != nil {
		return Record{}, nil, "", activation.Selection{}, err
	}
	store, err := state.OpenExistingReadOnly(ctx, stateRoot, enrolled.InstallID)
	if err != nil {
		return Record{}, nil, "", activation.Selection{}, err
	}
	path := filepath.Join(store.Directory(), selection.FileName)
	selected, err := activation.ReadSelection(path)
	if err != nil {
		store.Close()
		return Record{}, nil, "", activation.Selection{}, err
	}
	if selected.InstallID != enrolled.InstallID {
		store.Close()
		return Record{}, nil, "", activation.Selection{}, ErrUnsafe
	}
	return enrolled, store, path, selected, nil
}

// verifyChildAgainstManifest is the shared image validation for the completed
// and selected child verifiers. It matches the actual image against a unique
// persisted member of the expected role — no caller-supplied digest or member
// path is accepted as authority — and re-reads the selection file afterwards
// to catch a selection that changed mid-verification.
func verifyChildAgainstManifest(ctx context.Context, handoff ChildContext, versionsDirectory, executable string, client bool, manifest state.ActivationManifest, selectionPath string, selected activation.Selection) (Record, error) {
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
	after, err := activation.ReadSelection(selectionPath)
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

// VerifyInstalledChild obtains fingerprints independently from protected existing
// state for a completed, healthy selection. It is NOT a first-launch/candidate
// verifier: a candidate awaiting health must use VerifySelectedChild, which
// shares this function's locator checks and image validation but authorizes
// only the exact selected candidate startup window. handoff is only a locator.
// stateRoot and versionsDirectory must come from trusted local installation
// policy, never the handoff/argv/PATH; executable must be os.Executable() in
// production. client selects the fixed launcher role.
// No files are provisioned or migrated, and no image is executed. SQLite may
// use its normal WAL/SHM coordination files even on a read-only connection.
func VerifyInstalledChild(ctx context.Context, handoff ChildContext, stateRoot, versionsDirectory, executable string, client bool) (Record, error) {
	if ctx == nil {
		return Record{}, ErrUnsafe
	}
	enrolled, store, selectionPath, selected, err := openChildSelection(ctx, handoff, stateRoot, versionsDirectory)
	if err != nil {
		return Record{}, err
	}
	_ = enrolled
	defer store.Close()
	target := state.SelectedVersion{Version: selected.SelectedVersion, Directory: selected.StagedVersionDir, ArtifactSHA256: selected.VerifiedArtifactSHA256, Generation: selected.Generation}
	manifest, err := store.LoadCompletedSelectionManifest(ctx, target)
	if err != nil {
		return Record{}, err
	}
	return verifyChildAgainstManifest(ctx, handoff, versionsDirectory, executable, client, manifest, selectionPath, selected)
}

// VerifySelectedChild authorizes a CANDIDATE child image at its exact approved
// startup window: the installation's current, allocated operation must be in
// the restarting or health-check lifecycle and its activation journal must sit
// at the selected checkpoint, with the manifest, journal and operation bound to
// the selection's facts and generation (prior selection generation + 1). It
// reuses VerifyInstalledChild's locator checks and image validation unchanged,
// replacing only the manifest lookup with the selected-candidate gate; the
// completed-only verifier never accepts a candidate and this verifier never
// accepts a completed operation. handoff is only a locator. stateRoot and
// versionsDirectory must come from trusted local installation policy, never
// the handoff/argv/PATH; executable must be os.Executable() in production.
// client selects the fixed launcher role. No files are provisioned or
// migrated, and no image is executed.
func VerifySelectedChild(ctx context.Context, handoff ChildContext, stateRoot, versionsDirectory, executable string, client bool) (Record, error) {
	if ctx == nil {
		return Record{}, ErrUnsafe
	}
	enrolled, store, selectionPath, selected, err := openChildSelection(ctx, handoff, stateRoot, versionsDirectory)
	if err != nil {
		return Record{}, err
	}
	_ = enrolled
	defer store.Close()
	target := state.SelectedVersion{Version: selected.SelectedVersion, Directory: selected.StagedVersionDir, ArtifactSHA256: selected.VerifiedArtifactSHA256, Generation: selected.Generation}
	manifest, err := store.LoadCandidateSelectionManifest(ctx, target)
	if err != nil {
		return Record{}, err
	}
	return verifyChildAgainstManifest(ctx, handoff, versionsDirectory, executable, client, manifest, selectionPath, selected)
}
