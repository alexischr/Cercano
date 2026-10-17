package server

import (
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/runner"
	"cercano/source/server/pkg/proto"
	"context"
	"google.golang.org/grpc"
	"testing"
	"time"
)

func TestCompletionWakePublishesFinalForAttachment(t *testing.T) {
	s, _ := newServerWithStore(t)
	seedCompletion(t, s, "one")
	s.inProcessRunner = completionProbeRunner{run: func(context.Context, runner.Request, runner.PermissionRequester) (runner.Result, error) {
		return runner.Result{FinalText: "Child finished successfully", Model: "fixture"}, nil
	}}
	s.tryTaskCompletionWake(t.Context(), "owner")
	replay, _, detach := s.turnBroker.Attach("owner")
	defer detach()
	for _, event := range replay {
		if event.Kind == runner.EventDone && event.Result.FinalText == "Child finished successfully" {
			return
		}
	}
	t.Fatal("background final response never reached the main-conversation attachment")
}

func TestBackgroundFinalArrivesThroughSDKAttachment(t *testing.T) {
	s, _ := newServerWithStore(t)
	seedCompletion(t, s, "one")
	s.inProcessRunner = completionProbeRunner{run: func(context.Context, runner.Request, runner.PermissionRequester) (runner.Result, error) {
		if err := s.persistSvc.Store().Append(t.Context(), conversation.Turn{ID: "saved-answer", ConversationID: "owner", Role: "assistant", Content: "automatic reply", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		return runner.Result{FinalText: "automatic reply", Model: "fixture"}, nil
	}}
	client := taskObserverClient(t, s)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	updates, err := client.AttachConversation(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	s.tryTaskCompletionWake(t.Context(), "owner")
	for {
		select {
		case update := <-updates:
			if update.Err != nil {
				t.Fatal(update.Err)
			}
			if final := update.Response.GetFinalResponse(); final != nil {
				if update.Response.PersistedTurnId != "saved-answer" || final.Output != "automatic reply" || !update.Response.BackgroundTurn || update.Response.TurnEpoch == "" || update.Response.EventSequence == 0 {
					t.Fatalf("final=%v", update.Response)
				}
				cancel()
				for range updates {
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

type attachmentProbe struct {
	grpc.ServerStream
	ctx  context.Context
	send func(*proto.StreamProcessResponse) error
}

func (p *attachmentProbe) Context() context.Context                         { return p.ctx }
func (p *attachmentProbe) Send(response *proto.StreamProcessResponse) error { return p.send(response) }
func TestAttachmentPagedReplayDoesNotDropAndResumesCursor(t *testing.T) {
	s, _ := newTestServer()
	_, gen, release, ok := s.turnBroker.TryBeginTurn(t.Context(), "owner")
	if !ok {
		t.Fatal("idle lease denied")
	}
	defer release()
	for i := 0; i < 300; i++ {
		s.turnBroker.Publish("owner", gen, runner.Event{Kind: runner.EventToken, Text: "x"})
	}
	meta := s.turnBroker.TurnMetadata("owner", gen)
	for _, after := range []uint64{0, 298} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		count := after
		probe := &attachmentProbe{ctx: ctx, send: func(response *proto.StreamProcessResponse) error {
			count++
			if response.EventSequence != count || !response.BackgroundTurn {
				t.Fatalf("unordered replay=%v", response)
			}
			if count == 300 {
				cancel()
			}
			return nil
		}}
		_ = s.AttachConversation(&proto.AttachConversationRequest{ConversationId: "owner", AfterTurnEpoch: meta.TurnEpoch, AfterTurnGeneration: gen, AfterEventSequence: after}, probe)
		cancel()
		if count != 300 {
			t.Fatalf("lost events: %d", count)
		}
	}
}
