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

func TestInstalledChildUsesSelectedProtectedManifest(t *testing.T) {
	ctx := context.Background()
	root := fixture(t)
	enrolled := created(t, root)
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
	store, e := state.Open(stateRoot, enrolled.InstallID)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	op, e := state.NewAdapter(store).Start(ctx, "1.0.0")
	if e != nil {
		t.Fatal(e)
	}
	versions := filepath.Join(root, "versions")
	if _, e = privdir.Ensure(versions); e != nil {
		t.Fatal(e)
	}
	versionDir := filepath.Join(versions, "v1")
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
	for _, cp := range []state.JournalCheckpoint{state.JournalSwitchIntent, state.JournalSelected, state.JournalHealthVerified, state.JournalComplete} {
		journal.Checkpoint = cp
		rev, e = store.SaveActivationJournal(ctx, rev, journal)
		if e != nil {
			t.Fatal(e)
		}
	}
	selected := activation.Selection{SchemaVersion: 1, InstallID: enrolled.InstallID, Generation: 1, SelectedVersion: "1.0.0", StagedVersionDir: "v1", VerifiedArtifactSHA256: manifest.VerifiedArtifactSHA256}
	raw, e := json.Marshal(selected)
	if e != nil {
		t.Fatal(e)
	}
	selectionPath := filepath.Join(store.Directory(), selection.FileName)
	if e = os.WriteFile(selectionPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	handoff := ChildContext{Root: root, InstallID: enrolled.InstallID}
	exe := filepath.Join(versionDir, a)
	verify := func() (Record, error) { return VerifyInstalledChild(ctx, handoff, stateRoot, versions, exe, false) }
	if _, e = verify(); e == nil {
		t.Fatal("incomplete operation authorized child")
	}
	rec, r, e := store.LoadOperationRecord(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateComplete
	rec.HealthVerified = true
	if _, e = store.SaveOperationRecord(ctx, r, rec); e != nil {
		t.Fatal(e)
	}
	if _, e = state.NewAdapter(store).Start(ctx, "2.0.0"); e != nil {
		t.Fatal(e)
	}
	if got, e := verify(); e != nil || got != enrolled {
		t.Fatal("selected image not verified with newer pending operation", got, e)
	}
	if _, e = VerifyInstalledChild(ctx, handoff, stateRoot, versions, exe, true); e == nil {
		t.Fatal("wrong role accepted")
	}
	if _, e = VerifyInstalledChild(ctx, handoff, stateRoot, versions, filepath.Join(versionDir, c), true); e != nil {
		t.Fatal("client role", e)
	}
	handoff.InstallID = "wrong"
	if _, e = verify(); e == nil {
		t.Fatal("wrong installation accepted")
	}
	handoff.InstallID = enrolled.InstallID
	if e = os.WriteFile(exe, []byte(strings.Repeat("x", len(data))), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = verify(); e == nil {
		t.Fatal("same-size tamper accepted")
	}
	if e = os.WriteFile(exe, data, 0700); e != nil {
		t.Fatal(e)
	}
	selected.Generation++
	bad, e := json.Marshal(selected)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(selectionPath, bad, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = verify(); e == nil {
		t.Fatal("wrong selection generation accepted")
	}
	if e = os.WriteFile(selectionPath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, e = VerifyInstalledChild(ctx, handoff, missing, versions, exe, false); e == nil {
		t.Fatal("missing state accepted")
	}
	if _, e = os.Stat(missing); !os.IsNotExist(e) {
		t.Fatal("missing state initialized", e)
	}
}
