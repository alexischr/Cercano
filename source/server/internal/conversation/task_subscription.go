package conversation

import (
	"context"
	"time"
)

// TaskSubscriptionStore provides bounded pages for reconnectable observation.
// Both queries require the parent; child IDs supplied by clients never authorize
// access to another conversation's dispatch evidence.
type TaskSubscriptionStore interface {
	TaskSnapshotPage(context.Context, string, string) ([]TaskRecord, error)
	TaskProgressPage(context.Context, string, string, int64) ([]DispatchEvent, error)
}

const TaskSubscriptionPageSize = 128

func (s *sqliteStore) TaskSnapshotPage(ctx context.Context, parent, after string) ([]TaskRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,parent_id,status,task,child_id,error FROM delegated_tasks t WHERE parent_id=? AND id>? AND NOT EXISTS(SELECT 1 FROM conversations c WHERE c.id=t.child_id AND c.dismissed=1) ORDER BY id LIMIT ?`, parent, after, TaskSubscriptionPageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TaskRecord, 0)
	for rows.Next() {
		var task TaskRecord
		err := rows.Scan(&task.ID, &task.ParentID, &task.Status, &task.Task, &task.ChildID, &task.Error)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}
func (s *sqliteStore) TaskProgressPage(ctx context.Context, parent, id string, after int64) ([]DispatchEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.conversation_id,e.seq,e.kind,e.iteration,e.created_at,e.payload FROM dispatch_events e JOIN delegated_tasks t ON t.child_id=e.conversation_id WHERE t.parent_id=? AND t.id=? AND e.seq>? AND e.kind IN ('dispatch_start','dispatch_done','note','model_response','tool_call','tool_result') ORDER BY e.seq LIMIT ?`, parent, id, after, TaskSubscriptionPageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DispatchEvent
	for rows.Next() {
		var ev DispatchEvent
		var created int64
		if err := rows.Scan(&ev.ConversationID, &ev.Seq, &ev.Kind, &ev.Iteration, &created, &ev.PayloadJSON); err != nil {
			return nil, err
		}
		ev.Timestamp = time.Unix(created, 0)
		out = append(out, ev)
	}
	return out, rows.Err()
}

var _ TaskSubscriptionStore = (*sqliteStore)(nil)
