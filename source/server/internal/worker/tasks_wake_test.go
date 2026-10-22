package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/capabilities"
	"cercano/source/server/internal/capabilities/builtins"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/taskrun"
	"cercano/source/server/pkg/proto"
	wire "google.golang.org/protobuf/proto"
)

type observedWaitStore struct {
	conversation.TaskStore
	reads chan struct{}
}

func (s *observedWaitStore) GetTask(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	rec, err := s.TaskStore.GetTask(ctx, parent, id)
	select {
	case s.reads <- struct{}{}:
	default:
	}
	return rec, err
}

type wrappedWaitTasks struct{ capabilities.Tasks }

func (s wrappedWaitTasks) Wait(ctx context.Context, parent, id string) (conversation.TaskRecord, error) {
	rec, err := s.Tasks.Wait(ctx, parent, id)
	if err != nil {
		err = fmt.Errorf("host context: %w", err)
	}
	return rec, err
}

// Exercise the real manager, both serialized wire envelopes, restored sentinel
// identity, and the foreground capability's follow-up Inspect/yield contract.
func TestSiblingWakeThroughWorkerAndForeground(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := &observedWaitStore{TaskStore: store.(conversation.TaskStore), reads: make(chan struct{}, 1)}
	release := make(chan struct{})
	manager, err := taskrun.New(observed, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		if s.Task == "sibling" {
			select {
			case <-release:
				return dispatch.Result{Text: "sibling done"}, nil
			case <-ctx.Done():
			}
		}
		<-ctx.Done()
		return dispatch.Result{}, ctx.Err()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := manager.Close(c); err != nil {
			t.Error(err)
		}
	}()
	a, err := manager.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "target"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := manager.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "sibling"})
	if err != nil {
		t.Fatal(err)
	}
	sndr := &sender{ch: make(chan *proto.WorkerToHost, 4)}
	proxy := newStreamTasks(sndr, "owner")
	bridgeCtx, stopBridge := context.WithCancel(ctx)
	bridgeDone := make(chan struct{})
	go func() {
		defer close(bridgeDone)
		for {
			select {
			case <-bridgeCtx.Done():
				return
			case req := <-sndr.ch:
				raw, err := wire.Marshal(req)
				if err != nil {
					t.Error(err)
					return
				}
				var incoming proto.WorkerToHost
				if err := wire.Unmarshal(raw, &incoming); err != nil {
					t.Error(err)
					return
				}
				response := handleTaskControl(bridgeCtx, wrappedWaitTasks{manager}, "owner", incoming.GetTaskControlRequest())
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
	defer func() { stopBridge(); <-bridgeDone }()
	args, _ := json.Marshal(map[string]any{"task_id": a.ID, "timeout_seconds": 3})
	type result struct {
		res *capabilities.Result
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := builtins.TaskControl("wait").Execute(ctx, &capabilities.Call{ConversationID: "owner", Args: args, Svc: capabilities.Services{Tasks: proxy}})
		done <- result{res, err}
	}()
	select {
	case <-observed.reads:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.res == nil || !strings.Contains(got.res.Text, a.ID) || !strings.Contains(got.res.Text, "end this reply so the finished task's outcome can be delivered") {
			t.Fatalf("missing target status/yield guidance: %+v", got.res)
		}
	case <-ctx.Done():
		t.Fatal("foreground wait did not yield")
	}
	rec, err := manager.Inspect(ctx, "owner", a.ID)
	if err != nil || rec.Terminal() {
		t.Fatalf("target stopped: %+v %v", rec, err)
	}
	sibling, err := manager.Inspect(ctx, "owner", b.ID)
	if err != nil || !sibling.Terminal() || !sibling.NotificationPending {
		t.Fatalf("completion consumed or lost: %+v %v", sibling, err)
	}
	// The completed sibling is not replayed into the next registered wait.
	again, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	if _, err := manager.Wait(again, "owner", a.ID); err != context.DeadlineExceeded {
		t.Fatalf("stale completion replay: %v", err)
	}
}
