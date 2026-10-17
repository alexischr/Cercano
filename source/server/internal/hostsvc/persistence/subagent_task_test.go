package persistence

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cercano/source/server/internal/conversation"
	"cercano/source/server/pkg/proto"
)

func TestListSubAgentsUsesTaskChildIdentity(t *testing.T) {
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	tasks := store.(conversation.TaskStore)
	if err := tasks.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureConversation(ctx, "owner", "/project", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSubagentConversation(ctx, "child", "owner", "/project", "fixture", []string{"Read"}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.InsertTask(ctx, conversation.TaskRecord{ID: "task", ParentID: "owner", ChildID: "child", Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.StartTask(ctx, "owner", "task"); err != nil {
		t.Fatal(err)
	}
	service := &svc{convAgent: &viewportResumeFakeAgent{store: store}}
	response, err := service.ListSubAgents(ctx, &proto.ListSubAgentsRequest{ParentId: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Subagents) != 1 {
		t.Fatalf("children=%v", response.Subagents)
	}
	child := response.Subagents[0]
	if child.TaskId != "task" || child.TaskStatus != "running" {
		t.Fatalf("child task metadata=%q/%q; child ID is not a task ID", child.TaskId, child.TaskStatus)
	}
}

func TestListSubAgentsNestedTaskMetadataAndForeignIsolation(t *testing.T) {
	store, err := conversation.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	tasks := store.(conversation.TaskStore)
	if err := tasks.InitTasks(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureConversation(ctx, "owner", "/project", "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, child := range []struct{ id, parent string }{{"child", "owner"}, {"nested", "child"}, {"legacy", "owner"}} {
		if err := store.EnsureSubagentConversation(ctx, child.id, child.parent, "/project", "fixture", nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []conversation.TaskRecord{
		{ID: "nested-task", ParentID: "child", ChildID: "nested", Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "foreign-task", ParentID: "other", ChildID: "legacy", Status: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := tasks.InsertTask(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tasks.GetTaskByChild(ctx, "owner", "nested"); !errors.Is(err, conversation.ErrTaskNotFound) {
		t.Fatalf("foreign lookup=%v", err)
	}
	service := &svc{convAgent: &viewportResumeFakeAgent{store: store}}
	response, err := service.ListSubAgents(ctx, &proto.ListSubAgentsRequest{ParentId: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range response.Subagents {
		if child.Id == "legacy" && child.TaskId != "" {
			t.Fatal("foreign task metadata leaked")
		}
	}
	response, err = service.ListSubAgents(ctx, &proto.ListSubAgentsRequest{ParentId: "child"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Subagents) != 1 || response.Subagents[0].TaskId != "nested-task" || response.Subagents[0].TaskStatus != "accepted" {
		t.Fatalf("nested metadata=%v", response.Subagents)
	}
}
