package server

import (
	"cercano/source/server/internal/broker"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/llm"
	"context"
	"encoding/json"
	"errors"
	"time"
)

// ForceSaveFailure is an opaque, process-local retry receipt. It does not grant
// network authority; the local-record layer must authorize the separate choice.
type ForceSaveFailure struct {
	owner   *Server
	turns   []broker.ActiveTurn
	expires time.Time
}

func (*ForceSaveFailure) Error() string {
	return "Could not save interruption history. Cancel leaves the agent running; Force anyway may leave incomplete history."
}
func sameForceTurns(a, b []broker.ActiveTurn) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const forceRequestedNote = "[host:force-update-requested] A force update was requested during this turn. It may interrupt active work; unfinished tool outcomes are unknown and changes may already have occurred. Do not automatically replay commands. This notice records the request, not proof that interruption or update completed."

func (s *Server) recordForceIntent(parent context.Context, turns []broker.ActiveTurn) error {
	if len(turns) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, forcePersistenceBudget)
	defer cancel()
	if s.forceSaveRecorder != nil {
		return s.forceSaveRecorder(ctx, turns)
	}
	if s.persistSvc == nil || s.persistSvc.Store() == nil {
		return errors.New("interruption history store unavailable")
	}
	store := s.persistSvc.Store()
	blocks, e := json.Marshal([]llm.Block{{Type: llm.BlockText, Text: forceRequestedNote}})
	if e != nil {
		return e
	}
	for _, turn := range turns {
		if !s.turnIsCurrent(turn.ConversationID, turn.Generation) {
			continue
		}
		if e = store.EnsureConversation(ctx, turn.ConversationID, "", ""); e != nil {
			return e
		}
		if e = store.Append(ctx, conversation.Turn{ConversationID: turn.ConversationID, Role: string(llm.RoleSystem), Content: forceRequestedNote, BlocksJSON: string(blocks)}); e != nil {
			return e
		}
	}
	return ctx.Err()
}
