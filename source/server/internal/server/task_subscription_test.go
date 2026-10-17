package server

import (
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/taskrun"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/pkg/agentclient"
	"cercano/source/server/pkg/proto"
	"google.golang.org/grpc"
)

func taskObserverClient(t *testing.T, s *Server) *agentclient.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	proto.RegisterAgentServer(server, s)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	client, err := agentclient.DialExisting(t.Context(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}
func seedObservedTask(t *testing.T, s *Server, parent, id, child string) {
	t.Helper()
	ctx := t.Context()
	store := s.persistSvc.Store()
	tasks := store.(conversation.TaskStore)
	if err := tasks.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureConversation(ctx, parent, "/project", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSubagentConversation(ctx, child, parent, "/project", "fixture", nil); err != nil {
		t.Fatal(err)
	}
	if err := tasks.InsertTask(ctx, conversation.TaskRecord{ID: id, ParentID: parent, ChildID: child, Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.StartTask(ctx, parent, id); err != nil {
		t.Fatal(err)
	}
}
func TestTaskSubscriptionIndependentLiveUpdatesAndIsolation(t *testing.T) {
	s, _ := newServerWithStore(t)
	seedObservedTask(t, s, "owner", "task", "child")
	seedObservedTask(t, s, "foreign", "secret-task", "secret-child")
	store := s.persistSvc.Store().(conversation.DispatchEventStore)
	appendEvent := func(seq int64) {
		t.Helper()
		if err := store.AppendDispatchEvent(t.Context(), conversation.DispatchEvent{ConversationID: "child", Seq: seq, Kind: "note", PayloadJSON: `{"text":"progress"}`}); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent(1)
	client := taskObserverClient(t, s)
	mainCtx, _, release := s.beginTurn(t.Context(), "owner")
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	updates, err := client.SubscribeTasks(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	sawSnapshot, sawProgress := false, false
	for {
		select {
		case update, ok := <-updates:
			if !ok || update.Err != nil {
				t.Fatalf("stream ended: %v", update.Err)
			}
			if task := update.Event.GetTask(); task != nil {
				if task.TaskId != "task" || task.Status != "running" {
					t.Fatalf("unexpected task: %v", task)
				}
				sawSnapshot = true
			}
			if progress := update.Event.GetProgress(); progress != nil {
				if progress.TaskId != "task" || progress.Sequence != 1 {
					t.Fatalf("unexpected progress: %v", progress)
				}
				sawProgress = true
			}
			if update.Event.GetSnapshotComplete() {
				goto snapshotDone
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
snapshotDone:
	if !sawSnapshot || !sawProgress {
		t.Fatal("missing snapshot or durable replay")
	}
	// Superseding the parent does not terminate its task observation stream.
	_, _, releaseNext := s.beginTurn(t.Context(), "owner")
	defer releaseNext()
	if mainCtx.Err() == nil {
		t.Fatal("probe did not supersede parent")
	}
	appendEvent(2)
	for {
		select {
		case update := <-updates:
			if update.Err != nil {
				t.Fatal(update.Err)
			}
			if progress := update.Event.GetProgress(); progress != nil {
				if progress.Sequence != 2 {
					t.Fatalf("duplicate progress: %v", progress)
				}
				goto liveDone
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
liveDone:
	cancel()
	for range updates {
	}
	task, err := s.persistSvc.Store().(conversation.TaskStore).GetTask(t.Context(), "owner", "task")
	if err != nil || task.Status != "running" {
		t.Fatalf("observer cancellation changed execution: %v %v", task, err)
	}
}

// Direct streaming probe lets the initial snapshot cancel exactly at its marker.
type taskSnapshotProbe struct {
	grpc.ServerStream
	ctx  context.Context
	send func(*proto.TaskSubscriptionEvent) error
}

func (s *taskSnapshotProbe) Context() context.Context                  { return s.ctx }
func (s *taskSnapshotProbe) Send(e *proto.TaskSubscriptionEvent) error { return s.send(e) }
func TestTaskSubscriptionPaginationCursorAndDismissal(t *testing.T) {
	s, _ := newServerWithStore(t)
	for i := 0; i < conversation.TaskSubscriptionPageSize+2; i++ {
		seedObservedTask(t, s, "owner", fmt.Sprintf("task-%03d", i), fmt.Sprintf("child-%03d", i))
	}
	events := s.persistSvc.Store().(conversation.DispatchEventStore)
	for seq := int64(1); seq <= 3; seq++ {
		if err := events.AppendDispatchEvent(t.Context(), conversation.DispatchEvent{ConversationID: "child-000", Seq: seq, Kind: "note", PayloadJSON: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.persistSvc.Store().MarkSubagentDismissed(t.Context(), "child-001"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	snapshots, progress := 0, 0
	probe := &taskSnapshotProbe{ctx: ctx, send: func(event *proto.TaskSubscriptionEvent) error {
		if task := event.GetTask(); task != nil {
			snapshots++
			if task.ChildId == "child-001" {
				t.Fatal("dismissed child resurrected")
			}
		}
		if record := event.GetProgress(); record != nil {
			progress++
			if record.Sequence != 3 {
				t.Fatalf("cursor replayed old record %v", record)
			}
		}
		if event.GetSnapshotComplete() {
			cancel()
		}
		return nil
	}}
	_ = s.SubscribeTasks(&proto.SubscribeTasksRequest{ParentId: "owner", Cursors: []*proto.TaskProgressCursor{{TaskId: "task-000", Sequence: 2}, {TaskId: "foreign-task", Sequence: 999}}}, probe)
	if snapshots != conversation.TaskSubscriptionPageSize+1 || progress != 1 {
		t.Fatalf("snapshots=%d progress=%d", snapshots, progress)
	}
}

func TestTaskSubscriptionAfterStoreReopenReportsInterruptedWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	store, err := conversation.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newTestServer()
	s.persistSvc = &stubPersistSvc{store: store}
	seedObservedTask(t, s, "owner", "task", "child")
	if err := store.(conversation.DispatchEventStore).AppendDispatchEvent(t.Context(), conversation.DispatchEvent{ConversationID: "child", Seq: 1, Kind: "note", PayloadJSON: `{"text":"preserved work"}`}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := conversation.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var executions atomic.Int32
	manager, err := taskrun.New(reopened.(conversation.TaskStore), func(context.Context, dispatch.Spec) (dispatch.Result, error) {
		executions.Add(1)
		return dispatch.Result{}, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(t.Context())
	next, _ := newTestServer()
	next.persistSvc = &stubPersistSvc{store: reopened}
	client := taskObserverClient(t, next)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	updates, err := client.SubscribeTasks(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	interrupted, preserved := false, false
	for {
		select {
		case update, ok := <-updates:
			if !ok || update.Err != nil {
				t.Fatalf("subscription ended: %v", update.Err)
			}
			if task := update.Event.GetTask(); task != nil {
				interrupted = task.Status == "interrupted"
			}
			if progress := update.Event.GetProgress(); progress != nil {
				preserved = progress.PayloadJson == `{"text":"preserved work"}`
			}
			if update.Event.GetSnapshotComplete() {
				if !interrupted || !preserved || executions.Load() != 0 {
					t.Fatalf("interrupted=%v preserved=%v executions=%d", interrupted, preserved, executions.Load())
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
