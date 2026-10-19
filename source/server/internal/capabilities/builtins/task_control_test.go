package builtins

import (
	"cercano/source/server/internal/capabilities"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/taskrun"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskControlsRequireTrustedScope(t *testing.T) {
	for _, op := range []string{"list", "inspect", "wait", "stop"} {
		t.Run(op, func(t *testing.T) {
			_, err := TaskControl(op).Execute(context.Background(), &capabilities.Call{Args: json.RawMessage(`{"conversation_id":"forged","task_id":"guessed"}`)})
			if err == nil || !strings.Contains(err.Error(), "trusted per-chat binding missing") {
				t.Fatalf("unbound request: %v", err)
			}
		})
	}
}
// A wait deadline is a normal bounded-wait outcome, not a task error: the
// capability must return the task's current status as a normal result so quiet
// UI does not leak a timeout error row, while owner cancellation stays an error.
func TestTaskWaitDeadlineReportsStatusNotError(t *testing.T) {
	ctx := context.Background()
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	release := make(chan struct{})
	m, err := taskrun.New(store.(conversation.TaskStore), func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		select {
		case <-release:
			return dispatch.Result{Text: "done"}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer close(release)
	defer func() {
		c, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := m.Close(c); err != nil {
			t.Error(err)
		}
	}()
	accepted, err := m.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"task_id": accepted.ID, "conversation_id": "owner", "timeout_seconds": 1})
	res, err := TaskControl("wait").Execute(ctx, &capabilities.Call{ConversationID: "owner", Args: args, Svc: capabilities.Services{Tasks: m}})
	if err != nil {
		t.Fatalf("wait deadline must not be an error: %v", err)
	}
	text := ""
	if res != nil {
		text = res.Text
	}
	if !strings.Contains(text, `"status":"running"`) && !strings.Contains(text, `"status":"accepted"`) {
		t.Fatalf("wait timeout should return current task status, got %q", text)
	}
	if !strings.Contains(text, "wait stopped") {
		t.Fatalf("wait timeout result should note the bounded wait ended, got %q", text)
	}
	if strings.Contains(text, `"error"`) {
		t.Fatalf("wait timeout must not fabricate a task error, got %q", text)
	}
}

func TestTaskControlsChatIsolationAndCancellation(t *testing.T) {
	ctx := context.Background()
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	release := make(chan struct{})
	m, err := taskrun.New(store.(conversation.TaskStore), func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		select {
		case <-release:
			return dispatch.Result{Text: "done"}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := m.Close(c); err != nil {
			t.Error(err)
		}
	}()
	accepted, err := m.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(ctx context.Context, op, parent string) (*capabilities.Result, error) {
		args, _ := json.Marshal(map[string]any{"task_id": accepted.ID, "conversation_id": "owner"})
		return TaskControl(op).Execute(ctx, &capabilities.Call{ConversationID: parent, Args: args, Svc: capabilities.Services{Tasks: m}})
	}
	checkForeign := func() {
		t.Helper()
		for _, op := range []string{"inspect", "wait", "stop"} {
			if _, err := invoke(ctx, op, "foreign"); !errors.Is(err, conversation.ErrTaskNotFound) {
				t.Fatalf("foreign %s: %v", op, err)
			}
		}
		rows, err := m.List(ctx, "foreign", 20)
		if err != nil || len(rows) != 0 {
			t.Fatalf("foreign list: %v %v", rows, err)
		}
	}
	checkForeign()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := invoke(canceled, "wait", "owner"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait: %v", err)
	}
	record, err := m.Inspect(ctx, "owner", accepted.ID)
	if err != nil || record.Terminal() {
		t.Fatalf("wait canceled execution: %+v %v", record, err)
	}
	close(release)
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := invoke(deadline, "wait", "owner"); err != nil {
		t.Fatal(err)
	}
	checkForeign()
	if _, err := invoke(ctx, "inspect", "owner"); err != nil {
		t.Fatal(err)
	}
}
