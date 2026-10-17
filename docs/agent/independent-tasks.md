# Independent delegated tasks

The native agent's public `dispatch` tool and `workflow` alias launch host-owned work. A successful response is an **acceptance receipt**, not the child's answer:

```json
{"task_id":"…","child_id":"…","status":"accepted"}
```

The status can already have advanced when the receipt arrives. Use the task handle to observe the work:

- `task_list` lists tasks belonging to the current chat.
- `task_inspect` retrieves retained state and results.
- `task_wait` explicitly waits for an outcome. Canceling the wait does not stop execution.
- `task_stop` explicitly stops the selected task, without stopping siblings.

Consumers that previously treated dispatch's returned text as the final answer must inspect or wait for the task instead. Synchronous internal consumers, including the dedicated review and research implementations, retain their existing result contracts; the public dispatch/workflow launch contract is the one that changed.

## Lifetime and permissions

Steering the main conversation, finishing its originating turn, and disconnecting the CLI do not stop delegated work. Closing a child tab dismisses its presentation; it is not a stop request. Restarting the agent does stop unfinished execution: persisted tasks become `interrupted`, with available evidence retained. Tools are never automatically replayed after restart. Partial output is not proof that an interrupted tool had no side effects.

Existing routing, budgets, approved tool grants, and policy checks still apply. Task completion does not authorize new tool use. Task controls require host-established chat identity; a caller-provided `conversation_id` does not establish ownership. Unbound external MCP callers are rejected rather than granted access to another chat's tasks.

## Progress and completion

The CLI uses a dedicated, chat-scoped `SubscribeTasks` stream, independent of the main turn. It receives authoritative lifecycle snapshots and durable progress records. Reconnect cursors avoid duplicate records, and stale events from a previously selected chat cannot update the new chat. Progress is currently published at persisted model-response and tool-event boundaries, not token-by-token.

A child finishing while the main agent is busy leaves a durable pending notification. The host supplies it at a safe turn boundary rather than canceling active work. When idle, the host can run a completion-handling turn without a connected CLI. Pending tool approvals remain gated and available for reconnection. Interrupted completion handling leaves the result inspectable rather than repeatedly triggering automatic retries.

## Implementation notes and remaining acceptance work

Task status, child progress, restart reconciliation, and host completion handling have regression coverage. Host-generated main-chat replies now arrive through the normal conversation attachment, separately from child progress. Event cursors prevent duplicate replay, and saved-turn identities prevent a completed reply from duplicating restored history. Worker-main completion coverage and the final resource/consumer audit remain outstanding.

The progress transport reads durable storage in bounded pages, with a 200 ms tail interval. It uses transport backpressure rather than an unbounded event queue. Lifecycle scanning is proportional to the chat's retained task history.
