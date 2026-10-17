package builtins

import (
	"cercano/source/server/internal/capabilities"
	"context"
	"encoding/json"
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
		return "Inspect this chat's task status and saved result, including errors and partial output."
	case "wait":
		return "Wait up to timeout_seconds (default 30, maximum 300) for this chat's task. Timeout or cancellation stops waiting, not execution."
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
			defer cancel()
			value, err = call.Svc.Tasks.Wait(waitCtx, call.ConversationID, args.ID)
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
