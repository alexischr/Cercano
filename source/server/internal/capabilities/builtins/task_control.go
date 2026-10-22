package builtins

import (
	"cercano/source/server/internal/capabilities"
	"cercano/source/server/internal/conversation"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type taskControl struct{ op string }

func TaskControl(op string) capabilities.Capability { return taskControl{op} }
func (c taskControl) Name() string                  { return "task_" + c.op }
func (c taskControl) Tier() capabilities.Tier {
	if c.op == "stop" {
		return capabilities.TierW
	}
	return capabilities.TierR
}
func (c taskControl) Surfaces() capabilities.Surface {
	return capabilities.SurfaceAgent | capabilities.SurfaceMCP
}
func (c taskControl) Description() string {
	switch c.op {
	case "list":
		return "List this chat's independent tasks, newest first. Does not list tasks owned by other chats."
	case "inspect":
		return "Inspect this chat's task status, saved result, and bounded recent execution evidence. Inspect before reporting progress; distinguish child-authored claims from recorded tool outcomes. Missing results do not prove execution, and silence does not prove a stall."
	case "wait":
		return "Wait up to timeout_seconds (default 30, maximum 300) for this chat's task. Timeout or cancellation stops waiting, not execution. If another task in this chat finishes first, the wait returns early with this task's current status so that outcome can be delivered."
	default:
		return "Request cancellation of this chat's task. This does not dismiss its tab; inspect or wait for the terminal outcome."
	}
}
func (c taskControl) Schema() capabilities.Schema {
	if c.op == "list" {
		return capabilities.Schema(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":100,"description":"Maximum records; defaults to 20."}},"additionalProperties":false}`)
	}
	return capabilities.Schema(`{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"description":"Wait timeout only; defaults to 30 seconds."}},"additionalProperties":false}`)
}
func (c taskControl) Execute(ctx context.Context, call *capabilities.Call) (*capabilities.Result, error) {
	// A model-provided conversation_id is never an authorization credential.
	if strings.TrimSpace(call.ConversationID) == "" {
		return nil, fmt.Errorf("%s: trusted per-chat binding missing; host integration must supply chat identity", c.Name())
	}
	if call.Svc.Tasks == nil {
		return nil, fmt.Errorf("%s: independent task service unavailable", c.Name())
	}
	var args struct {
		ID      string `json:"task_id"`
		Limit   int    `json:"limit"`
		Timeout int    `json:"timeout_seconds"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name(), err)
		}
	}
	var value any
	var err error
	if c.op == "list" {
		if args.Limit == 0 {
			args.Limit = 20
		}
		if args.Limit < 1 || args.Limit > 100 {
			return nil, fmt.Errorf("task_list: limit must be 1–100")
		}
		value, err = call.Svc.Tasks.List(ctx, call.ConversationID, args.Limit)
	} else {
		if strings.TrimSpace(args.ID) == "" {
			return nil, fmt.Errorf("%s: task_id is required", c.Name())
		}
		switch c.op {
		case "inspect":
			value, err = call.Svc.Tasks.Inspect(ctx, call.ConversationID, args.ID)
		case "stop":
			value, err = call.Svc.Tasks.Stop(ctx, call.ConversationID, args.ID)
		case "wait":
			if args.Timeout == 0 {
				args.Timeout = 30
			}
			if args.Timeout < 1 || args.Timeout > 300 {
				return nil, fmt.Errorf("task_wait: timeout_seconds must be 1–300")
			}
			waitCtx, cancel := context.WithTimeout(ctx, time.Duration(args.Timeout)*time.Second)
			value, err = call.Svc.Tasks.Wait(waitCtx, call.ConversationID, args.ID)
			// waitCtx.Err() must be read before cancel(): cancel overwrites
			// the deadline state with context.Canceled.
			waitExpired := waitCtx.Err() == context.DeadlineExceeded
			cancel()
			if err != nil && errors.Is(err, conversation.ErrTaskWaitInterrupted) {
				// Another task in this chat finished while the foreground turn
				// was blocked in task_wait. The waiter returns early with the
				// waited task's nonterminal record; end the current reply so
				// the host's completion boundary can deliver the finished
				// task's outcome, then wait again if needed. The waited task
				// keeps running; nothing here consumes or acknowledges the
				// sibling completion.
				value, err = call.Svc.Tasks.Inspect(ctx, call.ConversationID, args.ID)
				if err == nil {
					if rec, ok := value.(conversation.TaskRecord); ok && !rec.Terminal() {
						note := fmt.Sprintf("\n(wait interrupted: another task in this chat finished while this turn was waiting; task still %s and running independently — end this reply so the finished task's outcome can be delivered, then wait again if needed)", rec.Status)
						data, mErr := json.Marshal(value)
						if mErr != nil {
							return nil, mErr
						}
						return capabilities.NewTextResult(string(data) + note), nil
					}
				}
			}
			if err != nil && waitExpired && errors.Is(err, context.DeadlineExceeded) {
				// A wait deadline is a normal bounded-wait outcome, not a task
				// error: the tool contract says timeout stops waiting, not
				// execution. Report the current task status instead of failing
				// so quiet UIs do not leak a timeout error row. Only the wait
				// deadline qualifies — owner cancellation and arbitrary
				// errors still propagate untouched below.
				value, err = call.Svc.Tasks.Inspect(ctx, call.ConversationID, args.ID)
				if err == nil {
					if rec, ok := value.(conversation.TaskRecord); ok && !rec.Terminal() {
						note := fmt.Sprintf("\n(wait stopped after %ds: task still %s; the task keeps running independently — wait again or inspect for the outcome)", args.Timeout, rec.Status)
						data, mErr := json.Marshal(value)
						if mErr != nil {
							return nil, mErr
						}
						return capabilities.NewTextResult(string(data) + note), nil
					}
				}
			}
		default:
			return nil, fmt.Errorf("unknown task operation %q", c.op)
		}
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return capabilities.NewTextResult(string(data)), nil
}
