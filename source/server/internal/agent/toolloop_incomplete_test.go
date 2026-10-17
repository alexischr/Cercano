package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"cercano/source/server/internal/agenttools"
	"cercano/source/server/internal/inference"
	"cercano/source/server/internal/llm"
)

// countingTool is a registry tool that records every actual execution so tests
// can prove whether pending (malformed) calls reached the tool or not.
type countingTool struct {
	name  string
	perm  agenttools.Permission
	calls int
}

func (c *countingTool) Name() string                      { return c.name }
func (c *countingTool) Description() string               { return "counting stub" }
func (c *countingTool) Permission() agenttools.Permission { return c.perm }
func (c *countingTool) Schema() json.RawMessage           { return json.RawMessage(`{"type":"object"}`) }
func (c *countingTool) Execute(_ context.Context, _ json.RawMessage) (*agenttools.Result, error) {
	c.calls++
	return &agenttools.Result{Type: agenttools.ResultText, Text: "ok"}, nil
}

func countRegistry(tools ...*countingTool) *agenttools.Registry {
	r := agenttools.NewRegistry()
	for _, t := range tools {
		r.MustRegister(t)
	}
	return r
}

func bypassPerms(t *testing.T) *PermissionStore {
	t.Helper()
	return NewStaticPermissionStore(ModeBypass)
}

// A no-tool final response whose stop reason is "length" was cut off at the
// output-token cap. The loop must NOT report it as a completed answer: the
// result must carry the typed output-truncation error while still retaining
// the partial text, blocks, history, token usage, and called-tools list.
func TestToolLoop_FinalTextLengthTruncation_ExplicitlyIncomplete(t *testing.T) {
	prov := &lengthTextProvider{text: "The bug is in matchTable, specifically the", stopReason: llm.StopReasonLength}
	reg := countRegistry(&countingTool{name: "Grep", perm: agenttools.PermR})

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "find the bug",
	})
	if err == nil {
		t.Fatal("a length-truncated final response must be reported as incomplete, not success")
	}
	var le *llm.Error
	if !errors.As(err, &le) {
		t.Fatalf("error should be a typed llm.Error, got %T: %v", err, err)
	}
	if le.Class != llm.ErrOutputTruncated {
		t.Errorf("error class = %q, want ErrOutputTruncated (nonretryable, nonfailoverable)", le.Class)
	}
	if result.FinalText != "The bug is in matchTable, specifically the" {
		t.Errorf("partial final text must be retained, got %q", result.FinalText)
	}
	if len(result.FinalBlocks) == 0 {
		t.Error("final blocks must be retained")
	}
	if len(result.History) == 0 {
		t.Error("history must be retained")
	}
	if len(result.CalledTools) != 0 || result.ToolCallCount != 0 {
		t.Errorf("called tools/tool call count = %v/%d, want 0/0", result.CalledTools, result.ToolCallCount)
	}
}

// The same requirement on the iteration-cap degradation pass: the final
// no-tool answer at maxIters+1 must be flagged incomplete when it hits the
// output-token cap, while the tools called earlier in the run are preserved.
func TestToolLoop_IterationCapFinalTruncation_ExplicitlyIncomplete(t *testing.T) {
	prov := &capThenTruncatedTextProvider{}
	glob := &countingTool{name: "Glob", perm: agenttools.PermR}
	reg := countRegistry(glob)

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "scan everything", MaxIterations: 1,
	})
	if err == nil {
		t.Fatal("iteration-cap final answer cut at the output limit must be reported as incomplete")
	}
	var le *llm.Error
	if !errors.As(err, &le) || le.Class != llm.ErrOutputTruncated {
		t.Fatalf("error should be typed ErrOutputTruncated, got %v", err)
	}
	if len(result.CalledTools) != 1 || result.CalledTools[0] != "Glob" {
		t.Errorf("called tools must be preserved on truncated cap answers, got %v", result.CalledTools)
	}
	if result.ToolCallCount != 1 {
		t.Errorf("tool call count = %d, want 1", result.ToolCallCount)
	}
	if result.FinalText == "" {
		t.Error("partial final text must be retained")
	}
}

// Terminal exhaustion: a model that repeatedly emits oversized, length-cut
// tool calls never executes anything. The abort after three consecutive error
// iterations must classify this distinctly as output truncation (so callers can
// distinguish it from generic tool failures), and the result must truthfully
// identify the calls: recorded as emitted, but zero executed.
func TestToolLoop_RepeatedTruncatedToolCalls_AbortsAsOutputTruncation(t *testing.T) {
	prov := &alwaysTruncatedProvider{}
	write := &countingTool{name: "Write", perm: agenttools.PermW}
	reg := countRegistry(write)

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "write the dashboard", MaxIterations: 10,
	})
	if err == nil {
		t.Fatal("repeated truncated tool calls must abort the loop")
	}
	var le *llm.Error
	if !errors.As(err, &le) {
		t.Fatalf("abort should carry a typed llm.Error, got %T: %v", err, err)
	}
	if le.Class != llm.ErrOutputTruncated {
		t.Errorf("terminal exhaustion from repeated output truncation must be classed ErrOutputTruncated, got %q", le.Class)
	}
	if !strings.Contains(err.Error(), "output") {
		t.Errorf("abort message should name output truncation, got %q", err.Error())
	}
	if write.calls != 0 {
		t.Errorf("truncated calls must never reach the tool; tool executed %d times", write.calls)
	}
	if len(result.CalledTools) != 1 || result.CalledTools[0] != "Write" {
		t.Errorf("called tools must record the emitted (unexecuted) calls, got %v", result.CalledTools)
	}
	if result.ToolCallCount != 3 {
		t.Errorf("tool call count = %d, want 3 emitted calls", result.ToolCallCount)
	}
	if len(result.History) == 0 {
		t.Error("history must be preserved at the abort")
	}
}

// Terminal exhaustion with completed work earlier in the run: the successful
// tool call, its result in history, and the called-tools record must all be
// preserved when the later truncated-call iterations trigger the abort.
func TestToolLoop_TruncationAbort_PreservesEarlierSuccessfulTool(t *testing.T) {
	prov := &successThenTruncatedProvider{}
	glob := &countingTool{name: "Glob", perm: agenttools.PermR}
	reg := countRegistry(glob)

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "scan then write", MaxIterations: 10,
	})
	if err == nil {
		t.Fatal("expected abort after the truncated iterations")
	}
	var le *llm.Error
	if !errors.As(err, &le) || le.Class != llm.ErrOutputTruncated {
		t.Fatalf("abort should be classed ErrOutputTruncated, got %v", err)
	}
	if glob.calls != 1 {
		t.Errorf("the earlier successful Glob call must have executed exactly once, got %d", glob.calls)
	}
	res := errResultFor(result.History, "ok1")
	if res == nil {
		t.Fatal("history: successful tool_result for ok1 not preserved")
	}
	if res.IsError {
		t.Error("the preserved successful tool_result must not be flagged as an error")
	}
	if len(result.CalledTools) != 2 || result.CalledTools[0] != "Glob" || result.CalledTools[1] != "Write" {
		t.Errorf("called tools = %v, want [Glob Write]", result.CalledTools)
	}
	if result.ToolCallCount != 4 {
		t.Errorf("tool call count = %d, want 4 emitted calls", result.ToolCallCount)
	}
}

// Mixed failures must not be mislabeled: plain malformed JSON (not truncated)
// repeated across iterations still aborts with the generic tool-error
// classification, not the output-truncation one.
func TestToolLoop_GenericMalformedAbort_NotMislabelledAsTruncation(t *testing.T) {
	prov := &alwaysMalformedProvider{}
	write := &countingTool{name: "Write", perm: agenttools.PermW}
	reg := countRegistry(write)

	_, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "write the dashboard", MaxIterations: 10,
	})
	if err == nil {
		t.Fatal("repeated malformed tool calls must abort the loop")
	}
	if !strings.Contains(err.Error(), "3 consecutive iterations of tool errors") {
		t.Errorf("generic malformed abort message must be unchanged, got %q", err.Error())
	}
	var le *llm.Error
	if errors.As(err, &le) && le.Class == llm.ErrOutputTruncated {
		t.Errorf("non-truncated malformed calls must not be classed as output truncation")
	}
}

// The chunk advice works: after being told its oversized call was cut at the
// output limit, a model that resends a smaller valid call succeeds — the loop
// recovers with no retries or JSON auto-repair.
func TestToolLoop_TruncatedThenSmallerCall_Recovers(t *testing.T) {
	prov := &truncatedThenRecoversProvider{}
	write := &countingTool{name: "Write", perm: agenttools.PermW}
	reg := countRegistry(write)

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "write the dashboard", MaxIterations: 10,
	})
	if err != nil {
		t.Fatalf("recovery via a smaller call must succeed, got %v", err)
	}
	if write.calls != 1 {
		t.Errorf("the smaller resent call must execute exactly once, got %d", write.calls)
	}
	if result.FinalText != "done" {
		t.Errorf("final text = %q, want %q", result.FinalText, "done")
	}
	if result.ToolCallCount != 2 {
		t.Errorf("tool call count = %d, want 2 (one truncated, one resent)", result.ToolCallCount)
	}
}

// ToolCallCount must count real invocations, not distinct names: two Glob
// invocations in different turns give one CalledTools entry but count 2.
func TestToolLoop_ToolCallCount_CountsInvocationsNotNames(t *testing.T) {
	prov := &twoInvocationProvider{}
	glob := &countingTool{name: "Glob", perm: agenttools.PermR}
	reg := countRegistry(glob)

	result, err := RunToolLoop(t.Context(), ToolLoopInput{
		Provider: prov, Registry: reg, Permissions: bypassPerms(t),
		UserInput: "scan twice", MaxIterations: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CalledTools) != 1 || result.CalledTools[0] != "Glob" {
		t.Errorf("called tools = %v, want single distinct name [Glob]", result.CalledTools)
	}
	if result.ToolCallCount != 2 {
		t.Errorf("tool call count = %d, want 2 actual invocations", result.ToolCallCount)
	}
	if glob.calls != 2 {
		t.Errorf("Glob executed %d times, want 2", glob.calls)
	}
}

// --- providers ---

type lengthTextProvider struct {
	text       string
	stopReason string
}

func (p *lengthTextProvider) Name() string { return "length-text" }
func (p *lengthTextProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *lengthTextProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *lengthTextProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, TextDelta: p.text},
		{Type: llm.EventMessageStop, StopReason: p.stopReason},
	}}, nil
}

// capThenTruncatedTextProvider calls a tool once, then (on the iteration-cap
// final pass) answers with text cut at the output limit.
type capThenTruncatedTextProvider struct{ calls int }

func (p *capThenTruncatedTextProvider) Name() string { return "cap-then-truncated" }
func (p *capThenTruncatedTextProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *capThenTruncatedTextProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *capThenTruncatedTextProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	defer func() { p.calls++ }()
	if p.calls == 0 {
		return &scriptedStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventToolUseStart, ToolUseID: "g1", ToolName: "Glob"},
			{Type: llm.EventToolUseInputDelta, TextDelta: `{"pattern":"*.go"}`},
			{Type: llm.EventToolUseStop},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}}, nil
	}
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, TextDelta: "Best partial answer: the bug is in"},
		{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
	}}, nil
}

// alwaysTruncatedProvider emits the oversized length-cut Write call forever.
type alwaysTruncatedProvider struct{ calls int }

func (p *alwaysTruncatedProvider) Name() string { return "always-truncated" }
func (p *alwaysTruncatedProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *alwaysTruncatedProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *alwaysTruncatedProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: "w1", ToolName: "Write"},
		{Type: llm.EventToolUseInputDelta, TextDelta: truncatedInput},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
	}}, nil
}

// successThenTruncatedProvider executes one valid Glob call, then emits the
// oversized length-cut Write call forever.
type successThenTruncatedProvider struct{ calls int }

func (p *successThenTruncatedProvider) Name() string { return "success-then-truncated" }
func (p *successThenTruncatedProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *successThenTruncatedProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *successThenTruncatedProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	defer func() { p.calls++ }()
	if p.calls == 0 {
		return &scriptedStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventToolUseStart, ToolUseID: "ok1", ToolName: "Glob"},
			{Type: llm.EventToolUseInputDelta, TextDelta: `{"pattern":"*.go"}`},
			{Type: llm.EventToolUseStop},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}}, nil
	}
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: "w1", ToolName: "Write"},
		{Type: llm.EventToolUseInputDelta, TextDelta: truncatedInput},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
	}}, nil
}

// alwaysMalformedProvider emits the same malformed input but with a normal
// stop reason: a genuine authoring mistake, not a size limit.
type alwaysMalformedProvider struct{}

func (p *alwaysMalformedProvider) Name() string { return "always-malformed" }
func (p *alwaysMalformedProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *alwaysMalformedProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *alwaysMalformedProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventToolUseStart, ToolUseID: "w1", ToolName: "Write"},
		{Type: llm.EventToolUseInputDelta, TextDelta: truncatedInput},
		{Type: llm.EventToolUseStop},
		{Type: llm.EventMessageStop, StopReason: "stop"},
	}}, nil
}

// truncatedThenRecoversProvider emits one oversized length-cut call, then a
// valid smaller call, then a plain text answer.
type truncatedThenRecoversProvider struct{ calls int }

func (p *truncatedThenRecoversProvider) Name() string { return "truncated-then-recovers" }
func (p *truncatedThenRecoversProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *truncatedThenRecoversProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *truncatedThenRecoversProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	defer func() { p.calls++ }()
	switch p.calls {
	case 0:
		return &scriptedStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventToolUseStart, ToolUseID: "w1", ToolName: "Write"},
			{Type: llm.EventToolUseInputDelta, TextDelta: truncatedInput},
			{Type: llm.EventToolUseStop},
			{Type: llm.EventMessageStop, StopReason: llm.StopReasonLength},
		}}, nil
	case 1:
		return &scriptedStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventToolUseStart, ToolUseID: "w2", ToolName: "Write"},
			{Type: llm.EventToolUseInputDelta, TextDelta: `{"path":"/tmp/x.go","content":"small"}`},
			{Type: llm.EventToolUseStop},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}}, nil
	}
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, TextDelta: "done"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}}, nil
}

// twoInvocationProvider calls Glob twice, then answers in text.
type twoInvocationProvider struct{ calls int }

func (p *twoInvocationProvider) Name() string { return "two-invocations" }
func (p *twoInvocationProvider) Capabilities() inference.Capabilities {
	return inference.Capabilities{SupportsTools: true}
}
func (p *twoInvocationProvider) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}
func (p *twoInvocationProvider) StreamChat(context.Context, llm.ChatRequest) (llm.StreamReader, error) {
	defer func() { p.calls++ }()
	if p.calls < 2 {
		return &scriptedStream{events: []llm.StreamEvent{
			{Type: llm.EventMessageStart},
			{Type: llm.EventToolUseStart, ToolUseID: "g1", ToolName: "Glob"},
			{Type: llm.EventToolUseInputDelta, TextDelta: `{"pattern":"*.go"}`},
			{Type: llm.EventToolUseStop},
			{Type: llm.EventMessageStop, StopReason: "end_turn"},
		}}, nil
	}
	return &scriptedStream{events: []llm.StreamEvent{
		{Type: llm.EventMessageStart},
		{Type: llm.EventTextDelta, TextDelta: "done"},
		{Type: llm.EventMessageStop, StopReason: "end_turn"},
	}}, nil
}
