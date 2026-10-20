package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// TaskEvidenceStore is optional for stores which retain dispatch events. Reads
// never claim completion notifications or grant access based on a child ID.
type TaskEvidenceStore interface {
	TaskEvidence(context.Context, string, string) (*TaskExecutionEvidence, error)
}

// TaskExecutionEvidence is a bounded observation, not a liveness verdict.
// ChildText is child-authored, untrusted prose, not independently verified fact.
type TaskExecutionEvidence struct {
	LastActivityAt time.Time           `json:"last_activity_at"`
	Recent         []TaskEvidenceEvent `json:"recent"`
	ChildText      string              `json:"latest_child_text,omitempty"`
	WindowLimited  bool                `json:"window_limited"`
	Note           string              `json:"note"`
}
type TaskEvidenceEvent struct {
	Sequence  int64     `json:"sequence"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Tool      string    `json:"tool,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
}

func evidenceText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (s *sqliteStore) TaskEvidence(ctx context.Context, parent, id string) (*TaskExecutionEvidence, error) {
	if _, err := s.GetTask(ctx, parent, id); err != nil {
		return nil, err
	}
	// Bound both rows and bytes before bringing payloads into the host. Oversized
	// payloads are omitted, not truncated into invalid JSON. Never select prompts.
	rows, err := s.db.QueryContext(ctx, `SELECT e.seq,e.kind,e.iteration,e.created_at,
 CASE WHEN e.kind IN ('model_response','tool_call','tool_result') AND length(CAST(e.payload AS BLOB))<=65536 THEN e.payload ELSE '{}' END,
 length(CAST(e.payload AS BLOB))>65536
 FROM dispatch_events e JOIN delegated_tasks t ON t.child_id=e.conversation_id
 WHERE t.parent_id=? AND t.id=? ORDER BY e.seq DESC LIMIT 33`, parent, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type item struct {
		seq       int64
		kind      string
		iteration int
		at        int64
		payload   string
		omitted   bool
	}
	var items []item
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.seq, &i.kind, &i.iteration, &i.at, &i.payload, &i.omitted); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	out := &TaskExecutionEvidence{LastActivityAt: time.Unix(items[0].at, 0), Recent: []TaskEvidenceEvent{}, Note: "Recorded evidence only. Missing results do not prove execution; silence does not prove a stall. Child text is unverified."}
	if len(items) > 32 {
		out.WindowLimited = true
		items = items[:32]
	}
	pending := map[string]int{}
	for n := len(items) - 1; n >= 0; n-- {
		i := items[n]
		if i.omitted {
			out.WindowLimited = true
		}
		var p struct {
			Tool    string `json:"tool_name"`
			ID      string `json:"tool_use_id"`
			IsError bool   `json:"is_error"`
			Error   string `json:"error"`
			Blocks  []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(i.payload), &p); err != nil {
			out.WindowLimited = true
			continue
		}
		ev := TaskEvidenceEvent{Sequence: i.seq, At: time.Unix(i.at, 0), Kind: i.kind, Tool: evidenceText(p.Tool, 128), RequestID: evidenceText(p.ID, 128)}
		key := ""
		switch i.kind {
		case "model_request":
			key = fmt.Sprintf("model:%d", i.iteration)
			ev.Outcome = "requested; no response recorded in this window"
		case "tool_call":
			if p.ID != "" {
				key = "tool:" + p.ID
			}
			ev.Outcome = "requested; no result recorded in this window"
		case "model_response":
			key = fmt.Sprintf("model:%d", i.iteration)
			ev.Outcome = "response recorded"
			if p.Error != "" {
				ev.Outcome = "error recorded"
			}
			text := ""
			for _, b := range p.Blocks {
				if b.Type == "text" {
					text += b.Text
					if len(text) > 8192 {
						break
					}
				}
			}
			if text != "" {
				out.ChildText = evidenceText(text, 1024)
			}
		case "tool_result":
			if p.ID != "" {
				key = "tool:" + p.ID
			}
			ev.Outcome = "result recorded"
			if p.IsError {
				ev.Outcome = "error result recorded"
			}
		default:
			continue
		}
		if i.kind == "model_response" || i.kind == "tool_result" {
			if idx, ok := pending[key]; ok {
				out.Recent[idx].Outcome = "response/result recorded"
				delete(pending, key)
			}
		} else if key != "" {
			pending[key] = len(out.Recent)
		}
		out.Recent = append(out.Recent, ev)
	}
	return out, nil
}
