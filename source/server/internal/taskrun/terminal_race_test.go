package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

func TestConcurrentStopAndCompletionRetainEvidenceAndReleaseExecution(t *testing.T) {
	release := make(chan struct{})
	manager, _ := newManager(t, func(ctx context.Context, _ dispatch.Spec) (dispatch.Result, error) {
		result := dispatch.Result{Text: "retained evidence", Model: "fixture"}
		select {
		case <-release:
			return result, nil
		case <-ctx.Done():
			return result, ctx.Err()
		}
	})
	ctx := deadline(t)
	var tasks []conversation.TaskRecord
	for i := 0; i < 32; i++ {
		task, err := manager.Launch(ctx, taskSpec())
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	var stops sync.WaitGroup
	for _, task := range tasks {
		stops.Add(1)
		go func(id string) {
			defer stops.Done()
			if _, err := manager.Stop(ctx, "parent", id); err != nil {
				t.Error(err)
			}
		}(task.ID)
	}
	close(release)
	stops.Wait()
	for _, task := range tasks {
		final, err := manager.Wait(ctx, "parent", task.ID)
		for errors.Is(err, conversation.ErrTaskWaitInterrupted) {
			final, err = manager.Wait(ctx, "parent", task.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		if final.Status != "completed" && final.Status != "stopped" {
			t.Fatalf("nonterminal outcome=%v", final)
		}
		var result dispatch.Result
		if err := json.Unmarshal(final.ResultJSON, &result); err != nil {
			t.Fatal(err)
		}
		// A task canceled before startup can fail StartTask without executing. Its
		// error must still be retained rather than masquerading as successful work.
		if result.Text == "" && final.Error == "" {
			t.Fatalf("missing output and error: %v", final)
		}
		if !final.NotificationPending || final.ChildID != task.ChildID {
			t.Fatalf("lost notification or identity: %v", final)
		}
	}
	manager.mu.Lock()
	active := len(manager.active)
	manager.mu.Unlock()
	if active != 0 {
		t.Fatalf("terminal execution handles retained: %d", active)
	}
}
