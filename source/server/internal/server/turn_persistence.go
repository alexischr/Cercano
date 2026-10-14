package server

import (
	"cercano/source/server/internal/llm"
	"context"
	"log"
	"time"
)

const forcePersistenceBudget = 2 * time.Second

// persistCurrentTurn is the host generation fence shared by turn persistence.
func (s *Server) persistCurrentTurn(ctx context.Context, convID string, gen uint64, m llm.Message) {
	if !s.turnIsCurrent(convID, gen) {
		return
	}
	s.safeStopMu.Lock()
	forced, deadline := s.forceStopCommitted, s.forcePersistenceDeadline
	s.safeStopMu.Unlock()
	if forced {
		// All final writes share one force-commit deadline; each message cannot
		// restart a grace period. Preserve request metadata, not cancellation.
		final, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		defer cancel()
		if final.Err() != nil {
			log.Print("[updates] final turn persistence deadline exhausted; saved history may be incomplete")
			return
		}
		if !s.turnIsCurrent(convID, gen) {
			return
		}
		s.persistTurn(final, convID, m)
		return
	}
	s.persistTurn(ctx, convID, m)
}
