package activationtxn

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"cercano/source/server/internal/updatecoord/state"
)

func TestSwitchCapturesManifestBeforeIntentAndPreservesItOnRetry(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	sentinel := errors.New("fixture interruption after intent")
	testHookAfterIntent = func() error {
		m, rev, e := f.store.LoadActivationManifest(ctx, f.opID)
		if e != nil || rev != 1 || len(m.Members) != 2 {
			t.Fatalf("intent without manifest: %+v %d %v", m, rev, e)
		}
		return sentinel
	}
	t.Cleanup(func() { testHookAfterIntent = nil })
	if _, e := Switch(ctx, f.request()); !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	before, rev, e := f.store.LoadActivationManifest(ctx, f.opID)
	if e != nil {
		t.Fatal(e)
	}
	f.requireJournal(state.JournalSwitchIntent, 2)
	f.requireSelectionAbsent()
	testHookAfterIntent = nil
	if _, e := Switch(ctx, f.request()); e != nil {
		t.Fatal(e)
	}
	after, r, e := f.store.LoadActivationManifest(ctx, f.opID)
	if e != nil || r != rev || !reflect.DeepEqual(after, before) {
		t.Fatal("manifest rewritten", r, e)
	}
}

func TestSwitchMissingPostIntentManifestRefusesWithoutInventingFacts(t *testing.T) {
	f := newFixture(t, false)
	f.advanceJournal(state.JournalSwitchIntent)
	if _, e := Switch(context.Background(), f.request()); !errors.Is(e, ErrTargetImages) {
		t.Fatal("legacy missing manifest accepted", e)
	}
	if _, _, e := f.store.LoadActivationManifest(context.Background(), f.opID); !errors.Is(e, state.ErrRecordNotFound) {
		t.Fatal("manifest invented", e)
	}
	f.requireJournal(state.JournalSwitchIntent, 2)
	f.requireSelectionAbsent()
}

func TestSwitchRefusesConflictingDurableManifestBeforeIntent(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	j, _ := f.journal()
	// Persist a structurally valid but contradictory artifact before the switch.
	p := f.request().Images
	m := state.ActivationManifest{SchemaVersion: state.ActivationManifestSchemaVersion, InstallID: j.InstallID, OpID: j.OpID, TargetVersion: j.TargetVersion, StagedVersionDir: j.StagedVersionDir, VerifiedArtifactSHA256: strings.Repeat("b", 64)}
	for _, member := range p.Manifest.Members {
		if member.Name == p.AgentMember || member.Name == p.ClientMember {
			m.Members = append(m.Members, state.ManifestMember{Path: member.Name, Length: member.Size, SHA256: member.SHA256})
		}
	}
	if _, e := f.store.SaveActivationManifest(ctx, m); e != nil {
		t.Fatal(e)
	}
	if _, e := Switch(ctx, f.request()); !errors.Is(e, ErrTargetImages) {
		t.Fatal("conflicting manifest accepted", e)
	}
	got, rev, e := f.store.LoadActivationManifest(ctx, f.opID)
	if e != nil || rev != 1 || !reflect.DeepEqual(got, m) {
		t.Fatal("conflicting facts overwritten", e)
	}
	f.requireJournal(state.JournalPrepared, 1)
	f.requireSelectionAbsent()
}
