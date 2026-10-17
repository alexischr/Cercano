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

Approved by the user: retain a pending approval for reconnection, without executing the gated action. This retains intent and requires reconnectable permission-request lifecycle behavior. Existing grants remain unchanged; disconnected execution never implies approval. Implementation resumed after this approval.


## Durable task foundation and approval barrier

Checkpoint `57f2a6df62e1` adds host-owned task-manager and SQLite task-state foundations. Durable acceptance precedes launch acknowledgement; task identity and reserved child identity survive restart. Terminal outcomes and pending notifications are written together. Wait cancellation does not cancel execution, explicit stop is parent-scoped, shutdown interrupts execution, and restart reconciliation never replays tools. This is not yet wired into public dispatch, the host lifecycle, or worker transport.

Race-enabled full suites passed for `internal/taskrun`, `internal/conversation`, `internal/dispatch`, and `internal/hostsvc/tools`.

Checkpoint `93eb0c88e389` fixes a reproduced duplicate-approval deadlock and adds registration-before-notification support. Focused race-enabled agent/server approval tests and the full permissions broker suite passed. Reconnect prompt retention/delivery itself remains unwired.

## MCP task-control authorization boundary

`mcpadapter.makeHandler` sends capability name and raw arguments to InvokeCapability. `hostsvc/tools.Service.InvokeCapability` constructs a capability Call without ConversationID, and delegates permission gating to the external MCP host. `TestInvokeCapabilityDoesNotBindConversationFromArguments` passed, confirming that even a supplied conversation_id is not a host-bound conversation identity.

New task inspection and stop controls therefore cannot apply the internal agent's parent-conversation authorization rule unchanged on the MCP surface. A security policy decision is required before wiring this interface: bind MCP calls to a host-established conversation/session identity, or explicitly treat the local MCP client as a host-wide operator allowed to address tasks across conversations under its existing external permission gate. The latter must not be inferred merely from caller-supplied identifiers. Recommendation: conversation-scoped binding; additional session-binding implementation is needed.

No task-control or asynchronous dispatch capability has been exposed yet. Existing public dispatch remains synchronous.

### Follow-up: conversation binding transport

The user directed investigation of the actual host binding rather than requesting that information from them. The MCP host uses shared per-server connections, not per-conversation connections. Although the tool loop sets a conversation ID in its local Go context, `mcp_host.conn.call` sends only tool name and arguments. The receiving adapter forwards only capability name and arguments. A transport session is therefore not a reliable conversation scope.

`TestConversationScopeProbe` (checkpoint `74ddb63a6603`) observes two conversation contexts on one connection and reconnects the first: all three calls arrive without metadata, a conversation ID, or a stream-transport session ID. This does not establish behavior for every external MCP client.

The server scope fixture had a string/pointer mismatch; correcting its observation field and assignment makes its intended assertions execute. Race-enabled focused checks now pass for `TestMCPConversationScope*`, `TestInvokeCapabilityDoesNotBindConversationFromArguments`, and `TestConversationScopeProbe` in the server and MCP-host packages.

The user has now explicitly selected per-chat isolation: each external chat may see and manage only the background agents it launched. Application-wide ownership is not allowed. This resolves the ownership-policy question; do not ask it again. A model-supplied conversation_id and a per-connection random ID cannot substitute for a trustworthy per-chat binding when a connection is shared. Existing unrelated synchronous tools need not change.

### Per-chat isolation regression coverage

`TestTaskAccessIsIsolatedByChat` now exercises two owners against the real task manager and SQLite store. Each owner lists only its own task; foreign inspect, wait, and stop return not-found without leaking a record or result. The checks run both during execution and after terminal persistence. Both tasks complete successfully despite foreign stop attempts. `go test -race ./internal/taskrun ./internal/conversation` passed. This proves manager-level isolation only; the plan's authorization task remains in progress until transport binding and public controls enforce the same boundary.

The external-client transport implementation remains to be established. The existing probe demonstrates that Cercano's current MCP transport drops its local chat context; it does not prove that every external client supplies a usable chat identity. Do not claim external per-chat isolation is implemented merely by adding request metadata. Native agent calls already carry host conversation context. Preserve that trusted scope, and require evidence of an external integration's binding before exposing cross-request task access through it.

## Approved unbound-client policy and task controls

The user approved failing closed for asynchronous task operations when a client lacks trustworthy per-chat identity. Missing binding is an integration failure, not normal operation. Do not silently fall back to synchronous dispatch or infer identity from tool arguments or shared transport sessions. Unrelated synchronous capabilities remain unchanged.

Added a separate capabilities.Tasks interface and host task-service adapter, plus task_list, task_inspect, task_wait, and task_stop. Controls require surface-injected ConversationID; a model-provided conversation_id is never used for ownership. Wait is bounded and does not stop execution. Public dispatch itself still uses the synchronous hook pending worker/launch integration. The worker task proxy and external trusted binding remain unwired.

Verified with `go test -race ./internal/capabilities/... ./internal/toolstack ./internal/server ./internal/taskrun`. Tests cover unbound external RPC rejection with no result payload, active/completed foreign-task rejection, and wait cancellation without child cancellation.

## Worker task-control bridge

Added dedicated TaskControlRequest/Response protobuf envelopes and a worker Tasks proxy. The host takes parent ownership from the active worker request, never from tool-supplied ownership fields. Launch transfers only the public task/grant/routing inputs; reserved child IDs and progress callbacks do not cross the boundary. The task manager remains host-owned. Wait requests have a bounded timeout and stream cancellation does not cancel accepted child execution.

`go test -race ./internal/worker ./internal/server ./internal/capabilities/... ./internal/taskrun` passed. The bridge test exercises protobuf round trips, launch/inspect/list, foreign-access rejection, and task completion after the initiating stream disappears. This is not yet full subprocess-death coverage. Public dispatch still awaits the asynchronous contract change; completion wake-up remains pending.

## Public asynchronous dispatch

Public dispatch (and its workflow synonym) now uses Tasks.Launch and returns task_id, child_id, and accepted status, without holding a main-turn progress callback. A conversation_id argument can only match the already-bound owner; it cannot establish identity. The synchronous Services.Dispatch contract remains unchanged for review, research, and other synchronous consumers. Terminal result metadata, tool evidence, suspicious-result flags, and partial errors remain in the persisted task result rather than the launch response.

Race-enabled capability, worker, server, host-tool-service, task-manager, and toolstack suites pass. A real bufconn worker plus deterministic HTTP provider test exercises public dispatch, confirms a write-grant approval, returns from the main turn while the child is blocked, and observes the child's later terminal result. External MCP launch is intentionally unavailable until its trusted chat binding is implemented. Automatic completion delivery remains pending.

## Idle turn arbitration and unresolved external integration

Added Broker.TryBeginTurn for atomic idle-only acquisition. Unlike a HasActiveTurn/BeginTurn pair, it cannot cancel intervening user work. Rejected claims preserve replay and generation; a later human turn still supersedes the background turn. Race-enabled broker/server suites pass, including concurrent claim arbitration. Completion scheduling itself is not wired yet.

External integration requires upstream chat-aware support: the existing stdio MCP path provides neither trustworthy per-chat identity nor an external-main-agent wake interface. Failing closed is approved and implemented, but does not establish the missing integration contract. Need the intended external client/adapter and its available chat-scoped API/source before implementing that boundary; cannot fabricate ownership from tool arguments or silently run a separate native agent on behalf of an external chat.
