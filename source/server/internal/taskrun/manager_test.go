package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/dispatch"
)

func newManager(t *testing.T, run Executor) (*Manager, conversation.Store) {
	t.Helper()
	s, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(s.(conversation.TaskStore), run, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
		s.Close()
	})
	return m, s
}
func taskSpec() dispatch.Spec {
	return dispatch.Spec{Mode: dispatch.Agentic, Task: "controlled work", ConversationID: "parent"}
}
func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func TestLaunchAndWaitCancellationDoNotCancelChild(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(canceled)
			return dispatch.Result{}, ctx.Err()
		case <-release:
			return dispatch.Result{Text: "complete", SubConversationID: s.SubConversationID, GrantedTools: []string{"Read"}}, nil
		}
	})
	parent, cancel := context.WithCancel(context.Background())
	task, err := m.Launch(parent, taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	waiting, stopWait := context.WithCancel(context.Background())
	stopWait()
	if _, err := m.Wait(waiting, "parent", task.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error: %v", err)
	}
	select {
	case <-canceled:
		t.Fatal("main cancellation killed child")
	default:
	}
	if _, err := m.Inspect(context.Background(), "other", task.ID); !errors.Is(err, conversation.ErrTaskNotFound) {
		t.Fatalf("cross-parent inspect: %v", err)
	}
	if _, err := m.Stop(context.Background(), "other", task.ID); !errors.Is(err, conversation.ErrTaskNotFound) {
		t.Fatalf("cross-parent stop: %v", err)
	}
	close(release)
	final, err := m.Wait(deadline(t), "parent", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.ChildID == "" || final.ChildID != task.ChildID || final.Status != "completed" || !final.NotificationPending {
		t.Fatalf("final: %+v", final)
	}
	var result dispatch.Result
	if err := json.Unmarshal(final.ResultJSON, &result); err != nil {
		t.Fatal(err)
	}
	if result.Text != "complete" || len(result.GrantedTools) != 1 {
		t.Fatalf("lost metadata: %+v", result)
	}
}
func TestStopPreservesPartialFailureAndDoesNotStopSibling(t *testing.T) {
	started := make(chan struct{}, 2)
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		started <- struct{}{}
		<-ctx.Done()
		return dispatch.Result{Text: "partial evidence", SubConversationID: "child", Suspicious: true, SuspicionReason: "test"}, ctx.Err()
	})
	a, err := m.Launch(context.Background(), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Launch(context.Background(), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	<-started
	if _, err := m.Stop(context.Background(), "parent", a.ID); err != nil {
		t.Fatal(err)
	}
	final, err := m.Wait(deadline(t), "parent", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "stopped" || final.ChildID != "child" || final.Error == "" {
		t.Fatalf("final: %+v", final)
	}
	other, err := m.Inspect(context.Background(), "parent", b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other.Status != "running" {
		t.Fatalf("sibling status %s", other.Status)
	}
}
func TestPanicProducesInspectableFailure(t *testing.T) {
	m, _ := newManager(t, func(context.Context, dispatch.Spec) (dispatch.Result, error) { panic("test panic") })
	task, err := m.Launch(context.Background(), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	final, err := m.Wait(deadline(t), "parent", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "failed" || final.Error == "" {
		t.Fatalf("final: %+v", final)
	}
}
func TestLaunchRequiresDurableAcceptance(t *testing.T) {
	m, s := newManager(t, func(context.Context, dispatch.Spec) (dispatch.Result, error) {
		t.Error("must not run")
		return dispatch.Result{}, nil
	})
	s.Close()
	if task, err := m.Launch(context.Background(), taskSpec()); err == nil || task.ID != "" {
		t.Fatalf("false acceptance: %+v %v", task, err)
	}
}
func TestShutdownInterruptsAndRejectsNewWork(t *testing.T) {
	started := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, _ dispatch.Spec) (dispatch.Result, error) {
		close(started)
		<-ctx.Done()
		return dispatch.Result{Text: "partial"}, ctx.Err()
	})
	task, err := m.Launch(context.Background(), taskSpec())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.Close(deadline(t)); err != nil {
		t.Fatal(err)
	}
	final, err := m.Inspect(context.Background(), "parent", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "interrupted" {
		t.Fatalf("status %s", final.Status)
	}
	if _, err := m.Launch(context.Background(), taskSpec()); err == nil {
		t.Fatal("launch after shutdown")
	}
}

// Acceptance must not collapse the existing dispatch result into a success
// string: failed and suspicious runs still carry useful evidence and routing.
func TestTerminalResultRetainsDispatchEvidence(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "completed"
		if failed {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			expected := dispatch.Result{Text: "partial transcript and final summary", Model: "model", Provider: "provider", Tier: "standard", IsCloud: true, Notice: "route notice", InputTokens: 123, OutputTokens: 45, GrantedTools: []string{"Read"}, IgnoredTools: []string{"unknown"}, Suspicious: failed, SuspicionReason: "inspect actual tool evidence", Profile: "profile", Destination: "secondary", ContextWindow: 8192, ContextWindowKnown: true}
			m, _ := newManager(t, func(_ context.Context, s dispatch.Spec) (dispatch.Result, error) {
				result := expected
				result.SubConversationID = s.SubConversationID
				if failed {
					return result, errors.New("execution budget exhausted")
				}
				return result, nil
			})
			accepted, err := m.Launch(context.Background(), taskSpec())
			if err != nil {
				t.Fatal(err)
			}
			final, err := m.Wait(deadline(t), "parent", accepted.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected.SubConversationID = accepted.ChildID
			var got dispatch.Result
			if err := json.Unmarshal(final.ResultJSON, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("lost dispatch evidence: got %+v want %+v", got, expected)
			}
			if final.Status != name || !final.NotificationPending {
				t.Fatalf("outcome %+v", final)
			}
			if failed && final.Error != "execution budget exhausted" {
				t.Fatalf("lost failure: %q", final.Error)
			}
		})
	}
}

func TestLaunchDoesNotRetainMainTurnProgressCallback(t *testing.T) {
	observed := make(chan dispatch.Spec, 1)
	release := make(chan struct{})
	m, _ := newManager(t, func(ctx context.Context, s dispatch.Spec) (dispatch.Result, error) {
		observed <- s
		select {
		case <-release:
			return dispatch.Result{}, nil
		case <-ctx.Done():
			return dispatch.Result{}, ctx.Err()
		}
	})
	spec := taskSpec()
	spec.Tools = []string{"Read"}
	spec.Emit = func(agenttools.ProgressEvent) { t.Error("main-turn callback invoked") }
	accepted, err := m.Launch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	var child dispatch.Spec
	select {
	case child = <-observed:
	case <-deadline(t).Done():
		t.Fatal("executor not started")
	}
	spec.Tools[0] = "RunCommand"
	if child.Tools[0] != "Read" {
		t.Fatal("caller mutation changed accepted tool grants")
	}
	if child.Emit != nil {
		t.Fatal("main-turn callback escaped into independent work")
	}
	if child.SubConversationID != accepted.ChildID {
		t.Fatal("reserved child identity lost")
	}
	close(release)
	if _, err := m.Wait(deadline(t), "parent", accepted.ID); err != nil {
		t.Fatal(err)
	}
}
