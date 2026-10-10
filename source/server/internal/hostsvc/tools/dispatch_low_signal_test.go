package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"

	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/dispatch"
	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
)

// turnsProvider scripts one slice of stream events per StreamChat call.
type turnsProvider struct {
	name  string
	turns [][]llm.StreamEvent
	calls int
}

func (p *turnsProvider) Name() string { return p.name }
func (p *turnsProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *turnsProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *turnsProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	if p.calls >= len(p.turns) {
		return &sliceStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventTextDelta, TextDelta: "Done."},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}}, nil
	}
	ev := p.turns[p.calls]
	p.calls++
	return &sliceStream{events: ev}, nil
}

// executingStub is a registry tool that records actual executions.
type executingStub struct {
	name  string
	perm  agenttools.Permission
	calls int
}

func (s *executingStub) Name() string                      { return s.name }
func (s *executingStub) Description() string               { return "executing stub" }
func (s *executingStub) Permission() agenttools.Permission { return s.perm }
func (s *executingStub) Schema() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (s *executingStub) Execute(_ context.Context, _ json.RawMessage) (*agenttools.Result, error) {
	s.calls++
	return &agenttools.Result{Type: agenttools.ResultText, Text: "ok"}, nil
}

func globCallTurn(id, input string) []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: id, ToolName: "Glob"},
		{Type: llm.EventToolUseInputDelta, TextDelta: input},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}
}

// captureStdoutLog redirects the standard logger for the duration of a test
// body and returns the captured text.
func captureStdoutLog(t *testing.T, body func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)
	body()
	return buf.String()
}

// A single read-only tool call that succeeds must not appear in the failure
// log at all, and the neutral stdout telemetry must report accurate metrics:
// the distinct tool NAME count and the actual invocation count.
func TestRunAgenticDispatch_LowSignalNeutralCountsAccurate(t *testing.T) {
	svc := New(nil, nil, nil, nil)
	logPath := installTestFailureLog(t, svc)
	glob := &executingStub{name: "Glob", perm: agenttools.PermR}
	r := agenttools.NewRegistry()
	r.MustRegister(glob)
	svc.SetRegistry(r)

	var out string
	var res dispatch.Result
	var err error
	out = captureStdoutLog(t, func() {
		res, err = svc.RunAgenticDispatch(t.Context(), dispatch.Spec{
			Mode:           dispatch.Agentic,
			Task:           "Look around once.",
			Tools:          []string{"Glob"},
			MaxIterations:  10,
			ConversationID: "parent",
		}, inference.Selection{Provider: &turnsProvider{name: "once", turns: [][]llm.StreamEvent{
			globCallTurn("g1", `{"pattern":"*.go"}`),
		}}}, "test-model")
	})
	if err != nil {
		t.Fatalf("single-read dispatch must succeed, got %v", err)
	}
	if res.Text != "Done." {
		t.Fatalf("result text = %q", res.Text)
	}
	if glob.calls != 1 {
		t.Fatalf("Glob executed %d times, want 1", glob.calls)
	}
	if !strings.Contains(out, "distinct_tool_names_count=1") || !strings.Contains(out, "tool_call_count=1") {
		t.Errorf("neutral telemetry should report distinct_tool_names_count=1 and tool_call_count=1, got: %q", out)
	}
	if !strings.Contains(out, "called=[Glob]") {
		t.Errorf("neutral telemetry should name the called tool, got: %q", out)
	}
	logData := readFailureLog(t, logPath)
	for _, banned := range []string{"dispatch.degraded", "low_signal"} {
		if strings.Contains(logData, banned) {
			t.Errorf("successful single-read dispatch must not be in the failure log (%s): %s", banned, logData)
		}
	}
}

// CalledTools counts distinct tool NAMES, so repeated invocations of the same
// tool previously read as "<=1 tool call". The invocation-count metric must
// keep a five-invocation run from being labeled a one-call run at all.
func TestRunAgenticDispatch_RepeatedSameToolNeverFalselyLabeledOneCall(t *testing.T) {
	svc := New(nil, nil, nil, nil)
	logPath := installTestFailureLog(t, svc)
	glob := &executingStub{name: "Glob", perm: agenttools.PermR}
	r := agenttools.NewRegistry()
	r.MustRegister(glob)
	svc.SetRegistry(r)

	var err error
	out := captureStdoutLog(t, func() {
		_, err = svc.RunAgenticDispatch(t.Context(), dispatch.Spec{
			Mode:           dispatch.Agentic,
			Task:           "Look around a lot.",
			Tools:          []string{"Glob"},
			MaxIterations:  10,
			ConversationID: "parent",
		}, inference.Selection{Provider: &turnsProvider{name: "many", turns: [][]llm.StreamEvent{
			globCallTurn("g1", `{"pattern":"*.go"}`),
			globCallTurn("g2", `{"pattern":"*.md"}`),
			globCallTurn("g3", `{"pattern":"*.txt"}`),
		}}}, "test-model")
	})
	if err != nil {
		t.Fatalf("repeated-read dispatch must succeed, got %v", err)
	}
	if glob.calls != 3 {
		t.Fatalf("Glob executed %d times, want 3", glob.calls)
	}
	if strings.Contains(out, "low-signal run") {
		t.Errorf("a 3-invocation run must not be labeled a low-signal (<=1 call) run, got: %q", out)
	}
	logData := readFailureLog(t, logPath)
	for _, banned := range []string{"dispatch.degraded", "low_signal"} {
		if strings.Contains(logData, banned) {
			t.Errorf("successful repeated-read dispatch must not be in the failure log (%s): %s", banned, logData)
		}
	}
}

// Terminal output-truncation exhaustion must propagate to the parent with the
// partial handoff and transcript pointer — not as a silent nil-success — and
// must be classified distinctly (output_truncated) from a generic tool failure.
func TestRunAgenticDispatch_TruncationExhaustion_PropagatesHandoff(t *testing.T) {
	svc := New(nil, nil, nil, nil)
	logPath := installTestFailureLog(t, svc)
	r := agenttools.NewRegistry()
	r.MustRegister(&executingStub{name: "Write", perm: agenttools.PermW})
	svc.SetRegistry(r)

	truncatedTurn := []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: "w1", ToolName: "Write"},
		{Type: llm.EventToolUseInputDelta, TextDelta: `{"path": "/repo/x.go", "content": "package ui\n\nfunc bigThing() {`},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
	}
	var res dispatch.Result
	var err error
	out := captureStdoutLog(t, func() {
		res, err = svc.RunAgenticDispatch(t.Context(), dispatch.Spec{
			Mode:           dispatch.Agentic,
			Task:           "Write the big file.",
			Tools:          []string{"Write"},
			MaxIterations:  10,
			ConversationID: "parent",
		}, inference.Selection{Provider: &turnsProvider{name: "truncating", turns: [][]llm.StreamEvent{
			truncatedTurn, truncatedTurn, truncatedTurn, truncatedTurn,
		}}}, "test-model")
	})
	_ = out
	if err == nil {
		t.Fatal("repeated output truncation must abort with an error, not a silent success")
	}
	var le *llm.Error
	if !errors.As(err, &le) {
		t.Fatalf("abort should carry a typed llm.Error, got %T: %v", err, err)
	}
	if le.Class != llm.ErrOutputTruncated {
		t.Errorf("terminal repeated output truncation must be classed ErrOutputTruncated, got %q", le.Class)
	}
	if le.Class == llm.ErrTokenBudgetExhausted {
		t.Error("output truncation is a different failure than token-budget exhaustion")
	}
	if res.Text == "" {
		t.Fatal("partial handoff text must be propagated to the parent, got empty Result.Text")
	}
	if !strings.Contains(res.Text, "Partial work") || !strings.Contains(res.Text, "transcript") {
		t.Errorf("handoff should name the incomplete work and the transcript, got: %q", res.Text)
	}
	if !strings.Contains(err.Error(), res.Text) {
		t.Errorf("the returned error should carry the handoff for the parent tool_result, err: %q", err.Error())
	}
	logData := readFailureLog(t, logPath)
	if !strings.Contains(logData, "dispatch.tool_loop_failed") {
		t.Errorf("genuine loop failure must still be logged, got: %s", logData)
	}
	if !strings.Contains(logData, "output_truncated") {
		t.Errorf("failure log should classify the truncation distinctly, got: %s", logData)
	}
}

// Genuine failures must still be logged as degraded events — the removal of the
// low_signal heuristic must not touch the suspicious no-op policy.
func TestRunAgenticDispatch_SuspiciousNoOpStillLoggedDegraded(t *testing.T) {
	svc := New(nil, nil, nil, nil)
	logPath := installTestFailureLog(t, svc)
	svc.SetRegistry(regWith(permStub{"Edit", agenttools.PermW}, permStub{"Glob", agenttools.PermR}))

	_, err := svc.RunAgenticDispatch(t.Context(), dispatch.Spec{
		Mode:           dispatch.Agentic,
		Task:           "Edit the file.",
		Tools:          []string{"Edit", "Glob"},
		MaxIterations:  1,
		ConversationID: "parent",
	}, inference.Selection{Provider: textOnlyProvider{}}, "test-model")
	if err == nil {
		t.Fatal("expected suspicious write/execute no-op dispatch to fail")
	}
	logData := readFailureLog(t, logPath)
	for _, want := range []string{"\"event\":\"dispatch.degraded\"", "\"error_class\":\"suspicious_noop\""} {
		if !strings.Contains(logData, want) {
			t.Errorf("genuine suspicious no-op failure must still be logged as degraded; missing %s in %s", want, logData)
		}
	}
}

func TestRunAgenticDispatch_TruncatedFinalPreservesPartialText(t *testing.T) {
	svc := New(nil, nil, nil, nil)
	svc.SetRegistry(regWith(permStub{"Read", agenttools.PermR}))
	p := &turnsProvider{name: "partial-final", turns: [][]llm.StreamEvent{{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, TextDelta: "Partial finding: the archive"},
		{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
	}}}
	res, err := svc.RunAgenticDispatch(t.Context(), dispatch.Spec{Mode: dispatch.Agentic, Task: "inspect", Tools: []string{"Read"}, MaxIterations: 4, ConversationID: "parent"}, inference.Selection{Provider: p}, "fixture")
	var le *llm.Error
	if !errors.As(err, &le) || le.Class != llm.ErrOutputTruncated {
		t.Fatalf("want typed truncation: %v", err)
	}
	if !strings.Contains(res.Text, "Partial finding: the archive") || !strings.Contains(res.Text, "transcript") || res.Suspicious {
		t.Fatalf("lost partial work: %+v", res)
	}
	if p.calls != 1 {
		t.Fatalf("unexpected retry: %d", p.calls)
	}
}
