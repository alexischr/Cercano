package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/llm"
)

func TestForceInterruptedCurrentTurnPersistsBeforeResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.db")
	s, store := newServerWithStore(t, path)
	ctx, gen, release := s.beginTurn(context.Background(), "force-persist")
	defer release()
	if e := store.EnsureConversation(ctx, "force-persist", "", "test-model"); e != nil {
		t.Fatal(e)
	}
	s.persistCurrentTurn(ctx, "force-persist", gen, llm.Message{Role: llm.RoleUser, Blocks: []llm.Block{{Type: llm.BlockText, Text: "owned fixture prompt"}}})
	s.SetProcessForceStopRequester(func(string) {}) // No process action.
	if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if ctx.Err() != context.Canceled || !s.turnIsCurrent("force-persist", gen) {
		t.Fatal("force fixture did not retain canceled current generation")
	}
	final := llm.Message{Role: llm.RoleAssistant, Blocks: []llm.Block{{Type: llm.BlockText, Text: "Interrupted; do not replay owned fixture work."}}}
	s.persistCurrentTurn(ctx, "force-persist", gen, final)
	turns, e := store.GetTurns(context.Background(), "force-persist")
	if e != nil || len(turns) != 2 {
		t.Fatalf("interruption persistence lost: turns=%+v err=%v", turns, e)
	}
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := conversation.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	turns, e = reopened.GetTurns(context.Background(), "force-persist")
	if e != nil || len(turns) != 2 {
		t.Fatal("reopened history lost interruption", turns, e)
	}
	var blocks []llm.Block
	if e = json.Unmarshal([]byte(turns[1].BlocksJSON), &blocks); e != nil || len(blocks) != 1 || blocks[0].Text != final.Blocks[0].Text {
		t.Fatal("changed persisted interruption", blocks, e)
	}
}
func TestForcePersistenceDoesNotReviveSupersededGeneration(t *testing.T) {
	s, store := newServerWithStore(t)
	old, oldGen, oldRelease := s.beginTurn(context.Background(), "fenced")
	defer oldRelease()
	if e := store.EnsureConversation(context.Background(), "fenced", "", "test-model"); e != nil {
		t.Fatal(e)
	}
	_, gen, release := s.beginTurn(context.Background(), "fenced")
	defer release()
	s.SetProcessForceStopRequester(func(string) {})
	if ok, e := s.RequestForceUpdateStop(context.Background(), int64(os.Getpid()), true); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if gen == oldGen {
		t.Fatal("not superseded")
	}
	s.persistCurrentTurn(old, "fenced", oldGen, llm.Message{Role: llm.RoleAssistant, Blocks: []llm.Block{{Type: llm.BlockText, Text: "stale"}}})
	turns, e := store.GetTurns(context.Background(), "fenced")
	if e != nil || len(turns) != 0 {
		t.Fatal("stale generation persisted", turns, e)
	}
}

func TestForcePersistenceDeadlineAndOrdinaryCancellation(t *testing.T) {
	for _, forced := range []bool{false, true} {
		s, store := newServerWithStore(t)
		ctx, gen, release := s.beginTurn(context.Background(), "bounded")
		defer release()
		if e := store.EnsureConversation(context.Background(), "bounded", "", "test-model"); e != nil {
			t.Fatal(e)
		}
		s.turnBroker.InterruptActiveTurns()
		if forced {
			s.safeStopMu.Lock()
			s.forceStopCommitted = true
			s.forcePersistenceDeadline = time.Now().Add(-time.Second)
			s.safeStopMu.Unlock()
		}
		s.persistCurrentTurn(ctx, "bounded", gen, llm.Message{Role: llm.RoleAssistant, Blocks: []llm.Block{{Type: llm.BlockText, Text: "must not write"}}})
		turns, e := store.GetTurns(context.Background(), "bounded")
		if e != nil || len(turns) != 0 {
			t.Fatal("expired/ordinary canceled write revived", forced, turns, e)
		}
	}
}
