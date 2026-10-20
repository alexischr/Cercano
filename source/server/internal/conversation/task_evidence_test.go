package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTaskInspectionEvidenceContract(t *testing.T) {
	if _, ok := reflect.TypeOf(TaskRecord{}).FieldByName("Evidence"); !ok {
		t.Fatal("inspection record cannot carry execution evidence through worker JSON bridge")
	}
}

func TestTaskEvidenceOwnershipBoundsAndOutcomes(t *testing.T) {
	s, err := openTaskStore(t.TempDir() + "/tasks.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	db := s.(*sqliteStore)
	if err := s.EnsureConversation(ctx, "child", "", "fixture"); err != nil {
		t.Fatal(err)
	}
	task := TaskRecord{ID: "task", ParentID: "owner", ChildID: "child", Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.InsertTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	evidence := s.(TaskEvidenceStore)
	if got, err := evidence.TaskEvidence(ctx, "owner", "task"); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	events := []struct{ kind, payload string }{
		{"model_request", `{"messages":[{"content":"SECRET PROMPT"}]}`},
		{"model_response", `{"blocks":[{"type":"thinking","text":"SECRET REASONING"},{"type":"text","text":"Checking tests"}]}`},
		{"tool_call", `{"tool_use_id":"one","tool_name":"RunCommand","args":"SECRET ARGS"}`},
		{"tool_result", `{"tool_use_id":"one","tool_name":"RunCommand","is_error":true,"content":"SECRET BODY"}`},
		{"tool_call", `{"tool_use_id":"two","tool_name":"Read"}`},
	}
	for n, e := range events {
		if _, err := db.db.ExecContext(ctx, `INSERT INTO dispatch_events(conversation_id,seq,kind,iteration,created_at,payload) VALUES('child',?,?,1,?,?)`, n+1, e.kind, 100+n, e.payload); err != nil {
			t.Fatal(err)
		}
	}
	got, err := evidence.TaskEvidence(ctx, "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	if got.ChildText != "Checking tests" || got.LastActivityAt.Unix() != 104 || len(got.Recent) != 5 {
		t.Fatalf("evidence: %+v", got)
	}
	if !strings.Contains(got.Recent[2].Outcome, "recorded") || got.Recent[3].Outcome != "error result recorded" || !strings.Contains(got.Recent[4].Outcome, "no result") {
		t.Fatalf("outcomes: %+v", got.Recent)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "SECRET") {
		t.Fatalf("private material leaked: %s", encoded)
	}
	if _, err := evidence.TaskEvidence(ctx, "foreign", "task"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("foreign: %v", err)
	}
	for n := 6; n <= 45; n++ {
		if _, err := db.db.ExecContext(ctx, `INSERT INTO dispatch_events(conversation_id,seq,kind,iteration,created_at,payload) VALUES('child',?,'model_request',?,100,'{}')`, n, n); err != nil {
			t.Fatal(err)
		}
	}
	got, err = evidence.TaskEvidence(ctx, "owner", "task")
	if err != nil {
		t.Fatal(err)
	}
	if !got.WindowLimited || len(got.Recent) != 32 {
		t.Fatalf("unbounded: %+v", got)
	}
	for n, payload := range []string{`{"tool_name":"` + strings.Repeat("x", 65536) + `"}`, `{broken`} {
		if _, err := db.db.ExecContext(ctx, `INSERT INTO dispatch_events(conversation_id,seq,kind,iteration,created_at,payload) VALUES('child',?,'tool_call',1,200,?)`, 46+n, payload); err != nil {
			t.Fatal(err)
		}
	}
	got, err = evidence.TaskEvidence(ctx, "owner", "task")
	if err != nil || got == nil || !got.WindowLimited || got.LastActivityAt.Unix() != 200 {
		t.Fatalf("omitted payload evidence: %+v %v", got, err)
	}
	encoded, err = json.Marshal(got)
	if err != nil || len(encoded) > 20000 {
		t.Fatalf("oversized evidence: bytes=%d err=%v", len(encoded), err)
	}
	record, err := s.GetTask(ctx, "owner", "task")
	if err != nil || record.Status != "accepted" || record.WakeClaimed || record.NotificationPending || record.Evidence != nil {
		t.Fatalf("inspection mutated task: %+v %v", record, err)
	}
}
