package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskRestartAndNotificationState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	ctx := context.Background()
	s, err := openTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task := TaskRecord{ID: "id", ParentID: "parent", Status: "accepted", Task: "work", CreatedAt: now, UpdatedAt: now}
	if err := s.InsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := s.StartTask(ctx, "parent", "id"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = openTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(ctx, "parent", "id")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" || !got.NotificationPending || got.WakeClaimed {
		t.Fatalf("restart: %+v", got)
	}
	claimed, err := s.ClaimTaskWake(ctx, "parent", "id")
	if err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	claimed, err = s.ClaimTaskWake(ctx, "parent", "id")
	if err != nil || claimed {
		t.Fatalf("duplicate claim %v %v", claimed, err)
	}
	pending, err := s.PendingTasks(ctx, "parent")
	if err != nil || len(pending) != 1 {
		t.Fatalf("claim lost pending evidence: %v %v", pending, err)
	}
	if err := s.HandleTaskNotification(ctx, "parent", "id"); err != nil {
		t.Fatal(err)
	}
	// Reconciliation and duplicate completion cannot overwrite terminal evidence
	// or rearm an already-handled notification.
	if err := s.ReconcileTasks(ctx); err != nil {
		t.Fatal(err)
	}
	got.Status = "completed"
	got.ResultJSON = json.RawMessage(`{"Text":"late"}`)
	changed, err := s.FinishTask(ctx, got)
	if err != nil || changed {
		t.Fatalf("terminal overwrite %v %v", changed, err)
	}
	got, err = s.GetTask(ctx, "parent", "id")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" || got.NotificationPending || !got.WakeClaimed {
		t.Fatalf("terminal state changed: %+v", got)
	}
	if _, err := s.GetTask(ctx, "other", "id"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("ownership: %v", err)
	}
}
func TestTaskOutcomeAndNotificationAreAtomic(t *testing.T) {
	s, err := openTaskStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	task := TaskRecord{ID: "id", ParentID: "parent", Status: "accepted", Task: "work", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.InsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task.Status = "failed"
	task.ResultJSON = json.RawMessage(`{"Text":"partial","GrantedTools":["Read"]}`)
	task.Error = "budget exhausted"
	changed, err := s.FinishTask(ctx, task)
	if err != nil || !changed {
		t.Fatalf("finish %v %v", changed, err)
	}
	got, err := s.GetTask(ctx, "parent", "id")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" || !got.NotificationPending || string(got.ResultJSON) != string(task.ResultJSON) || got.Error != task.Error {
		t.Fatalf("lost outcome: %+v", got)
	}
}

func openTaskStore(path string) (interface {
	Store
	TaskStore
}, error) {
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	return s.(interface {
		Store
		TaskStore
	}), nil
}
