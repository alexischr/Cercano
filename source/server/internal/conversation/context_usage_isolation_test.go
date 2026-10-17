package conversation

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestContextUsage_RequestSurvivesCompactionAndReopen(t *testing.T) {
	for _, requestFirst := range []bool{true, false} {
		name := "history-first"
		if requestFirst {
			name = "request-first"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "usage.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := s.EnsureConversation(ctx, "c", "/p", "m"); err != nil {
				t.Fatal(err)
			}
			request := ContextUsage{ConversationID: "c", Source: "turn", TokensUsed: 864125, MessageTokens: 864125, EstimatedRequest: 887278, ContextWindow: 1050000, Model: "m", ComputedAt: time.Unix(100, 0)}
			history := ContextUsage{ConversationID: "c", Source: "compaction", TokensUsed: 4936958, RawTokens: 7472891, ComputedAt: time.Unix(200, 0)}
			writes := []ContextUsage{history, request}
			if requestFirst {
				writes = []ContextUsage{request, history}
			}
			for _, u := range writes {
				if err := s.SaveContextUsage(ctx, u); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, ok, err := s.GetContextUsage(ctx, "c")
			if err != nil || !ok {
				t.Fatalf("get: %v %v", ok, err)
			}
			if got.Source != "turn" || got.EstimatedRequest != 887278 || got.TokensUsed != 864125 || !got.ComputedAt.Equal(request.ComputedAt) {
				t.Fatalf("request replaced by history: %+v", got)
			}
			if got.RawTokens != history.RawTokens {
				t.Fatalf("raw history not preserved: %+v", got)
			}
		})
	}
}

func TestContextUsage_MigratesLegacyRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.EnsureConversation(ctx, "c", "/p", "m"); err != nil {
		t.Fatal(err)
	}
	// Emulate the pre-fix writer, including non-default runtime identity fields.
	_, err = s.(*sqliteStore).db.Exec(`INSERT INTO conversation_context_usage
 (conversation_id,source,tokens_used,estimated_request,computed_at,provider,runtime_instance_id)
 VALUES ('c','turn',864125,887278,100,'llama_server','runtime-1')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// A separate worker connection must not clobber the migrated request.
	worker, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if err := worker.SaveContextUsage(ctx, ContextUsage{ConversationID: "c", Source: "compaction", TokensUsed: 4900000}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetContextUsage(ctx, "c")
	if err != nil || !ok || got.EstimatedRequest != 887278 || got.Provider != "llama_server" || got.RuntimeInstanceID != "runtime-1" {
		t.Fatalf("lost legacy request: %+v ok=%v err=%v", got, ok, err)
	}
}
