package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/taskrun"
	"cercano/source/server/pkg/proto"
	wire "google.golang.org/protobuf/proto"
)

func TestWorkerTaskBridgeOwnershipAndLifetime(t *testing.T) {
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	release := make(chan struct{})
	started := make(chan dispatch.Spec, 1)
	manager, err := taskrun.New(store.(conversation.TaskStore), func(ctx context.Context, spec dispatch.Spec) (dispatch.Result, error) {
		started <- spec
		select {
		case <-release:
			return dispatch.Result{Text: "finished"}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	ownerCtx, cancelOwner := context.WithCancel(t.Context())
	defer cancelOwner()
	sndr := &sender{ch: make(chan *proto.WorkerToHost, 4)}
	proxy := newStreamTasks(sndr, "owner")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ownerCtx.Done():
				return
			case request := <-sndr.ch:
				// Exercise both envelopes' protobuf serialization, not just direct methods.
				raw, err := wire.Marshal(request)
				if err != nil {
					t.Error(err)
					return
				}
				var incoming proto.WorkerToHost
				if err := wire.Unmarshal(raw, &incoming); err != nil {
					t.Error(err)
					return
				}
				response := handleTaskControl(ownerCtx, manager, "owner", incoming.GetTaskControlRequest())
				raw, err = wire.Marshal(&proto.HostToWorker{Msg: &proto.HostToWorker_TaskControlResponse{TaskControlResponse: response}})
				if err != nil {
					t.Error(err)
					return
				}
				var outgoing proto.HostToWorker
				if err := wire.Unmarshal(raw, &outgoing); err != nil {
					t.Error(err)
					return
				}
				proxy.deliver(outgoing.GetTaskControlResponse())
			}
		}
	}()
	defer func() { cancelOwner(); <-done }()
	ctx, cancel := context.WithTimeout(ownerCtx, 3*time.Second)
	defer cancel()
	if _, err := proxy.Launch(ctx, dispatch.Spec{ConversationID: "foreign", Task: "invalid"}); err == nil {
		t.Fatal("foreign launch accepted")
	}
	accepted, err := proxy.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "work", Tools: []string{"Read"}, SubConversationID: "untrusted-child"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case spec := <-started:
		if spec.ConversationID != "owner" || spec.SubConversationID != accepted.ChildID || spec.SubConversationID == "untrusted-child" || spec.Mode != dispatch.Agentic || spec.Emit != nil {
			t.Fatalf("bad host spec: %+v", spec)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	records, err := proxy.List(ctx, "owner", 20)
	if err != nil || len(records) != 1 {
		t.Fatalf("list %v %v", records, err)
	}
	if err := store.EnsureConversation(ctx, accepted.ChildID, "", "fixture"); err != nil { t.Fatal(err) }
	if err := store.(conversation.DispatchEventStore).AppendDispatchEvent(ctx, conversation.DispatchEvent{ConversationID: accepted.ChildID, Seq: 1, Kind: "tool_call", Timestamp: time.Now(), PayloadJSON: `{"tool_name":"Read","tool_use_id":"read-one"}`}); err != nil { t.Fatal(err) }
	inspected, err := proxy.Inspect(ctx, "owner", accepted.ID)
	if err != nil || inspected.Evidence == nil || len(inspected.Evidence.Recent) != 1 || inspected.Evidence.Recent[0].Tool != "Read" || inspected.Terminal() {
		t.Fatalf("worker lost running evidence: %+v %v", inspected, err)
	}
	response := handleTaskControl(ctx, manager, "foreign", &proto.TaskControlRequest{Operation: "inspect", TaskId: accepted.ID})
	if !response.NotFound || len(response.ResultJson) > 0 {
		t.Fatalf("foreign host request leaked: %+v", response)
	}
	if _, err := proxy.Stop(ctx, "other", accepted.ID); err == nil {
		t.Fatal("foreign stop accepted")
	}
	canceled, cancelWait := context.WithCancel(ctx)
	cancelWait()
	if _, err := proxy.Wait(canceled, "owner", accepted.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait cancellation: %v", err)
	}
	cancelOwner()
	<-done // Simulate loss of the initiating worker stream.
	record, err := manager.Inspect(context.Background(), "owner", accepted.ID)
	if err != nil || record.Terminal() {
		t.Fatalf("stream loss killed task: %+v %v", record, err)
	}
	close(release)
	waitCtx, cancelFinal := context.WithTimeout(context.Background(), time.Second)
	defer cancelFinal()
	record, err = manager.Wait(waitCtx, "owner", accepted.ID)
	if err != nil || record.Status != "completed" {
		t.Fatalf("final %+v %v", record, err)
	}
	inspected, err = manager.Inspect(context.Background(), "owner", accepted.ID)
	if err != nil || inspected.Evidence == nil || inspected.Status != record.Status || string(inspected.ResultJSON) != string(record.ResultJSON) || inspected.NotificationPending != record.NotificationPending {
		t.Fatalf("terminal inspection changed outcome: %+v %v", inspected, err)
	}
}

func TestWorkerTaskBridgeRejectsInvalidRequests(t *testing.T) {
	for _, req := range []*proto.TaskControlRequest{{Operation: "launch"}, {Operation: "list"}, {Operation: "inspect"}, {Operation: "wait"}, {Operation: "stop"}} {
		response := handleTaskControl(t.Context(), nil, "", req)
		if response.Error == "" || len(response.ResultJson) > 0 {
			t.Fatalf("unbound: %+v", response)
		}
		response = handleTaskControl(t.Context(), nil, "owner", req)
		if response.Error == "" {
			t.Fatal("missing task service accepted")
		}
	}
}
