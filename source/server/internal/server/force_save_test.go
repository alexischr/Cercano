package server

import (
	"cercano/source/server/internal/broker"
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func failedSaveServer(t *testing.T) (*Server, context.Context, *int) {
	t.Helper()
	s := &Server{turnBroker: broker.New()}
	work, _, release := s.turnBroker.BeginTurn(context.Background(), "active")
	t.Cleanup(release)
	calls := new(int)
	s.SetProcessForceStopRequester(func(string) { *calls++ })
	s.forceSaveRecorder = func(context.Context, []broker.ActiveTurn) error { return errors.New("injected full disk") }
	return s, work, calls
}
func TestForceSaveFailureCancelOrExplicitOverride(t *testing.T) {
	s, work, calls := failedSaveServer(t)
	pid := int64(os.Getpid())
	ctx := context.Background()
	ok, e := s.RequestForceUpdateStop(ctx, pid, true)
	var receipt *ForceSaveFailure
	if ok || !errors.As(e, &receipt) || work.Err() != nil || *calls != 0 {
		t.Fatal("save failure interrupted work", ok, e, work.Err())
	}
	release, e := s.updateWork.enter()
	if e != nil {
		t.Fatal("failure left admission closed", e)
	}
	release()
	s.CancelForceSaveFailure(receipt)
	if ok, e = s.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent {
		t.Fatal("canceled choice replayed", ok, e)
	}
	if work.Err() != nil || *calls != 0 {
		t.Fatal("cancel stopped work")
	}
	_, e = s.RequestForceUpdateStop(ctx, pid, true)
	if !errors.As(e, &receipt) {
		t.Fatal(e)
	}
	if ok, e = s.ForceAfterSaveFailure(ctx, pid, false, receipt); ok || e != ErrForceConsent {
		t.Fatal("unconfirmed override", ok, e)
	}
	if ok, e = s.ForceAfterSaveFailure(ctx, pid, true, receipt); !ok || e != nil || *calls != 1 || work.Err() != context.Canceled {
		t.Fatal("explicit override failed", ok, e, *calls, work.Err())
	}
}
func TestForceSaveOverrideRejectsNewWorkAndForeignReceipt(t *testing.T) {
	s, _, calls := failedSaveServer(t)
	ctx := context.Background()
	pid := int64(os.Getpid())
	_, e := s.RequestForceUpdateStop(ctx, pid, true)
	var receipt *ForceSaveFailure
	if !errors.As(e, &receipt) {
		t.Fatal(e)
	}
	other, otherWork, otherCalls := failedSaveServer(t)
	if ok, e := other.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent || otherWork.Err() != nil || *otherCalls != 0 {
		t.Fatal("foreign receipt accepted", ok, e)
	}
	later, _, release := s.turnBroker.BeginTurn(ctx, "active")
	defer release()
	if ok, e := s.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent || later.Err() != nil || *calls != 0 {
		t.Fatal("stale work consent reused", ok, e)
	}
}
func TestForceSaveExpiredReceiptRejection(t *testing.T) {
	s, work, calls := failedSaveServer(t)
	ctx := context.Background()
	pid := int64(os.Getpid())

	// Get a receipt
	_, e := s.RequestForceUpdateStop(ctx, pid, true)
	var receipt *ForceSaveFailure
	if !errors.As(e, &receipt) {
		t.Fatal(e)
	}

	// Manually expire the receipt
	receipt.expires = time.Now().Add(-1 * time.Hour)

	// Try to use the expired receipt - should fail
	if ok, e := s.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent {
		t.Fatal("expired receipt accepted", ok, e)
	}

	// Work should still be running and admission should be open
	if work.Err() != nil || *calls != 0 {
		t.Fatal("expired receipt stopped work", work.Err(), *calls)
	}

	// Should be able to start new work
	release, e := s.updateWork.enter()
	if e != nil {
		t.Fatal("expired receipt sealed admission", e)
	}
	release()
}

func TestForceSaveForeignReceiptRejectionAfterCommit(t *testing.T) {
	// First server that generates the receipt
	s1, work1, calls1 := failedSaveServer(t)
	ctx := context.Background()
	pid := int64(os.Getpid())

	// Get a receipt from first server
	_, e := s1.RequestForceUpdateStop(ctx, pid, true)
	var receipt *ForceSaveFailure
	if !errors.As(e, &receipt) {
		t.Fatal(e)
	}

	// Second server that should not accept the receipt
	s2, work2, calls2 := failedSaveServer(t)

	// Foreign receipt should be rejected
	if ok, e := s2.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent || work2.Err() != nil || *calls2 != 0 {
		t.Fatal("foreign receipt accepted", ok, e)
	}

	// Now commit the receipt on the original server
	if ok, e := s1.ForceAfterSaveFailure(ctx, pid, true, receipt); !ok || e != nil || *calls1 != 1 || work1.Err() != context.Canceled {
		t.Fatal("original server failed to commit", ok, e, *calls1, work1.Err())
	}

	// Foreign receipt should still be rejected after original commit
	if ok, e := s2.ForceAfterSaveFailure(ctx, pid, true, receipt); ok || e != ErrForceConsent || work2.Err() != nil || *calls2 != 0 {
		t.Fatal("foreign receipt accepted after original commit", ok, e)
	}
}

func TestForceSaveNoWorkAndMissingStore(t *testing.T) {
	s := &Server{}
	calls := 0
	s.SetProcessForceStopRequester(func(string) { calls++ })
	if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); !ok || e != nil || calls != 1 {
		t.Fatal("no-work update required save", ok, e)
	}
	s, work, counter := failedSaveServer(t)
	s.forceSaveRecorder = nil
	if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); ok || e == nil || work.Err() != nil || *counter != 0 {
		t.Fatal("missing store ignored", ok, e)
	}
}

// A registration already parked during the save must be able to finish while
// the owner callback drains handlers, not only after that callback returns.
func TestForceSaveCallbackDrainsParkedRegistration(t *testing.T) {
	s, _, _ := failedSaveServer(t)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	s.forceSaveRecorder = func(context.Context, []broker.ActiveTurn) error {
		started := make(chan struct{})
		go func() {
			close(started)
			_, _, release := s.turnBroker.BeginTurn(parent, "waiting")
			release()
			close(done)
		}()
		<-started
		select {
		case <-done:
			return errors.New("registration bypassed preparation barrier")
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	}
	drained := false
	unlocked := false
	s.SetProcessForceStopRequester(func(string) {
		unlocked = s.safeStopMu.TryLock()
		if unlocked {
			s.safeStopMu.Unlock()
		}
		select {
		case <-done:
			drained = true
		case <-time.After(500 * time.Millisecond):
		}
	})
	ok, err := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registration did not clean up")
	}
	if !ok || err != nil || !unlocked || !drained {
		t.Fatalf("callback could not drain: accepted=%v err=%v mutexReleased=%v registrationReleased=%v", ok, err, unlocked, drained)
	}
}
