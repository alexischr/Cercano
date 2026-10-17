# Independent Sub-agent Lifecycle

## Problem and motivation

Delegated work currently runs as part of a main-agent tool call. Dispatch waits for the child, execution inherits the main turn's cancellation context, worker-backed execution belongs to the parent worker stream, and the CLI retires child tabs when the main turn is canceled. As a result, continuing or steering the main conversation can stop delegated work rather than letting the main agent supervise it independently.

Child conversations, transcripts, granted-tool metadata, and progress history already exist. They are useful foundations, but stored child conversations are not a live task manager.

## Goals

Dispatch launches independent work and promptly returns a stable task handle after successful acceptance. The main agent remains free to work, talk with the user, or launch other tasks. It can inspect a child's status, transcript, partial output, and final result; explicitly wait for results; and explicitly stop execution.

Children survive normal main-turn completion, steering, cancellation of a main-agent wait, and CLI disconnection. Their execution is owned by the host, not by a main-turn context or a client connection. Both in-process and worker-backed paths must implement this contract.

Child completion notifies the main agent without interrupting its current inference or tool execution. When the main agent is busy, completion notifications remain pending and become available at a safe opportunity. The main agent decides when to handle them alongside its existing work. When it is idle or waiting, notification automatically wakes it to inspect results and decide what to do next. Concurrent completions may share a wake-up, but no child's completion may be lost. There remains at most one active main-agent turn per conversation.

Task state and inspectable work persist across an agent restart. Unfinished tasks become interrupted rather than automatically resuming. The main agent can examine preserved work and decide whether to relaunch, revise the assignment, or use the partial result.

## Non-goals

This effort does not implement automatic crash recovery or replay of unfinished tool calls. It does not introduce a general-purpose durable workflow engine, a new main-agent work-queue product, or new permission to execute tools. Live modification of a running child's assignment is not required for the agreed inspect/wait/stop contract.

## Constraints and invariants

Existing model routing, tool grants, approval requirements, policy checks, and execution budgets remain authoritative. A background task must not acquire additional authority simply because its initiating turn ended. Task inspection and control must enforce the appropriate conversation and authorization boundaries.

Launch acknowledgement is not completion. The tool response and descriptions must distinguish accepted work from a completed result, including for the workflow alias and other consumers of the dispatch contract. Launch failure must not produce a misleading running task.

Main-turn cancellation and explicit child cancellation have distinct meanings. Dismissing a child tab does not substitute for stopping execution. Stopping a wait does not stop the child. Actual shutdown or failure must not leave tasks indefinitely marked running.

Child events cannot depend on the generation of the originating main turn. Reconnection must recover current task state and available progress without letting stale main-turn events modify a newer turn. Completion records must remain available even if a client was disconnected at completion time.

Completion wake-ups must be serialized with main-agent work rather than superseding it. A pending notification is information, not an instruction to abandon the current task. A task's recorded terminal outcome and notification handling must be distinguishable so that reconnect or repeated delivery does not create an endless wake-up loop.

Persisted partial output is evidence, not proof that a tool had no side effects. Interrupted tasks must not be silently replayed.

## Decisions

### Task lifetime

Chosen: children survive client disconnect and main-turn changes, but unfinished execution is marked interrupted after an agent restart. The user approved preserving work for the main agent to examine and decide what to do.

| Axis | Mark interrupted after agent restart — chosen | Automatically recover execution |
|---|---|---|
| Complexity | Persist state and output; reconcile unfinished tasks on startup | Add execution checkpoints and recovery rules for provider and tool calls |
| Risk | Restart visibly stops unfinished work | Recovery can repeat writes or external actions |
| Outcome | Independent conversational lifecycle with inspectable interrupted work | Restart-resilient background execution |
| Side effects | Main agent decides whether to relaunch | Recovery must revalidate authority and establish tool completion |
| Verification | Disconnect, cancellation, shutdown, and startup reconciliation | Same cases plus crashes around execution and persistence boundaries |

### Dispatch interface

Chosen: dispatch itself returns a task handle immediately; inspection and waiting are explicit controls. The user approved this instead of adding a parallel background-launch tool.

| Axis | Change dispatch — chosen | Add a separate background-launch tool |
|---|---|---|
| Complexity | One launch contract; update existing consumers | Maintain blocking and independent execution contracts |
| Risk | Old consumers may confuse acceptance with completion | Agents may choose blocking delegation unintentionally |
| Outcome | Independence is the normal delegation model | Independence is opt-in |
| Side effects | Explicit waiting when a result is required | More tool-selection and documentation complexity |
| Verification | Launch, inspection, waiting, completion, and consumer compatibility | Preserve blocking coverage and add independent lifecycle coverage |

### Completion delivery

Chosen by explicit user clarification: notify without interruption, and wake automatically when idle or waiting. Neither unconditional immediate turns nor passive notification requiring another user message meets this requirement. Those are not competing implementation options.

While busy, the main agent continues its current work until it has an opportunity to respond. It may already have a queue of work; a completion does not force priority over that work. While idle or waiting, it wakes automatically. Multiple completions may be considered together without introducing concurrent main turns.

### Execution ownership

Host-owned task lifecycle is required to satisfy the agreed behavior across both execution paths. Detaching only a Go cancellation context is insufficient because the parent worker stream can still close and its process can be killed. Independent execution, event delivery, and cancellation must all follow task ownership rather than main-turn ownership.

## Code audit anchors

The read-only audit identified these boundaries:

- `source/server/internal/capabilities/builtins/dispatch_cap.go`: synchronous dispatch tool contract and progress forwarding.
- `source/server/internal/hostsvc/tools/tools.go`: child tool loop, grants, transcript persistence, and dispatch history.
- `source/server/internal/server/server.go` and `source/server/internal/broker/broker.go`: main-turn ownership, cancellation, and event-generation fencing.
- `source/server/internal/worker/host.go`: worker streams inherit turn cancellation; cancellation can evict the worker.
- `source/server/internal/hostsvc/persistence/persistence.go`: stored child listing and dismissal, rather than live task control.
- `source/clients/cli/internal/ui/model.go`: main-stream input handling and child-tab cleanup.
- `source/clients/cli/internal/ui/subagent_stale_cleanup_test.go`: current regression expectations explicitly finalize child tabs on steering and cancellation; these require targeted revision, not wholesale removal of stale-event protection.

This audit is source inspection, not runtime verification. The implementation plan must define focused unit tests plus server/worker/CLI integration coverage for the changed interfaces.

## Acceptance criteria

A main agent can launch a child, receive its handle before completion, and continue useful work while the child runs. Steering the main conversation, ending its originating turn, or disconnecting the CLI does not stop the child.

The main agent can inspect partial and completed work, wait explicitly, and stop a selected child without stopping unrelated work. Canceling its wait leaves the child running.

A child completing during active main-agent work records a pending notification without canceling that work. A child completing while the main agent is idle or waiting wakes it automatically. Concurrent completion and user activity do not create competing main turns, lose results, or repeatedly wake the agent for an already-handled notification.

Reconnecting the CLI displays accurate child status and available history. Restarting the agent preserves inspectable work and marks unfinished tasks interrupted, without automatically repeating tool calls.

Permissions, budgets, routing, and policy remain enforced in both in-process and worker-backed execution. Existing dispatch consumers and documentation are updated to the new launch-versus-result distinction.
