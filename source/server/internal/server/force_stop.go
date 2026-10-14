package server

import (
	"cercano/source/server/internal/broker"
	"context"
	"errors"
	"os"
	"time"
)

var ErrForceConsent = errors.New("force update requires explicit interruption confirmation")
var ErrForceIdentity = errors.New("force update target no longer matches this agent")
var ErrForceUnavailable = errors.New("owned force-stop requester is not configured")

// SetProcessForceStopRequester binds a process-local owner, never a PID-kill
// fallback. It must stop only owned work/resources and let terminal persistence
// finish within a bounded cleanup budget. Configured before serving requests.
func (s *Server) SetProcessForceStopRequester(fn func(string)) {
	s.safeStopMu.Lock()
	s.processForceStopRequester = fn
	s.safeStopMu.Unlock()
}

// RequestForceUpdateStop commits an explicitly confirmed owned interruption.
// expectedPID is a stale-target guard, NOT caller authentication. The caller
// independently verifies installation/process identity and obtains user consent.
// This internal entrypoint is not yet a public RPC. Acceptance means requested,
// NOT idle, terminated, installed or healthy. The updater must prove actual exit
// before replacing/selecting files. No interrupted command is replayed here.
func (s *Server) RequestForceUpdateStop(ctx context.Context, pid int64, confirmed bool) (bool, error) {
	return s.requestForceStop(ctx, pid, confirmed, nil)
}
func (s *Server) ForceAfterSaveFailure(ctx context.Context, pid int64, confirmed bool, receipt *ForceSaveFailure) (bool, error) {
	if receipt == nil {
		return false, ErrForceConsent
	}
	return s.requestForceStop(ctx, pid, confirmed, receipt)
}
func (s *Server) CancelForceSaveFailure(receipt *ForceSaveFailure) {
	s.safeStopMu.Lock()
	defer s.safeStopMu.Unlock()
	if s.pendingForceSave == receipt {
		s.pendingForceSave = nil
	}
}
func (s *Server) requestForceStop(ctx context.Context, expectedPID int64, confirmed bool, override *ForceSaveFailure) (bool, error) {
	if ctx == nil || !confirmed {
		return false, ErrForceConsent
	}
	if expectedPID <= 0 || expectedPID != int64(os.Getpid()) {
		return false, ErrForceIdentity
	}
	s.safeStopMu.Lock()
	lockHeld := true
	defer func() {
		if lockHeld {
			s.safeStopMu.Unlock()
		}
	}()
	if override != nil && override.owner != s {
		return false, ErrForceConsent
	}
	if s.forceStopCommitted {
		return true, nil
	}
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if s.processForceStopRequester == nil {
		return false, ErrForceUnavailable
	}
	stop := s.processForceStopRequester
	if e := s.updateWork.sealForForce(ctx); e != nil {
		return false, e
	}
	committed := false
	defer func() {
		if !committed {
			s.updateWork.mu.Lock()
			s.updateWork.paused = false
			s.updateWork.notifyLocked()
			s.updateWork.mu.Unlock()
		}
	}()
	var turns []broker.ActiveTurn
	var release func()
	if s.turnBroker != nil {
		var e error
		turns, release, e = s.turnBroker.PrepareInterruption(ctx)
		if e != nil {
			return false, e
		}
		defer release()
	}
	if override != nil {
		if s.pendingForceSave != override || override.owner != s || time.Now().After(override.expires) || !sameForceTurns(turns, override.turns) {
			return false, ErrForceConsent
		}
		s.pendingForceSave = nil
	} else {
		s.pendingForceSave = nil
		if e := s.recordForceIntent(ctx, turns); e != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			receipt := &ForceSaveFailure{owner: s, turns: append([]broker.ActiveTurn(nil), turns...), expires: time.Now().Add(30 * time.Second)}
			s.pendingForceSave = receipt
			return false, receipt
		}
	}
	if e := ctx.Err(); e != nil {
		return false, e
	}
	committed = true
	// Once sealed, cancellation cannot reopen admission or undo interruption.
	s.forceStopCommitted = true
	s.forcePersistenceDeadline = time.Now().Add(forcePersistenceBudget)
	if s.turnBroker != nil {
		s.turnBroker.InterruptActiveTurns()
	}

	// Parked registrations must observe the committed interruption before the
	// owner drains handlers. Release is idempotent; its defer covers refusals.
	if release != nil {
		release()
	}
	// Final persistence may acquire safeStopMu during the owner callback.
	// Invoke the callback captured under the lock, never a mutable field.
	lockHeld = false
	s.safeStopMu.Unlock()
	stop("confirmed force update: owned work interrupted; no automatic replay")

	return true, nil
}
