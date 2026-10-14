package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/llm"
)

type reviewSequenceProvider struct {
	lengthTextProvider
	actions []string
	calls   int
}

func (p *reviewSequenceProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	if p.calls >= len(p.actions) {
		return nil, errors.New("unexpected extra request")
	}
	action := p.actions[p.calls]
	p.calls++
	args, stop := `{}`, "tool_calls"
	if action == "truncated" {
		args = truncatedInput
		stop = llm.StopReasonLength
	}
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: fmt.Sprint("call-", p.calls), ToolName: "Write"},
		{Type: llm.EventToolUseInputDelta, TextDelta: args},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: stop},
	}}, nil
}

type reviewWriteTool struct {
	countingTool
	path string
	fail bool
}

func (w *reviewWriteTool) Execute(context.Context, json.RawMessage) (*agenttools.Result, error) {
	w.calls++
	if w.fail {
		return nil, errors.New("fixture write failed")
	}
	if err := os.WriteFile(w.path, []byte("preserved work"), 0600); err != nil {
		return nil, err
	}
	return &agenttools.Result{Type: agenttools.ResultText, Text: "wrote file"}, nil
}
func TestTruncationReview_RecoveryAndTruthfulHandoff(t *testing.T) {
	for _, tc := range []struct {
		name       string
		actions    []string
		fail       bool
		truncated  bool
		executions int
	}{
		{"recovered_then_truncated", []string{"truncated", "valid", "truncated", "truncated", "truncated"}, false, true, 1},
		{"executed_failures", []string{"valid", "valid", "valid"}, true, false, 3},
		{"mixed_failures", []string{"truncated", "valid", "truncated"}, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &reviewSequenceProvider{actions: tc.actions}
			w := &reviewWriteTool{countingTool: countingTool{name: "Write", perm: agenttools.PermW}, path: filepath.Join(t.TempDir(), "out"), fail: tc.fail}
			reg := agenttools.NewRegistry()
			reg.MustRegister(w)
			result, err := RunToolLoop(t.Context(), ToolLoopInput{Provider: p, Registry: reg, Permissions: bypassPerms(t), UserInput: "write", MaxIterations: 10})
			if err == nil {
				t.Fatal("expected terminal error")
			}
			var le *llm.Error
			isTruncated := errors.As(err, &le) && le.Class == llm.ErrOutputTruncated
			if isTruncated != tc.truncated {
				t.Fatalf("truncation=%v want %v: %v", isTruncated, tc.truncated, err)
			}
			if w.calls != tc.executions || p.calls != len(tc.actions) {
				t.Fatalf("executed=%d requests=%d", w.calls, p.calls)
			}
			if tc.name == "executed_failures" && strings.Contains(result.FinalText, "Requested but not executed") {
				t.Fatalf("executed failures mislabeled: %s", result.FinalText)
			}
			if tc.truncated {
				b, e := os.ReadFile(w.path)
				if e != nil || string(b) != "preserved work" {
					t.Fatalf("earlier work lost: %q %v", b, e)
				}
				if !strings.Contains(result.FinalText, "1 completed tool calls") {
					t.Fatalf("handoff lacks successful work: %s", result.FinalText)
				}
				if strings.Contains(err.Error(), "5 calls") {
					t.Fatalf("lifetime calls mislabeled unexecuted: %v", err)
				}
			}
		})
	}
}
