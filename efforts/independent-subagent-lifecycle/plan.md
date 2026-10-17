# Independent Sub-agent Lifecycle

Implement the approved spec in this directory. Public dispatch and its workflow alias return task handles; host-owned children outlive main turns and client connections. Completion is non-interrupting while busy and wakes the main agent while idle or waiting. Restart preserves work but never automatically replays unfinished execution.

Execute directly, honoring the user's request not to delegate implementation. Use a dedicated implementation worktree after planning approval. Follow the repository's mandatory scoped Git delegation where required. Do not push. Stop for approval if implementation exposes a new architectural choice that changes the approved contract.

## Phase 1 — Establish lifecycle regression coverage

Objective: establish minimal reproductions of the current coupling and reusable deterministic test fixtures before replacing it. Files: server dispatch and host-service tests, worker host tests, broker tests, CLI ui queue and subagent_stale_cleanup tests. Tests: controlled child runner that remains blocked while the main turn ends, is steered, or loses its client connection; capture actual cancellation and event behavior without paid inference.

- [x] Read applicable repository instructions and create the implementation worktree from the approved planning commit
- [x] Trace dispatch and workflow consumers, shared synchronous execution callers, persistence migrations, worker launch boundaries, and conversation scheduling hooks
- [x] Add controlled child-runner and event fixtures; reproduce main-turn cancellation of child execution in both execution paths
- [x] Record the current stale-main-event protection and child-tab cleanup expectations so only the obsolete lifecycle expectations change

## Phase 2 — Host-owned tasks and persisted outcomes

Objective: introduce independent task ownership backed by the existing persistence infrastructure. Files: new host-owned task manager and tests under server internals, host-service tools and persistence, dispatch types as needed, existing persistence schema and migrations located in Phase 1. Tests: lifecycle transitions, acceptance failures, terminal-state races, isolation, bounded inspection, startup reconciliation, and cancellation ownership.

- [x] Define stable task identity, parent linkage, accepted/running/terminal status, inspectable output references, and explicit interrupted outcomes
- [x] Persist accepted work before acknowledging launch; ensure launch failures cannot leave fictitious running tasks
- [x] Implement host-owned execution contexts, explicit task stop, task listing and inspection, and cancellable waits that do not cancel the child
- [x] Preserve route metadata, granted and ignored tools, suspicion warnings, errors, budgets, partial transcripts, and final results
- [x] Persist terminal outcomes and pending completion information consistently, with startup reconciliation for interrupted tasks
- [x] Enforce task ownership and existing authorization boundaries on inspection and control
- [x] Verify concurrent stop/completion, shutdown, restart, and repeated terminal updates cannot lose results or leave tasks indefinitely running

## Phase 3 — Dispatch interface and independent worker execution

Objective: expose asynchronous delegation without changing unrelated synchronous tool contracts. Files: capabilities/services.go, capabilities/builtins/dispatch_cap.go and tests, workflow alias registration, capability adapters and service wiring, hostsvc/tools, server/agentic_dispatch.go, worker/host.go and associated worker transport and tests. Tests: public capability acceptance, permission denial, task control, alias parity, and actual worker lifecycle integration.

- [x] Wire public dispatch and workflow to launch accepted tasks and return an unambiguous task handle rather than final text
- [x] Register task listing, inspection, wait, and stop controls on the appropriate agent and Model Context Protocol surfaces
- [x] Preserve synchronous result contracts for review, research, Git workflows, and other internal dispatch-service consumers
- [x] Retain routing, model selection, locality policy, tool-grant approval, and execution budgets without broadening background authority
- [x] Run worker-backed children under task ownership rather than the initiating main worker stream; ensure main-worker eviction cannot kill independent children
- [x] Replace turn-scoped child progress callbacks with task-owned event publication and persisted history
- [x] Verify immediate launch acknowledgement, denied launch, failed startup, partial results, wait cancellation, targeted stop, and in-process/worker parity

## Phase 4 — Non-interrupting completion delivery and wake-up

Objective: integrate durable completion notification with the existing single-main-turn invariant. Files: server conversation orchestration, broker, host task manager and persistence, main-agent turn context assembly, worker main-turn integration, and focused tests. Tests: deterministic busy/idle/waiting cases, completion/user-input races, notification replay, disconnected clients, and multiple completions.

- [x] Make pending completions available to a busy main agent at a safe boundary without canceling inference or tool execution
- [x] Wake an idle or waiting main conversation automatically, including when no CLI stream is connected
- [x] Serialize completion wake-ups with user-driven and already-active main turns; do not use turn replacement as a wake-up mechanism
- [x] Coalesce wake-ups while retaining every task outcome and leaving response priority to the main agent
- [x] Track notification delivery/handling separately from terminal task state; preserve unhandled information across cancellation and reconnect without endless wake-up loops
- [x] Ensure stale main-turn generation checks remain intact while child events remain independent of the originating turn
- [x] Test completion before subscription, during active inference/tools, during explicit wait, at turn release, alongside user input, and after client disconnect
- [x] Test restart reconciliation preserves inspectable interrupted work without restarting children or repeatedly scheduling handled notifications

## Phase 5 — CLI lifecycle and reconnect integration

Objective: display and control independent children accurately without weakening main-stream safety. Files: source/proto/agent.proto where required and generated bindings, server attach/list/control handlers, source/clients/cli/internal/ui/model.go and child-tab/event handling, queue and subagent_stale_cleanup tests. Tests: client/server transport integration and CLI state tests.

- [x] Extend existing task listing, event attachment, and control transport as needed; regenerate bindings with repository tooling
- [x] Keep live child tabs active when the main conversation is steered, canceled, or finishes normally
- [x] Preserve cleanup of genuinely stale main-turn activity; distinguish actual child interruption/failure from a lost main stream
- [x] Rehydrate task status and available progress after reconnect and show interrupted status after agent restart
- [x] Keep dismiss-tab and stop-task semantics distinct
- [x] Verify CLI input can continue while children run, and completion notifications do not steal input or cancel active main work
- [x] Run client/server integration cases covering reconnect, task control, independent event delivery, and worker-backed children

## Phase 6 — Compatibility, documentation, and verification

Objective: prove the agreed contract across supported paths and explain the public behavior change. Files: dispatch tool descriptions and prompts/protocols referencing blocking dispatch, docs/features/cli/README.md, docs/agent/README.md, docs/agent/self-dev.md as applicable, and affected test packages. Tests: focused package suites, race-enabled concurrency tests, server/worker/CLI integration, and module builds.

- [x] Audit descriptions, prompts, protocols, examples, and consumers that assume dispatch returns final text; update them to launch, inspect, and explicitly wait
- [x] Document notification semantics, stop versus dismiss, restart interruption, permissions, and the public dispatch/workflow compatibility change
- [x] Run focused Go tests for dispatch, capabilities, host services/persistence, broker, server orchestration, workers, and CLI UI
- [x] Run race-enabled task-manager, notification-scheduler, and worker lifecycle tests using deterministic fake inference
- [x] Run interface integration tests and build affected server and CLI modules, including generated binding checks
- [x] Verify the acceptance matrix: launch-and-continue, steering survival, disconnected execution, inspect/wait/stop, busy notification, idle wake-up, coalesced completion, reconnect, and restart interruption
- [x] Review resource cleanup, authorization isolation, result retention, and preservation of unrelated synchronous tools
- [x] Checkpoint solved units with explicit paths, report test results and remaining limitations, and leave changes unpushed
