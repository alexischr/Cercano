package server

import (
	"context"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	runnersvc "cercano/source/server/internal/runner"
	"cercano/source/server/pkg/proto"
)

func TestGetContextUsage_CompactionCannotReplaceRequest(t *testing.T) {
	for _, requestFirst := range []bool{true, false} {
		name := "history-first"
		if requestFirst {
			name = "request-first"
		}
		t.Run(name, func(t *testing.T) {
			s, store := newServerWithStore(t)
			ctx := context.Background()
			if err := store.EnsureConversation(ctx, "c", "/p", "m"); err != nil {
				t.Fatal(err)
			}
			request := func() {
				(&brokerSink{server: s, broker: s.turnBroker, conv: "c"}).RecordRequestAccounting(runnersvc.RequestAccounting{MessageTokens: 864125, EstimatedRequestTokens: 887278, ContextWindow: 1050000, ContextWindowKnown: true})
			}
			history := func() {
				if err := store.SaveContextUsage(ctx, conversation.ContextUsage{ConversationID: "c", Source: "compaction", TokensUsed: 4936958, RawTokens: 7472891, ComputedAt: time.Now().Add(time.Minute)}); err != nil {
					t.Fatal(err)
				}
			}
			if requestFirst {
				request()
				history()
			} else {
				history()
				request()
			}
			// Simulate stale in-process accounting: a worker's durable snapshot wins.
			s.recordRequestAccounting("c", requestAccountingSnapshot{EstimatedRequestTokens: 1000, ContextWindow: 128000})
			for _, read := range []func(context.Context, *proto.GetContextUsageRequest) (*proto.GetContextUsageResponse, error){s.GetContextUsage, s.persistSvc.GetContextUsage} {
				got, err := read(ctx, &proto.GetContextUsageRequest{ConversationId: "c"})
				if err != nil {
					t.Fatal(err)
				}
				if got.TokensUsed != 887278 || got.MessageTokens != 864125 || got.RawTokens != 7472891 || got.ModelMax != 1050000 || got.UsageSource != "snapshot" {
					t.Fatalf("mixed request and history accounting: %v", got)
				}
			}
		})
	}
}

func TestGetContextUsage_LegacyCompactionIsNotARequest(t *testing.T) {
	s, store := newServerWithStore(t)
	ctx := context.Background()
	if err := store.EnsureConversation(ctx, "c", "/p", "m"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveContextUsage(ctx, conversation.ContextUsage{ConversationID: "c", Source: "compaction", TokensUsed: 4936958, RawTokens: 7472891}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetContextUsage(ctx, &proto.GetContextUsageRequest{ConversationId: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageSource != "none" || got.TokensUsed != 0 || got.EstimatedRequestTokens != 0 || got.RawTokens != 7472891 {
		t.Fatalf("history presented as request: %v", got)
	}
}
