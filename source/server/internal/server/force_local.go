package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"

	"cercano/source/server/internal/updatecoord/state"
)

// pendingSaveFailure retains the process-local save-failure receipt together
// with the crypto-random receipt ID under which its protected pending decision
// was published and the operation that consumed the original intent. The
// receipt and its ID are retained before the publication attempt so an
// ambiguous publication (write committed, response lost) keeps the in-memory
// receipt addressable: later polls reconcile through the same exact ID.
type pendingSaveFailure struct {
	receipt   *ForceSaveFailure
	receiptID string
	opID      int64
}

// forceLocalOwner is a Server-owned instance-bound poller. Owner state is only
// guarded by mu; process-local stop APIs are applied outside mu because their
// callbacks can reenter the owner (Shutdown -> CleanupPendingSave -> mu).
// Lock order is o.mu -> s.safeStopMu; it is never held across a stop callback.
type forceLocalOwner struct {
	mu                    sync.Mutex
	root, installID       string
	agent                 state.ForceAgent
	attemptedRegistration bool
	pendingSave           *pendingSaveFailure
	// dispatching marks a claimed process-local stop whose stop callback is
	// running outside o.mu. While set, no other poll may authorize another
	// stop, so a claimed interruption is applied at most once.
	dispatching bool
	closed      bool // permanent shutdown fence, guarded by mu
	// testPublishSaveFailure is a scoped test seam around the publication
	// call so an ambiguous commit (write committed, response lost) can be
	// reproduced against the real store. Never set in production.
	testPublishSaveFailure func(ctx context.Context, c *state.ForceControl, p *pendingSaveFailure) error
}

// pollStop is an authorized process-local interruption captured under o.mu
// (claim) and applied outside o.mu (dispatch). Capturing the immutable inputs
// under the lock and invoking callbacks outside it prevents both duplicate
// authorization and callback reentry deadlocks.
type pollStop struct {
	override *ForceSaveFailure // nil: confirmed intent stop; non-nil: confirmed save-failure override
	deadline time.Time         // intent or receipt deadline enforced across the dispatch
	opID     int64             // consumed intent operation, for save-failure publication
}

// newForceLocalOwner creates one immutable identity for this Server lifetime.
func (s *Server) newForceLocalOwner() (*forceLocalOwner, error) {
	if s == nil {
		return nil, nil
	}
	s.safeStopMu.Lock()
	defer s.safeStopMu.Unlock()
	if s.processForceStopRequester == nil {
		return nil, nil
	}
	if s.localForceControl == nil {
		a, e := state.NewForceAgent(int64(os.Getpid()))
		if e != nil {
			return nil, e
		}
		s.localForceControl = &forceLocalOwner{agent: a}
	}
	return s.localForceControl, nil
}

// poll drives instance-bound force stop control for this Server. It fully
// validates the instance identity before claiming anything, then authorizes at
// most one process-local interruption per invocation. Poll errors never stop
// work; only the specific Cancel/Force decision or confirmed force intent
// stops work. Callbacks are invoked outside o.mu so Shutdown/CleanupPendingSave
// reentry cannot deadlock.
func (o *forceLocalOwner) poll(ctx context.Context, s *Server, root, id string) (bool, error) {
	c, stop, e := o.claimPoll(ctx, s, root, id)
	if c != nil {
		defer c.Close()
	}
	if e != nil || stop == nil {
		return false, e
	}
	return o.applyStop(ctx, s, c, stop)
}

// claimPoll performs every state validation and authorization under o.mu and
// returns the claimed stop to apply outside the lock. It never invokes a stop
// callback while holding o.mu. A claimed stop clears dispatching when its
// dispatch completes; nothing here treats claim errors as stop requests.
func (o *forceLocalOwner) claimPoll(ctx context.Context, s *Server, root, id string) (*state.ForceControl, *pollStop, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, nil, nil
	}
	if o.installID != "" && (o.root != root || o.installID != id) {
		return nil, nil, state.ErrForceTarget
	}
	if o.dispatching {
		// A claimed dispatch is in flight; it owns the next stop. Never
		// authorize a second one concurrently.
		return nil, nil, nil
	}
	c, e := state.OpenExistingForceControl(ctx, root, id)
	if e != nil {
		return nil, nil, e
	}
	// From here on c is returned on every path so the caller can Close it.
	o.root = root
	o.installID = id
	if !o.attemptedRegistration {
		o.attemptedRegistration = true
		if e = c.RegisterAgent(ctx, o.agent); e != nil {
			return c, nil, e
		}
	}
	actual, found, e := c.Agent(ctx)
	if e != nil {
		return c, nil, e
	}
	if !found || actual != o.agent {
		return c, nil, state.ErrForceTarget
	}
	// Resolve any previously published save-failure decision first: the
	// original intent was already consumed, so Consume alone cannot reach it.
	if stop, e, done := o.claimPendingSave(ctx, s, c); done {
		return c, stop, e
	}
	intent, ok, e := c.Consume(ctx, o.agent, time.Now)
	if errors.Is(e, state.ErrForceReplay) {
		return c, nil, nil
	}
	if e != nil || !ok {
		return c, nil, e
	}
	o.dispatching = true
	return c, &pollStop{deadline: time.UnixMilli(intent.ExpiresMS), opID: intent.OpID}, nil
}

// applyStop applies a claimed process-local stop outside o.mu, bounded by the
// claim's own deadline across the whole dispatch, so the stop callback can
// reenter the owner (Shutdown -> CleanupPendingSave -> o.mu) without deadlock.
// dispatching is cleared exactly once, when the dispatch finishes; the claim
// cannot be re-applied after that.
func (o *forceLocalOwner) applyStop(ctx context.Context, s *Server, c *state.ForceControl, stop *pollStop) (stopped bool, err error) {
	defer func() {
		o.mu.Lock()
		o.dispatching = false
		o.mu.Unlock()
	}()
	stopCtx, cancel := context.WithDeadline(ctx, stop.deadline)
	defer cancel()
	if stop.override != nil {
		stopped, err = s.ForceAfterSaveFailure(stopCtx, o.agent.PID, true, stop.override)
		if !stopped {
			// The decision was consumed and this receipt can never be applied
			// through the pending path again; release the server-side copy
			// rather than leak it.
			s.CancelForceSaveFailure(stop.override)
		}
		// An expiry between commit and dispatch must not stop the agent.
		// Consumption remains durable so an interrupted observer cannot
		// replay it later.
		return stopped, err
	}
	stopped, err = s.RequestForceUpdateStop(stopCtx, o.agent.PID, true)
	var receipt *ForceSaveFailure
	if !stopped && errors.As(err, &receipt) {
		if e := o.retainSaveFailure(ctx, s, c, stop.opID, receipt); e != nil {
			return false, err
		}
	}
	return stopped, err
}

// claimPendingSave validates, reads and consumes a matching pending
// save-failure decision through ForceControl and claims the resulting
// process-local action. done reports whether the poll should return now;
// when false, polling continues to the normal intent path. A cancel decision
// is applied under o.mu (it never invokes a stop callback); a confirmed force
// decision is claimed under o.mu and dispatched outside it. No response,
// expiry, or storage failure ever stops work. Caller holds o.mu.
func (o *forceLocalOwner) claimPendingSave(ctx context.Context, s *Server, c *state.ForceControl) (*pollStop, error, bool) {
	p := o.pendingSave
	if p == nil {
		return nil, nil, false
	}
	if time.Now().After(p.receipt.expires) {
		// Bounded expiry: the receipt can never be honored, so release it.
		// Release also cancels the server-side copy, preventing a later
		// ambiguous-publication retry from resurrecting this receipt.
		o.releasePendingSaveLocked(s)
		return nil, nil, true
	}
	sf, found, e := c.PendingSaveFailure(ctx, p.opID)
	if e != nil {
		return nil, e, true // storage failure never stops work
	}
	if !found {
		// The retained publication has not committed (a previous attempt
		// failed or was ambiguous). Retry the exact same idempotent
		// publication under the same receipt ID: never a fresh ID, never an
		// overwrite. Cleanup/expiry above keeps this from resurrecting a
		// released receipt.
		if e := o.publishPendingSaveFailure(ctx, c, p); e != nil {
			return nil, e, true // publication failure never stops work
		}
		return nil, nil, true
	}
	if sf.ReceiptID != p.receiptID || sf.Agent != o.agent || sf.OpID != p.opID {
		// Never act on a decision that does not match the retained receipt.
		o.releasePendingSaveLocked(s)
		return nil, state.ErrSaveFailureWrongOp, true
	}
	if sf.Decision != state.ForceDecisionCancel && !(sf.Decision == state.ForceDecisionForce && sf.Confirmed) {
		return nil, nil, true // decision still pending; keep the receipt and wait
	}
	// Consume the decision with a fresh clock so the store re-checks the
	// deadline at consumption time, not against a stale sampled poll time.
	if e := c.ConsumeSaveDecision(ctx, p.receiptID, o.agent, time.Now); e != nil {
		return nil, e, true // consume failure never stops work
	}
	receipt := p.receipt
	o.pendingSave = nil
	if sf.Decision == state.ForceDecisionCancel {
		s.CancelForceSaveFailure(receipt)
		return nil, nil, true
	}
	o.dispatching = true
	return &pollStop{override: receipt, deadline: receipt.expires}, nil, true
}

// retainSaveFailure retains the process-local save-failure receipt with a
// fresh crypto-random receipt ID before publishing its protected pending
// decision through ForceControl. Retention precedes publication so an
// ambiguous commit (write succeeded, response lost) keeps the in-memory
// receipt addressable: the next poll reconciles by retrying the exact same
// ID. A publish/storage failure never stops work; the receipt stays
// actionable through the explicit cancel/override APIs.
func (o *forceLocalOwner) retainSaveFailure(ctx context.Context, s *Server, c *state.ForceControl, opID int64, receipt *ForceSaveFailure) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		s.CancelForceSaveFailure(receipt)
		return nil
	}
	if o.pendingSave != nil {
		// Never duplicate or overwrite a retained receipt.
		return nil
	}
	p := &pendingSaveFailure{receipt: receipt, opID: opID}
	if e := o.publishPendingSaveFailure(ctx, c, p); e != nil {
		// Keep the retained receipt and its ID: a retry reconciles the
		// ambiguous publication through the same ID instead of minting a
		// fresh one.
		o.pendingSave = p
		return e
	}
	o.pendingSave = p
	return nil
}

// publishPendingSaveFailure publishes the protected pending decision for the
// retained process-local receipt under one crypto-random receipt ID, bounded
// by the receipt's own expiry, idempotently. Caller holds o.mu.
func (o *forceLocalOwner) publishPendingSaveFailure(ctx context.Context, c *state.ForceControl, p *pendingSaveFailure) error {
	if p.receiptID == "" {
		var raw [32]byte
		if _, e := rand.Read(raw[:]); e != nil {
			return e
		}
		p.receiptID = hex.EncodeToString(raw[:])
	}
	if o.testPublishSaveFailure != nil {
		return o.testPublishSaveFailure(ctx, c, p)
	}
	return c.PublishSaveFailure(ctx, p.opID, o.agent, p.receiptID, p.receipt.expires, time.Now)
}

// releasePendingSaveLocked clears any retained pending save-failure receipt
// and releases the process-local server-side copy so it cannot be applied by
// the pending path again. Caller holds o.mu. Lock order stays o.mu ->
// s.safeStopMu.
func (o *forceLocalOwner) releasePendingSaveLocked(s *Server) {
	p := o.pendingSave
	o.pendingSave = nil
	if p != nil {
		s.CancelForceSaveFailure(p.receipt)
	}
}

// CleanupPendingSave releases the Server's pending save-failure receipt, if
// any, so Shutdown never leaks an actionable in-memory stop. Safe to call
// from a stop callback or concurrently with a poll.
func (o *forceLocalOwner) CleanupPendingSave(s *Server) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.releasePendingSaveLocked(s)
}
