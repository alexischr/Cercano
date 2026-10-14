package server

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/broker"
	"cercano/source/server/internal/updatecoord/operation"
	"cercano/source/server/internal/updatecoord/state"
)

// localSaveFixture registers the process-local owner against a real temp
// ForceControl DB, submits a fresh intent, and polls once so the recorder-backed
// save failure publishes a protected pending decision. It returns the poll
// inputs plus the retained process-local receipt.
func localSaveFixture(t *testing.T) (ctx context.Context, root string, store *state.Store, opID int64, s *Server, work context.Context, calls *int, owner *forceLocalOwner, receipt *ForceSaveFailure) {
	t.Helper()
	root, store, opID = forceLocalFixture(t)
	s, work, calls = failedSaveServer(t)
	ctx = context.Background()
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil {
		t.Fatal("local force owner", owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration poll stopped agent", stopped, e)
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found || published != owner.agent {
		t.Fatal("agent registration", published, found, e)
	}
	intent := state.ForceIntent{OpID: opID, Agent: published, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || !errors.As(e, &receipt) {
		t.Fatal("save failure poll did not surface receipt", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("save failure stopped work")
	}
	return ctx, root, store, opID, s, work, calls, owner, receipt
}

// TestLocalSaveFailurePublishesProtectedPendingDecisionWithoutStopping covers
// the save-failure/no-stop path: the consumed intent surfaces a *ForceSaveFailure,
// the bridge publishes the retained receipt's pending decision (bounded expiry,
// crypto-random receipt ID) and neither stops work nor re-consumes the intent.
func TestLocalSaveFailurePublishesProtectedPendingDecisionWithoutStopping(t *testing.T) {
	ctx, root, store, opID, s, work, calls, owner, receipt := localSaveFixture(t)
	if owner.pendingSave == nil {
		t.Fatal("pending save receipt not retained")
	}
	if owner.pendingSave.receipt != receipt {
		t.Fatal("retained receipt is not the process-local receipt")
	}
	if owner.pendingSave.opID != opID {
		t.Fatal("pending save bound to the wrong operation", owner.pendingSave.opID, opID)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(owner.pendingSave.receiptID) {
		t.Fatal("receipt ID is not crypto-length lowercase hex", owner.pendingSave.receiptID)
	}
	sf, found, e := store.ReadPendingSaveFailure(ctx, opID)
	if e != nil || !found {
		t.Fatal("pending decision not published", found, e)
	}
	if sf.ReceiptID != owner.pendingSave.receiptID || sf.Agent != owner.agent || sf.OpID != opID ||
		sf.Decision != state.ForceDecisionPending || sf.Consumed {
		t.Fatal("published pending decision does not match retained receipt", sf)
	}
	// Polling while the decision is still pending neither stops work nor
	// re-consumes the original intent.
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 || work.Err() != nil {
		t.Fatal("pending-decision poll stopped work", stopped, e)
	}
	if owner.pendingSave == nil || owner.pendingSave.receipt != receipt {
		t.Fatal("receipt dropped while decision pending")
	}
}

// TestLocalSaveFailureCancelDecisionNeverStopsWork covers the cancel/no-stop
// path and its exactly-once semantics.
func TestLocalSaveFailureCancelDecisionNeverStopsWork(t *testing.T) {
	ctx, root, store, opID, s, work, calls, owner, receipt := localSaveFixture(t)
	receiptID := owner.pendingSave.receiptID
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionCancel, false, time.Now); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("cancel decision stopped work", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("cancel decision stopped work")
	}
	if owner.pendingSave != nil {
		t.Fatal("canceled receipt still retained")
	}
	if sf, found, e := store.ReadPendingSaveFailure(ctx, opID); e != nil || found {
		t.Fatal("canceled decision still pending", found, sf, e)
	}
	// The decision is consumed exactly once at the store...
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionForce, true, time.Now); !errors.Is(e, state.ErrSaveFailureReplay) {
		t.Fatal("consumed decision replay allowed", e)
	}
	// ...and the released receipt is no longer actionable through the explicit API.
	if ok, e := s.ForceAfterSaveFailure(ctx, int64(os.Getpid()), true, receipt); ok || e != ErrForceConsent {
		t.Fatal("canceled receipt still actionable", ok, e)
	}
	// Subsequent polls stay idle.
	for i := 0; i < 2; i++ {
		if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 {
			t.Fatal("post-cancel poll not idle", i, stopped, e)
		}
	}
}

// TestLocalSaveFailureForceDecisionStopsExactlyOnce covers the force path and
// its exactly-once semantics.
func TestLocalSaveFailureForceDecisionStopsExactlyOnce(t *testing.T) {
	ctx, root, store, opID, s, work, calls, owner, receipt := localSaveFixture(t)
	receiptID := owner.pendingSave.receiptID
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionForce, true, time.Now); e != nil {
		t.Fatal(e)
	}
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if !stopped || e != nil || *calls != 1 || work.Err() != context.Canceled {
		t.Fatal("force decision did not stop owned work once", stopped, e, *calls, work.Err())
	}
	if owner.pendingSave != nil {
		t.Fatal("forced receipt still retained")
	}
	if sf, found, e := store.ReadPendingSaveFailure(ctx, opID); e != nil || found {
		t.Fatal("forced decision still pending", found, sf, e)
	}
	// Replaying the consumed decision is refused...
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionForce, true, time.Now); !errors.Is(e, state.ErrSaveFailureReplay) {
		t.Fatal("consumed decision replay allowed", e)
	}
	// ...and later polls never re-request the stop.
	for i := 0; i < 3; i++ {
		if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 1 {
			t.Fatal("force decision replayed", i, stopped, e, *calls)
		}
	}
	// The committed stop is durable: re-submitting the receipt acknowledges but
	// never invokes the stop requester again.
	if ok, e := s.ForceAfterSaveFailure(ctx, int64(os.Getpid()), true, receipt); !ok || e != nil || *calls != 1 {
		t.Fatal("committed stop re-invoked requester", ok, e, *calls)
	}
}

// TestLocalSaveFailureOriginalIntentReplayNeverRestops covers replay of the
// original intent after the save failure published: the store refuses the
// resubmission and polling never re-requests the stop.
func TestLocalSaveFailureOriginalIntentReplayNeverRestops(t *testing.T) {
	ctx, root, store, opID, s, work, calls, owner, receipt := localSaveFixture(t)
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found {
		t.Fatal("agent registration", found, e)
	}
	intent := state.ForceIntent{OpID: opID, Agent: published, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e := store.SubmitForceIntent(ctx, intent, true, time.Now); !errors.Is(e, state.ErrForceReplay) {
		t.Fatal("original intent replay allowed", e)
	}
	for i := 0; i < 3; i++ {
		if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 || work.Err() != nil {
			t.Fatal("replayed intent stopped work", i, stopped, e)
		}
	}
	if owner.pendingSave == nil || owner.pendingSave.receipt != receipt {
		t.Fatal("replay dropped the retained receipt")
	}
}

// TestLocalSaveFailureExpiryReleasesRetainedReceipt covers the bounded-expiry
// path: an expired retained receipt is released and never stops work.
func TestLocalSaveFailureExpiryReleasesRetainedReceipt(t *testing.T) {
	ctx, root, store, _, s, work, calls, owner, receipt := localSaveFixture(t)
	owner.mu.Lock()
	owner.pendingSave.receipt.expires = time.Now().Add(-time.Hour)
	owner.mu.Unlock()
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("expired receipt stopped work", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("expired receipt stopped work")
	}
	if owner.pendingSave != nil {
		t.Fatal("expired receipt not released")
	}
	// The released receipt is no longer actionable through the explicit API.
	if ok, e := s.ForceAfterSaveFailure(ctx, int64(os.Getpid()), true, receipt); ok || e != ErrForceConsent {
		t.Fatal("expired receipt still actionable", ok, e)
	}
	// Later polls stay idle.
	for i := 0; i < 2; i++ {
		if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 {
			t.Fatal("post-expiry poll not idle", i, stopped, e)
		}
	}
}

// TestLocalSaveFailureWrongAgentNeverStopsWork covers a changed agent binding:
// a foreign registered agent makes the poll refuse without stopping work and
// without dropping the retained receipt.
func TestLocalSaveFailureWrongAgentNeverStopsWork(t *testing.T) {
	ctx, root, store, _, s, work, calls, owner, _ := localSaveFixture(t)
	foreign, e := state.NewForceAgent(int64(os.Getpid()) + 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.RegisterForceAgent(ctx, foreign); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || !errors.Is(e, state.ErrForceTarget) {
		t.Fatal("foreign agent binding not refused", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("foreign agent stopped work")
	}
	if owner.pendingSave == nil {
		t.Fatal("refused agent binding dropped the retained receipt")
	}
}

// TestLocalSaveFailureWrongReceiptIdentityRefusedWithoutStopping covers a
// changed operation/decision identity: a decision that does not match the
// retained receipt is refused, released and never applied.
func TestLocalSaveFailureWrongReceiptIdentityRefusedWithoutStopping(t *testing.T) {
	ctx, root, store, _, s, work, calls, owner, _ := localSaveFixture(t)
	if e := store.SubmitSaveDecision(ctx, owner.pendingSave.receiptID, state.ForceDecisionForce, true, time.Now); e != nil {
		t.Fatal(e)
	}
	owner.mu.Lock()
	owner.pendingSave.receiptID = strings.Repeat("a", 64)
	owner.mu.Unlock()
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || !errors.Is(e, state.ErrSaveFailureWrongOp) {
		t.Fatal("mismatched receipt identity not refused", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("mismatched receipt identity stopped work")
	}
	if owner.pendingSave != nil {
		t.Fatal("mismatched receipt identity still retained")
	}
	// Subsequent polls stay idle.
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 {
		t.Fatal("post-refusal poll not idle", stopped, e)
	}
}

// TestLocalSaveFailureIneligibleOperationErrorIsRetriedNotStopped covers error
// handling: an ineligible operation makes decision consumption fail, the work is
// never stopped, the receipt is retained and a later poll retries the decision.
func TestLocalSaveFailureIneligibleOperationErrorIsRetriedNotStopped(t *testing.T) {
	ctx, root, store, opID, s, work, calls, owner, _ := localSaveFixture(t)
	if e := store.SubmitSaveDecision(ctx, owner.pendingSave.receiptID, state.ForceDecisionCancel, false, time.Now); e != nil {
		t.Fatal(e)
	}
	rec, rev, e := store.LoadOperationRecord(ctx, opID)
	if e != nil {
		t.Fatal(e)
	}
	rec.State = operation.StateInstalling
	if rev, e = store.SaveOperationRecord(ctx, rev, rec); e != nil {
		t.Fatal(e)
	}
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || e == nil {
		t.Fatal("ineligible operation silently accepted the decision", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("ineligible operation stopped work")
	}
	if owner.pendingSave == nil {
		t.Fatal("transient eligibility error dropped the retained receipt")
	}
	// Restore eligibility: the same decision is retried and consumed.
	rec.State = operation.StateDraining
	if _, e = store.SaveOperationRecord(ctx, rev, rec); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("retry after eligibility restore stopped work", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("cancel retry stopped work")
	}
	if owner.pendingSave != nil {
		t.Fatal("receipt retained after retry")
	}
	if sf, found, e := store.ReadPendingSaveFailure(ctx, opID); e != nil || found {
		t.Fatal("consumed decision still pending", found, sf, e)
	}
}

// TestLocalSaveFailureStorageErrorNeverStopsWork covers storage failure: a
// missing/unreadable ForceControl DB yields an error but never stops work and
// never drops the retained receipt.
func TestLocalSaveFailureStorageErrorNeverStopsWork(t *testing.T) {
	ctx, root, store, _, s, work, calls, owner, _ := localSaveFixture(t)
	if e := os.RemoveAll(root); e != nil {
		t.Fatal(e)
	}
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || e == nil {
		t.Fatal("storage failure did not surface an error", stopped, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("storage failure stopped work")
	}
	if owner.pendingSave == nil {
		t.Fatal("storage failure dropped the retained receipt")
	}
}

// TestLocalSaveFailureShutdownReleasesRetainedReceipt covers shutdown cleanup:
// the retained pending receipt is released and no longer actionable.
func TestLocalSaveFailureShutdownReleasesRetainedReceipt(t *testing.T) {
	ctx, _, _, _, s, work, calls, owner, receipt := localSaveFixture(t)
	s.Shutdown()
	if owner.pendingSave != nil {
		t.Fatal("shutdown retained the pending receipt")
	}
	if ok, e := s.ForceAfterSaveFailure(ctx, int64(os.Getpid()), true, receipt); ok || e != ErrForceConsent {
		t.Fatal("shutdown receipt still actionable", ok, e)
	}
	if *calls != 0 || work.Err() != nil {
		t.Fatal("shutdown stopped work")
	}
}

// TestLocalSaveFailureStopCallbackCleanupReentryNeverDeadlocks is the bounded
// regression for review finding 1: poll previously held o.mu while calling
// RequestForceUpdateStop/ForceAfterSaveFailure, whose stop callbacks reenter
// the owner via Shutdown -> CleanupPendingSave -> the same mutex. The poll runs
// in a goroutine so the regression fails (rather than hangs) on the deadlock
// while the test itself always unwinds.
func TestLocalSaveFailureStopCallbackCleanupReentryNeverDeadlocks(t *testing.T) {
	ctx, root, store, _, s, work, calls, owner, _ := localSaveFixture(t)
	if e := store.SubmitSaveDecision(ctx, owner.pendingSave.receiptID, state.ForceDecisionForce, true, time.Now); e != nil {
		t.Fatal(e)
	}
	// Reenter the owner exactly the way a production stop callback can:
	// Shutdown -> CleanupPendingSave.
	s.SetProcessForceStopRequester(func(string) {
		owner.CleanupPendingSave(s)
		*calls++
	})
	done := make(chan error, 1)
	go func() {
		stopped, e := owner.poll(ctx, s, root, store.InstallID())
		if e == nil && !stopped {
			e = errors.New("poll neither stopped nor failed")
		}
		done <- e
	}()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("reentrant cleanup broke the owned stop", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poll deadlocked: stop callback CleanupPendingSave blocked on the owner lock")
	}
	if *calls != 1 || work.Err() != context.Canceled {
		t.Fatal("owned interruption not applied exactly once", *calls, work.Err())
	}
	if owner.pendingSave != nil {
		t.Fatal("stop left the retained receipt behind")
	}
}

// TestLocalForceIntentStopCallbackCleanupReentryNeverDeadlocks is the bounded
// regression for review finding 1 on the confirmed-intent path: poll
// previously held o.mu across RequestForceUpdateStop, whose stop callback
// reenters the owner via Shutdown -> CleanupPendingSave.
func TestLocalForceIntentStopCallbackCleanupReentryNeverDeadlocks(t *testing.T) {
	root, store, id := forceLocalFixture(t)
	ctx := context.Background()
	s, _ := newServerWithStore(t)
	s.turnBroker = broker.New()
	calls := 0
	s.SetProcessForceStopRequester(func(string) { calls++ })
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil {
		t.Fatal("local force owner", owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration alone stopped agent", stopped, e)
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found || published != owner.agent {
		t.Fatal("agent registration", published, found, e)
	}
	work, _, release := s.turnBroker.BeginTurn(ctx, "conversation")
	defer release()
	now := time.Now()
	intent := state.ForceIntent{OpID: id, Agent: published, IssuedMS: now.UnixMilli(), ExpiresMS: now.Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	s.SetProcessForceStopRequester(func(string) {
		owner.CleanupPendingSave(s)
		calls++
	})
	done := make(chan error, 1)
	go func() {
		stopped, e := owner.poll(ctx, s, root, store.InstallID())
		if e == nil && !stopped {
			e = errors.New("poll neither stopped nor failed")
		}
		done <- e
	}()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("reentrant cleanup broke the owned stop", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poll deadlocked: stop callback CleanupPendingSave blocked on the owner lock")
	}
	if calls != 1 || work.Err() != context.Canceled {
		t.Fatal("owned interruption not applied exactly once", calls, work.Err())
	}
}

// TestLocalSaveFailureAmbiguousPublicationRetainsReceiptAndReconciles is the
// regression for review finding 2: publication previously minted the receipt
// ID and only stored pendingSave after Publish succeeded, so a committed write
// whose response was lost dropped the in-memory receipt entirely. Through a
// scoped seam the publication commits to the real store while the poll
// observes an error; the receipt and its exact ID must survive and later
// reconcile to the committed row without fresh IDs or overwrites.
func TestLocalSaveFailureAmbiguousPublicationRetainsReceiptAndReconciles(t *testing.T) {
	root, store, opID := forceLocalFixture(t)
	s, work, calls := failedSaveServer(t)
	ctx := context.Background()
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil {
		t.Fatal("local force owner", owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration poll stopped agent", stopped, e)
	}
	// Scoped seam: the write commits to the real store, then the caller
	// observes an error (response lost).
	owner.testPublishSaveFailure = func(ctx context.Context, c *state.ForceControl, p *pendingSaveFailure) error {
		if e := c.PublishSaveFailure(ctx, p.opID, owner.agent, p.receiptID, p.receipt.expires, time.Now); e != nil {
			return e
		}
		return errors.New("injected ambiguous publication: write committed, response lost")
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found {
		t.Fatal("agent registration", found, e)
	}
	intent := state.ForceIntent{OpID: opID, Agent: published, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	var receipt *ForceSaveFailure
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || !errors.As(e, &receipt) || *calls != 0 || work.Err() != nil {
		t.Fatal("ambiguous publication stopped work", stopped, e, *calls)
	}
	if owner.pendingSave == nil || owner.pendingSave.receipt != receipt || owner.pendingSave.opID != opID {
		t.Fatal("ambiguous publication lost the in-memory receipt", owner.pendingSave)
	}
	receiptID := owner.pendingSave.receiptID
	if receiptID == "" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(receiptID) {
		t.Fatal("retained receipt ID not crypto-length lowercase hex", receiptID)
	}
	// The committed publication is live in the real store under the exact
	// retained ID, and no consent was requested before publication.
	sf, found, e := store.ReadPendingSaveFailure(ctx, opID)
	if e != nil || !found || sf.ReceiptID != receiptID || sf.Decision != state.ForceDecisionPending || sf.Consumed {
		t.Fatal("committed publication not reconcilable", found, sf, e)
	}
	// The next poll reconciles the retained receipt against the committed row:
	// no fresh ID, no overwrite, no stop, and no storage failure action.
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 || work.Err() != nil {
		t.Fatal("reconcile poll stopped work", stopped, e)
	}
	if owner.pendingSave == nil || owner.pendingSave.receipt != receipt || owner.pendingSave.receiptID != receiptID {
		t.Fatal("reconcile dropped or changed the receipt/ID", owner.pendingSave)
	}
	// The reconciled receipt stays actionable: a confirmed Force decision
	// through the exact committed ID stops owned work exactly once.
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionForce, true, time.Now); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); !stopped || e != nil || *calls != 1 || work.Err() != context.Canceled {
		t.Fatal("reconciled decision did not stop owned work once", stopped, e, *calls, work.Err())
	}
	if owner.pendingSave != nil {
		t.Fatal("applied decision left a retained receipt")
	}
}

// TestLocalSaveFailurePublishFailureRetriesExactSameReceiptID covers the
// genuine storage-failure side of finding 2: a failed publication (nothing
// commits) still retains the receipt, and the next poll retries the exact
// same idempotent publication — never a fresh receipt ID, never an overwrite.
func TestLocalSaveFailurePublishFailureRetriesExactSameReceiptID(t *testing.T) {
	root, store, opID := forceLocalFixture(t)
	s, work, calls := failedSaveServer(t)
	ctx := context.Background()
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil {
		t.Fatal("local force owner", owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration poll stopped agent", stopped, e)
	}
	owner.testPublishSaveFailure = func(context.Context, *state.ForceControl, *pendingSaveFailure) error {
		return errors.New("injected publish failure")
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found {
		t.Fatal("agent registration", found, e)
	}
	intent := state.ForceIntent{OpID: opID, Agent: published, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	var receipt *ForceSaveFailure
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || !errors.As(e, &receipt) || *calls != 0 || work.Err() != nil {
		t.Fatal("publish failure stopped work", stopped, e, *calls)
	}
	if owner.pendingSave == nil || owner.pendingSave.receipt != receipt || owner.pendingSave.receiptID == "" {
		t.Fatal("publish failure lost the in-memory receipt", owner.pendingSave)
	}
	receiptID := owner.pendingSave.receiptID
	if sf, found, e := store.ReadPendingSaveFailure(ctx, opID); e != nil || found {
		t.Fatal("failed publication committed", found, sf, e)
	}
	// Retry with the real publication path: the exact same receipt ID commits.
	owner.testPublishSaveFailure = nil
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 || work.Err() != nil {
		t.Fatal("retry poll stopped work", stopped, e)
	}
	if owner.pendingSave == nil || owner.pendingSave.receiptID != receiptID {
		t.Fatal("retry minted a fresh receipt ID or dropped the receipt", owner.pendingSave)
	}
	sf, found, e := store.ReadPendingSaveFailure(ctx, opID)
	if e != nil || !found || sf.ReceiptID != receiptID || sf.Decision != state.ForceDecisionPending {
		t.Fatal("retry did not commit the exact publication", found, sf, e)
	}
	// The retried receipt is actionable through the exact same ID.
	if e := store.SubmitSaveDecision(ctx, receiptID, state.ForceDecisionForce, true, time.Now); e != nil {
		t.Fatal(e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); !stopped || e != nil || *calls != 1 || work.Err() != context.Canceled {
		t.Fatal("retried decision did not stop owned work once", stopped, e, *calls, work.Err())
	}
}

// TestLocalSaveFailureCleanupPreventsRetryResurrection covers finding 2's
// constraint that cleanup must prevent retry resurrection: after a failed
// publication and CleanupPendingSave, later polls must neither re-publish the
// decision nor retain a receipt, even with the publication seam active.
func TestLocalSaveFailureCleanupPreventsRetryResurrection(t *testing.T) {
	root, store, opID := forceLocalFixture(t)
	s, work, calls := failedSaveServer(t)
	ctx := context.Background()
	owner, e := s.newForceLocalOwner()
	if e != nil || owner == nil {
		t.Fatal("local force owner", owner, e)
	}
	if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil {
		t.Fatal("registration poll stopped agent", stopped, e)
	}
	owner.testPublishSaveFailure = func(context.Context, *state.ForceControl, *pendingSaveFailure) error {
		return errors.New("injected publish failure")
	}
	published, found, e := store.ReadForceAgent(ctx)
	if e != nil || !found {
		t.Fatal("agent registration", found, e)
	}
	intent := state.ForceIntent{OpID: opID, Agent: published, IssuedMS: time.Now().UnixMilli(), ExpiresMS: time.Now().Add(20 * time.Second).UnixMilli()}
	if e = store.SubmitForceIntent(ctx, intent, true, time.Now); e != nil {
		t.Fatal(e)
	}
	var receipt *ForceSaveFailure
	stopped, e := owner.poll(ctx, s, root, store.InstallID())
	if stopped || !errors.As(e, &receipt) || *calls != 0 || work.Err() != nil {
		t.Fatal("publish failure stopped work", stopped, e)
	}
	if owner.pendingSave == nil {
		t.Fatal("receipt not retained before cleanup")
	}
	owner.CleanupPendingSave(s)
	if owner.pendingSave != nil {
		t.Fatal("cleanup left the receipt retained")
	}
	if sf, found, e := store.ReadPendingSaveFailure(ctx, opID); e != nil || found {
		t.Fatal("cleanup resurrected the publication", found, sf, e)
	}
	for i := 0; i < 3; i++ {
		if stopped, e := owner.poll(ctx, s, root, store.InstallID()); stopped || e != nil || *calls != 0 || work.Err() != nil {
			t.Fatal("post-cleanup poll resurrected the stop", i, stopped, e)
		}
		if owner.pendingSave != nil {
			t.Fatal("post-cleanup poll resurrected the receipt", i)
		}
	}
}
