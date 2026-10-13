package server

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"cercano/source/server/internal/updatecoord/state"
)

// forceLocalOwner belongs to one Server lifetime and one enrolled installation.
// Its boot identity is never loaded from a request or adopted from old disk state.
type forceLocalOwner struct {
	mu                    sync.Mutex
	root, installID       string
	agent                 state.ForceAgent
	attemptedRegistration bool
}

func (s *Server) newForceLocalOwner() (*forceLocalOwner, error) {
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

// poll consumes permission before invoking process-local shutdown. Opening never
// creates/migrates updater state. A missing/old DB leaves the agent running.
// Register is attempted once: ambiguous commit is reconciled by readback on the
// next poll, never by overwriting a replacement agent's session repeatedly.
func (o *forceLocalOwner) poll(ctx context.Context, s *Server, root, id string) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.installID != "" && (o.root != root || o.installID != id) {
		return false, state.ErrForceTarget
	}

	c, e := state.OpenExistingForceControl(ctx, root, id)
	if e != nil {
		return false, e
	}
	defer c.Close()
	o.root = root
	o.installID = id
	if !o.attemptedRegistration {
		o.attemptedRegistration = true
		if e = c.RegisterAgent(ctx, o.agent); e != nil {
			return false, e
		}
	}
	actual, found, e := c.Agent(ctx)
	if e != nil {
		return false, e
	}
	if !found || actual != o.agent {
		return false, state.ErrForceTarget
	}
	intent, ok, e := c.Consume(ctx, o.agent, time.Now)
	if errors.Is(e, state.ErrForceReplay) {
		return false, nil
	}
	if e != nil || !ok {
		return false, e
	}
	// An expiry between commit and dispatch must not stop the agent. Consumption
	// remains durable so an interrupted observer cannot replay it later.
	stopCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(intent.ExpiresMS))
	defer cancel()
	return s.RequestForceUpdateStop(stopCtx, o.agent.PID, true)
}
