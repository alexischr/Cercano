package server

import (
	"context"
	"errors"
	"os"
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
func (s *Server) RequestForceUpdateStop(ctx context.Context, expectedPID int64, confirmed bool) (bool, error) {
	if ctx == nil || !confirmed {
		return false, ErrForceConsent
	}
	if expectedPID <= 0 || expectedPID != int64(os.Getpid()) {
		return false, ErrForceIdentity
	}
	s.safeStopMu.Lock()
	defer s.safeStopMu.Unlock()
	if s.forceStopCommitted {
		return true, nil
	}
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if s.processForceStopRequester == nil {
		return false, ErrForceUnavailable
	}
	if e := s.updateWork.sealForForce(ctx); e != nil {
		return false, e
	}
	// Once sealed, cancellation cannot reopen admission or undo interruption.
	s.forceStopCommitted = true
	if s.turnBroker != nil {
		s.turnBroker.InterruptActiveTurns()
	}
	s.processForceStopRequester("confirmed force update: owned work interrupted; no automatic replay")
	return true, nil
}
