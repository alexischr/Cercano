# Execution notes

## Initial regression slice

Implementation worktree: `/Users/bryancostanich/git_repos/cercano-ai/Cercano-independent-subagent-lifecycle`, branch `feat/independent-subagent-lifecycle`, based on planning commit `909704e57865`.

Added `source/server/internal/server/agentic_lifecycle_test.go`, checkpoint `a69663c86535`. The deterministic blocking child tool observes cancellation of its execution owner. This is a characterization of the shared synchronous primitive, whose cancellation support must remain intact; it is not yet coverage of independent public dispatch.

Passed checks:

- Server: `TestRunAgenticDispatch_RTierDefault`.
- Server: `TestAgenticExecutionHonorsOwnerCancellation`.
- Worker: `TestWorkerRunner_ContextCancel`, `TestBuildWorkerToolSvc_WiresDispatch`, `TestDispatchEventProxy_Cancellation`.
- CLI: `TestCancelCurrentStreamSilently_FinalizesChildTabsBeforeNewTurn`, `TestCancelCurrentStreamWithNotice_FinalizesChildTabs`, `TestSubAgentTerminalEvent_ResolvesStaleInProgressTool`.

The existing CLI tests still require child finalization on main cancellation; those expectations will change only when independent task ownership is wired. No production behavior has changed.

## Trace findings

The shared `capabilities.Services.Dispatch` hook also serves review, research, Git workflows, and other synchronous consumers. Preserve their result contracts when the public dispatch/workflow interface changes.

The worker builds its own tool service and dispatch engine; its dispatch-event persistence proxy is scoped to the worker stream and known conversation identities. Independent public task launch must reach host-owned execution rather than leave children inside the originating worker's lifetime.

Conversation persistence owns child identity, transcript history, and dispatch events. Task terminal state and notification handling must complement these records, not infer liveness from child-tab dismissal or transcript presence.

Main-turn orchestration is in `server.go:streamProcessRequestWithToolLoop`. It calls BeginTurn with the initiating stream context and creates a stream-bound permission requester. The requester sends PermissionRequired directly on that stream, then waits on the permission broker. AttachConversation is explicitly observe-only and does not currently supply a replacement approval path.

## Decision requiring user input

Automatic completion wake-up when disconnected creates a main-agent turn with no initiating approval stream. Existing grants must remain authoritative and no new action may be implicitly approved. For newly gated actions, choose between retaining a pending approval for reconnection (suspending execution until approval) and returning an explicit approval-unavailable result (requiring a later retry).

Recommendation: retain a pending approval for reconnection, without executing the gated action. This retains intent but adds reconnectable permission-request lifecycle behavior. Because this affects permission interaction semantics, implementation is paused for explicit approval rather than selecting it silently.
