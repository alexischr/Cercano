package server

import (
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/taskrun"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"cercano/source/server/internal/agent"
	"cercano/source/server/internal/conversation"
	"cercano/source/server/internal/llm"
	"cercano/source/server/internal/runner"
	"cercano/source/server/pkg/proto"
)

type completionProbeRunner struct {
	run func(context.Context, runner.Request, runner.PermissionRequester) (runner.Result, error)
}

func (p completionProbeRunner) RunTurn(ctx context.Context, req runner.Request, _ runner.EventSink, permission runner.PermissionRequester, _ runner.PersistFunc) (runner.Result, error) {
	return p.run(ctx, req, permission)
}
func seedCompletion(t *testing.T, s *Server, id string) conversation.TaskStore {
	t.Helper()
	store := s.persistSvc.Store()
	ts := store.(conversation.TaskStore)
	if err := store.EnsureConversation(t.Context(), "owner", t.TempDir(), "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := ts.InitTasks(t.Context()); err != nil {
		t.Fatal(err)
	}
	task := conversation.TaskRecord{ID: id, ChildID: "child-" + id, ParentID: "owner", Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := ts.InsertTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	task.Status = "completed"
	if _, err := ts.FinishTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	return ts
}
func TestCompletionWakeCoalescesAndDoesNotReplaceBusyTurn(t *testing.T) {
	s, _ := newServerWithStore(t)
	ts := seedCompletion(t, s, "one")
	seedCompletion(t, s, "two")
	calls := 0
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, req runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		calls++
		if req.InputRole != string(llm.RoleSystem) || !strings.Contains(req.Input, "child-one") || !strings.Contains(req.Input, "child-two") {
			t.Errorf("bad completion request: %+v", req)
		}
		return runner.Result{FinalText: "handled", Model: "fixture"}, nil
	}}
	active, gen, release := s.beginTurn(t.Context(), "owner")
	s.tryTaskCompletionWake(t.Context(), "owner")
	if calls != 0 || active.Err() != nil || !s.turnIsCurrent("owner", gen) {
		t.Fatal("busy turn interrupted")
	}
	pending, err := ts.PendingTasks(t.Context(), "owner")
	if err != nil || len(pending) != 2 || pending[0].WakeClaimed {
		t.Fatalf("busy notification state: %v %v", pending, err)
	}
	release()
	s.tryTaskCompletionWake(t.Context(), "owner")
	s.tryTaskCompletionWake(t.Context(), "owner")
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	pending, err = ts.PendingTasks(t.Context(), "owner")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after success: %v %v", pending, err)
	}
	if s.hasActiveTurn("owner") {
		t.Fatal("completion lease leaked")
	}
}
func TestSupersededCompletionRetainsEvidenceWithoutAutomaticLoop(t *testing.T) {
	s, _ := newServerWithStore(t)
	ts := seedCompletion(t, s, "one")
	calls := 0
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, _ runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		calls++
		_, _, release := s.beginTurn(t.Context(), "owner")
		defer release()
		return runner.Result{}, ctx.Err()
	}}
	s.tryTaskCompletionWake(t.Context(), "owner")
	s.tryTaskCompletionWake(t.Context(), "owner")
	pending, err := ts.PendingTasks(t.Context(), "owner")
	if err != nil || len(pending) != 1 || !pending[0].WakeClaimed || calls != 1 {
		t.Fatalf("calls=%d pending=%v err=%v", calls, pending, err)
	}
}
func TestClaimedCompletionIsRecoveredAtHumanTurnBoundary(t *testing.T) {
	s, _ := newServerWithStore(t)
	ts := seedCompletion(t, s, "one")
	if _, err := ts.ClaimTaskWake(t.Context(), "owner", "one"); err != nil {
		t.Fatal(err)
	}
	var roles []string
	s.inProcessRunner = completionProbeRunner{run: func(_ context.Context, req runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		roles = append(roles, req.InputRole)
		if len(roles) == 2 && !strings.Contains(req.Input, "child-one") {
			t.Error("retained outcome omitted")
		}
		return runner.Result{Model: "fixture"}, nil
	}}
	if err := s.streamProcessRequestWithToolLoop(&proto.ProcessRequestRequest{ConversationId: "owner", Input: "resume"}, &fakeStream{ctx: t.Context()}); err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 || roles[0] != string(llm.RoleUser) || roles[1] != string(llm.RoleSystem) {
		t.Fatalf("roles=%v", roles)
	}
	pending, err := ts.PendingTasks(t.Context(), "owner")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
}

func TestDisconnectedCompletionWaitsForReconnectedApproval(t *testing.T) {
	s, _ := newServerWithStore(t)
	ts := seedCompletion(t, s, "one")
	s.SetPermissions(agent.NewStaticPermissionStore(agent.ModePermissive), agent.NewPendingDecisions())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	approved := make(chan bool, 1)
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, _ runner.Request, permission runner.PermissionRequester) (runner.Result, error) {
		allow, err := permission(ctx, "background-approval", "Edit", json.RawMessage(`{}`), llm.PermW, false)
		approved <- allow
		return runner.Result{FinalText: "handled", Model: "fixture"}, err
	}}
	changes, detach := s.pendingPrompts.subscribe("owner")
	defer detach()
	<-changes
	done := make(chan struct{})
	go func() { defer close(done); s.tryTaskCompletionWake(ctx, "owner") }()
	select {
	case <-changes:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-approved:
		t.Fatal("action resolved without a client approval")
	default:
	}
	attachCtx, disconnect := context.WithCancel(ctx)
	defer disconnect()
	observer := &immediatePermissionStream{ctx: attachCtx, reply: func(prompt *proto.PermissionRequired) {
		if !s.permBroker.Resolve("owner", prompt.ToolUseId, agent.Decision{Allow: true}) {
			t.Error("approval not registered")
		}
		disconnect()
	}}
	_ = s.AttachConversation(&proto.AttachConversationRequest{ConversationId: "owner"}, observer)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !<-approved {
		t.Fatal("explicit approval lost")
	}
	pending, err := ts.PendingTasks(ctx, "owner")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
}

func TestCompletionAdmissionDenialDoesNotConsumeNotice(t *testing.T) {
	s, _ := newServerWithStore(t)
	ts := seedCompletion(t, s, "one")
	s.SetBackgroundWorkAdmission(func(ctx context.Context) (context.Context, func(), error) {
		return ctx, nil, errors.New("policy unavailable")
	})
	s.inProcessRunner = completionProbeRunner{run: func(context.Context, runner.Request, runner.PermissionRequester) (runner.Result, error) {
		t.Error("inference bypassed admission")
		return runner.Result{}, nil
	}}
	s.tryTaskCompletionWake(t.Context(), "owner")
	pending, err := ts.PendingTasks(t.Context(), "owner")
	if err != nil || len(pending) != 1 || pending[0].WakeClaimed || s.hasActiveTurn("owner") {
		t.Fatalf("denied wake consumed state: %v %v", pending, err)
	}
}
func TestCompletionAdmissionContextAndFinish(t *testing.T) {
	s, _ := newServerWithStore(t)
	seedCompletion(t, s, "one")
	type policyKey struct{}
	finished := false
	s.SetBackgroundWorkAdmission(func(ctx context.Context) (context.Context, func(), error) {
		return context.WithValue(ctx, policyKey{}, "admitted"), func() { finished = true }, nil
	})
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, _ runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		if ctx.Value(policyKey{}) != "admitted" {
			t.Error("policy context lost")
		}
		if finished {
			t.Error("policy admission released before inference")
		}
		return runner.Result{Model: "fixture"}, nil
	}}
	s.tryTaskCompletionWake(t.Context(), "owner")
	if !finished {
		t.Fatal("admission leaked")
	}
}
func TestTaskCompletionCallbackWakesAfterHumanTurnRelease(t *testing.T) {
	s, _ := newServerWithStore(t)
	store := s.persistSvc.Store()
	ts := store.(conversation.TaskStore)
	manager, err := taskrun.New(ts, func(context.Context, dispatch.Spec) (dispatch.Result, error) {
		return dispatch.Result{Text: "finished"}, nil
	}, s.notifyTaskChange)
	if err != nil {
		t.Fatal(err)
	}
	s.taskManager = manager
	s.taskWakeCtx, s.taskWakeCancel = context.WithCancel(t.Context())
	defer s.closeTaskManager()
	if err := store.EnsureConversation(t.Context(), "owner", t.TempDir(), "fixture"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	humanStarted := make(chan struct{})
	releaseHuman := make(chan struct{})
	woken := make(chan runner.Request, 1)
	s.inProcessRunner = completionProbeRunner{run: func(ctx context.Context, req runner.Request, _ runner.PermissionRequester) (runner.Result, error) {
		if req.Input == "human" {
			close(humanStarted)
			select {
			case <-releaseHuman:
			case <-ctx.Done():
				return runner.Result{}, ctx.Err()
			}
		} else {
			woken <- req
		}
		return runner.Result{Model: "fixture"}, nil
	}}
	humanDone := make(chan error, 1)
	go func() {
		humanDone <- s.streamProcessRequestWithToolLoop(&proto.ProcessRequestRequest{ConversationId: "owner", Input: "human"}, &fakeStream{ctx: ctx})
	}()
	select {
	case <-humanStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	task, err := manager.Launch(ctx, dispatch.Spec{ConversationID: "owner", Task: "finish"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Wait(ctx, "owner", task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-woken:
		t.Fatal("woke while human turn active")
	default:
	}
	close(releaseHuman)
	select {
	case err := <-humanDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case req := <-woken:
		if req.InputRole != string(llm.RoleSystem) || !strings.Contains(req.Input, task.ID) {
			t.Fatalf("bad wake: %+v", req)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		pending, err := ts.PendingTasks(ctx, "owner")
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
