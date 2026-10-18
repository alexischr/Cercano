package enrollment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"cercano/source/server/internal/updatecoord/activation"
	"cercano/source/server/internal/updatecoord/operation"
	"cercano/source/server/internal/updatecoord/privdir"
	"cercano/source/server/internal/updatecoord/selection"
	"cercano/source/server/internal/updatecoord/state"
)

// stateRootForInstall builds the trusted per-installation updater state root
// (protected dirs) for an enrolled record.
func stateRootForInstall(t *testing.T, enrolled Record) string {
	t.Helper()
	stateRoot, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	org := "cercano"
	if runtime.GOOS == "windows" {
		org = "Cercano"
	}
	dir := stateRoot
	for _, part := range []string{org, "updater", enrolled.InstallID} {
		dir = filepath.Join(dir, part)
		if _, e = privdir.Ensure(dir); e != nil {
			t.Fatal(e)
		}
	}
	return stateRoot
}

// selectedCandidateFixture builds an enrolled root plus a protected selection
// whose manifest and journal sit at the approved Selected checkpoint with the
// current operation restarting — the exact candidate startup window.
func selectedCandidateFixture(t *testing.T) (root string, enrolled Record, stateRoot, versions, versionDir string, store *state.Store, opID int64) {
	t.Helper()
	ctx := context.Background()
	root = fixture(t)
	enrolled = created(t, root)
	stateRoot = stateRootForInstall(t, enrolled)
	var e error
	store, e = state.Open(stateRoot, enrolled.InstallID)
	if e != nil {
		t.Fatal(e)
	}
	op, e := state.NewAdapter(store).Start(ctx, "1.0.0")
	if e != nil {
		t.Fatal(e)
	}
	versions = filepath.Join(root, "versions")
	if _, e = privdir.Ensure(versions); e != nil {
		t.Fatal(e)
	}
	versionDir = filepath.Join(versions, "v1")
	if _, e = privdir.Ensure(versionDir); e != nil {
		t.Fatal(e)
	}
	a, c := names()
	data := []byte("inert verified child image")
	members := []state.ManifestMember{}
	for _, name := range []string{a, c} {
		if e = os.WriteFile(filepath.Join(versionDir, name), data, 0700); e != nil {
			t.Fatal(e)
		}
		members = append(members, state.ManifestMember{Path: name, Length: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	manifest := state.ActivationManifest{SchemaVersion: state.ActivationManifestSchemaVersion, InstallID: enrolled.InstallID, OpID: op.ID, TargetVersion: "1.0.0", StagedVersionDir: "v1", VerifiedArtifactSHA256: strings.Repeat("a", 64), Members: members}
	if _, e = store.SaveActivationManifest(ctx, manifest); e != nil {
		t.Fatal(e)
	}
	journal := state.ActivationJournal{SchemaVersion: state.ActivationJournalSchemaVersion, InstallID: enrolled.InstallID, OpID: op.ID, TargetVersion: manifest.TargetVersion, StagedVersionDir: manifest.StagedVersionDir, VerifiedArtifactSHA256: manifest.VerifiedArtifactSHA256, Checkpoint: state.JournalPrepared}
	rev, e := store.SaveActivationJournal(ctx, 0, journal)
	if e != nil {
		t.Fatal(e)
	}
	for _, cp := range []state.JournalCheckpoint{state.JournalSwitchIntent, state.JournalSelected} {
		journal.Checkpoint = cp
		rev, e = store.SaveActivationJournal(ctx, rev, journal)
		if e != nil {
			t.Fatal(e)
		}
	}
	rec, r, e := store.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateRestarting
	if _, e = store.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	selected := activation.Selection{SchemaVersion: 1, InstallID: enrolled.InstallID, Generation: 1, SelectedVersion: "1.0.0", StagedVersionDir: "v1", VerifiedArtifactSHA256: manifest.VerifiedArtifactSHA256}
	raw, e := json.Marshal(selected)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(store.Directory(), selection.FileName), raw, 0600); e != nil {
		t.Fatal(e)
	}
	return root, enrolled, stateRoot, versions, versionDir, store, op.ID
}

func TestSelectedChildVerifierAuthorizesCandidateStartup(t *testing.T) {
	ctx := context.Background()
	root, enrolled, stateRoot, versions, versionDir, store, opID := selectedCandidateFixture(t)
	defer store.Close()
	a, c := names()
	handoff := ChildContext{Root: root, InstallID: enrolled.InstallID}
	exe := filepath.Join(versionDir, a)

	// The completed-only verifier still refuses the candidate.
	if _, e := VerifyInstalledChild(ctx, handoff, stateRoot, versions, exe, false); e == nil {
		t.Fatal("completed-only verifier authorized a candidate")
	}
	// The selected-child verifier authorizes the exact selected image.
	if got, e := VerifySelectedChild(ctx, handoff, stateRoot, versions, exe, false); e != nil || got != enrolled {
		t.Fatal("candidate startup not authorized", got, e)
	}
	if _, e := VerifySelectedChild(ctx, handoff, stateRoot, versions, exe, true); e == nil {
		t.Fatal("agent executable accepted in client role")
	}
	if _, e := VerifySelectedChild(ctx, handoff, stateRoot, versions, filepath.Join(versionDir, c), true); e != nil {
		t.Fatal("client role", e)
	}
	if _, e := VerifySelectedChild(ctx, handoff, stateRoot, versions, filepath.Join(versionDir, c), false); e == nil {
		t.Fatal("client executable accepted in agent role")
	}
	// Outside the startup window (operation completed) the candidate verifier
	// must refuse; the completed-only verifier now authorizes instead.
	rec, r, e := store.LoadOperationRecord(ctx, opID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = store.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifySelectedChild(ctx, handoff, stateRoot, versions, exe, false); e == nil {
		t.Fatal("completed operation accepted as candidate")
	}
}
