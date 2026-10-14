package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/broker"
	"cercano/source/server/internal/updatecoord/operation"
	"cercano/source/server/internal/updatecoord/state"
)

func forceLocalFixture(t *testing.T, ids ...string) (string, *state.Store, int64) {
	t.Helper()
	root, s, _, _ := installedNoticeFixture(t, ids...)
	op, e := state.NewAdapter(s).Start(context.Background(), "9.9.9")
	if e != nil {
		t.Fatal(e)
	}
	r, rev, e := s.LoadOperationRecord(context.Background(), op.ID)
	if e != nil {
		t.Fatal(e)
	}
	r.State = operation.StateDraining
	r.ConsentRecorded = true
	if _, e = s.SaveOperationRecord(context.Background(), rev, r); e != nil {
		t.Fatal(e)
	}
	j := state.ActivationJournal{SchemaVersion: state.ActivationJournalSchemaVersion, InstallID: s.InstallID(), OpID: op.ID, TargetVersion: "9.9.9", StagedVersionDir: "staged-new", VerifiedArtifactSHA256: strings.Repeat("b", 64), Checkpoint: state.JournalPrepared}
	if _, e = s.SaveActivationJournal(context.Background(), 0, j); e != nil {
		t.Fatal(e)
	}
	id := s.InstallID()
	s.Close() // publish current main-file header for existing-only control
	s, e = state.Open(root, id)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return root, s, op.ID
}
func TestLocalForceIntentInvokesOnlyOwnedCallbackOnce(t *testing.T) {
	root, store, id := forceLocalFixture(t)
	ctx := context.Background()
	s, _ := newServerWithStore(t)
	s.turnBroker = broker.New()
	calls := 0
	s.SetProcessForceStopRequester(func(string) { calls++ })
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil || owner.agent.PID != int64(os.Getpid()) {
		t.Fatal(owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration alone stopped agent", stopped, e)
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found || published != owner.agent {
		t.Fatal(published, found, e)
	}
	work, gen, release := s.turnBroker.BeginTurn(ctx, "conversation")
	defer release()
	now := time.Now()
	intent := state.ForceIntent{OpID: id, Agent: published, IssuedMS: now.UnixMilli(), ExpiresMS: now.Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); !stopped || e != nil {
		t.Fatal(stopped, e)
	}
	if calls != 1 || work.Err() != context.Canceled || !s.turnBroker.IsCurrent("conversation", gen) {
		t.Fatal("owned interruption/reporting failed", calls, work.Err())
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || calls != 1 {
		t.Fatal("request replayed", stopped, e, calls)
	}
}
func TestLocalForceSessionReplacementDoesNotStopEitherAgent(t *testing.T) {
	root, store, id := forceLocalFixture(t)
	ctx := context.Background()
	s := &Server{}
	calls := 0
	s.SetProcessForceStopRequester(func(string) { calls++ })
	old, e := s.newForceLocalOwner()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = old.poll(ctx, s, root, store.InstallID()); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	f := state.ForceIntent{OpID: id, Agent: old.agent, IssuedMS: now.UnixMilli(), ExpiresMS: now.Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, f, true, time.Now); e != nil {
		t.Fatal(e)
	}
	replacementServer := &Server{}
	replacementServer.SetProcessForceStopRequester(func(string) { calls++ })
	replacement, e := replacementServer.newForceLocalOwner()
	if e != nil {
		t.Fatal(e)
	}
	if stopped, e := replacement.poll(ctx, replacementServer, root, store.InstallID()); stopped || e == nil {
		t.Fatal("new lifetime adopted old request", stopped, e)
	}
	if stopped, e := old.poll(ctx, s, root, store.InstallID()); stopped || e == nil {
		t.Fatal("revoked lifetime consumed request", stopped, e)
	}
	if calls != 0 {
		t.Fatal("unproven stop requested", calls)
	}
}

func TestLocalForceOwnerIsStableAndCannotRebindInstallations(t *testing.T) {
	root, store, _ := forceLocalFixture(t)
	s := &Server{}
	s.SetProcessForceStopRequester(func(string) {})
	first, e := s.newForceLocalOwner()
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.newForceLocalOwner()
	if e != nil || first != second {
		t.Fatal("observer restart minted another lifetime", e)
	}
	if _, e = first.poll(context.Background(), s, root, store.InstallID()); e != nil {
		t.Fatal(e)
	}
	if stopped, e := second.poll(context.Background(), s, t.TempDir(), "other-install"); stopped || e != state.ErrForceTarget {
		t.Fatal("owner rebound", stopped, e)
	}
}
