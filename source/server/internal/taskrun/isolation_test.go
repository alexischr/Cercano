package taskrun

import (
	"context"
	"errors"
	"testing"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

// A guessed task handle must confer no authority, whether the task is still
// executing or its result has already been persisted.
func TestTaskAccessIsIsolatedByChat(t *testing.T) {
	release := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, spec dispatch.Spec) (dispatch.Result, error) {
		select {
		case <-release:
			return dispatch.Result{Text: "private result for " + spec.ConversationID}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	ctx := deadline(t)
	a, err := m.Launch(ctx, taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	otherSpec := taskSpec()
	otherSpec.ConversationID = "other"
	b, err := m.Launch(ctx, otherSpec)
	if err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T) {
		t.Helper()
		for _, owner := range []struct{ parent, own, foreign string }{
			{"parent", a.ID, b.ID}, {"other", b.ID, a.ID},
		} {
			tasks, err := m.List(ctx, owner.parent, 100)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 || tasks[0].ID != owner.own {
				t.Fatalf("%s list leaked or lost tasks: %+v", owner.parent, tasks)
			}
			for name, access := range map[string]func(context.Context, string, string) (conversation.TaskRecord, error){
				"inspect": m.Inspect, "wait": m.Wait, "stop": m.Stop,
			} {
				record, err := access(ctx, owner.parent, owner.foreign)
				if !errors.Is(err, conversation.ErrTaskNotFound) {
					t.Fatalf("%s foreign %s: %v", owner.parent, name, err)
				}
				if record.ID != "" || len(record.ResultJSON) != 0 {
					t.Fatalf("%s leaked a foreign record: %+v", name, record)
				}
			}
		}
		tasks, err := m.List(ctx, "unrelated-chat", 100)
		if err != nil || len(tasks) != 0 {
			t.Fatalf("unrelated list: %+v, %v", tasks, err)
		}
	}
	t.Run("executing", check)
	close(release)
	for _, owner := range []struct{ parent, id string }{{"parent", a.ID}, {"other", b.ID}} {
		final, err := m.Wait(ctx, owner.parent, owner.id)
		if err != nil {
			t.Fatal(err)
		}
		if final.Status != "completed" {
			t.Fatalf("foreign stop affected owner task: %+v", final)
		}
	}
	t.Run("persisted", check)
}
