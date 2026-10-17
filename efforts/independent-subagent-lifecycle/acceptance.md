# Independent task acceptance evidence

## Contract checks

| Contract | Evidence |
| --- | --- |
| Launch acknowledges durable acceptance, not a final answer | `TestLaunchRequiresDurableAcceptance`, dispatch capability launch-contract tests, `TestPublicWorkerDispatchReturnsBeforeChildFinishes` |
| Child outlives its launching turn and worker connection | `TestLaunchAndWaitCancellationDoNotCancelChild`, `TestWorkerTaskBridgeOwnershipAndLifetime`, public worker launch test explicitly stops the worker service before releasing the child |
| Inspect/wait/stop are chat-scoped | `TestTaskAccessIsIsolatedByChat`, worker ownership tests, capability and unbound MCP rejection tests |
| Stop/completion retain outcome and release runtime handles | `TestConcurrentStopAndCompletionRetainEvidenceAndReleaseExecution`, targeted stop/sibling tests, terminal evidence tests |
| Busy main work is not interrupted; completions coalesce | `TestCompletionWakeCoalescesAndDoesNotReplaceBusyTurn`, broker idle-acquisition tests |
| Idle or disconnected main conversation reacts automatically | `TestTaskCompletionCallbackWakesAfterHumanTurnRelease`, `TestDisconnectedCompletionUsesWorkerMainAndPublishesReply` |
| Canceled handling remains inspectable without an automatic retry loop | `TestSupersededCompletionRetainsEvidenceWithoutAutomaticLoop`, claimed-completion recovery tests |
| Approval remains explicit after disconnect | `TestDisconnectedCompletionWaitsForReconnectedApproval`, pending prompt tests, `TestReconnectedBackgroundApprovalDoesNotOfferToReplayUserPrompt` |
| Restart preserves interrupted work without replaying tools | task-manager reconciliation tests, `TestTaskSubscriptionAfterStoreReopenReportsInterruptedWithoutReplay` |
| Child status/progress survives parent cancellation | task subscription gRPC/SDK tests, CLI task lifecycle and progress tests |
| Automatic parent replies reach the main chat | `TestBackgroundFinalArrivesThroughSDKAttachment`, real-worker disconnected completion test |
| Replay does not duplicate or contaminate a newer reply | cursor tests, paginated 300-event attachment test, foreground-isolation and saved-turn-ID history tests |
| Dismiss does not stop execution or resurrect a hidden tab | task snapshot dismissal tests and CLI dismissal/replay tests |
| Completion leaves partially typed input alone | `TestTaskUpdatesPreserveTypedInput`; existing queue/steering tests also pass |
| Slow/canceled observers do not retain transport work | `TestAttachmentCancellationReleasesBackpressuredObserver`; bounded SDK FIFOs and paged/coalesced server observation |
| Unrelated synchronous dispatch consumers are preserved | dispatch, capability, host-service and worker suites; public launch uses the separate task service while internal dispatch retains `dispatch.Result` |

## Verification performed

Race-enabled suites passed for taskrun, conversation, broker, server, runner,
worker, agentclient, capabilities and adapters, dispatch, host services, protocols,
and the complete CLI UI package. Server command tests passed. Both affected Go
modules build. Regenerating Go protobuf bindings produces identical files.

Tests use deterministic local providers and real gRPC/worker-protocol endpoints.
No paid cloud inference, production model behavior, or manual terminal smoke test
was used as evidence. Worker eviction coverage terminates the worker test service
and its transport, rather than killing a separate production operating-system
process.

## Deliberate boundaries

- External MCP callers lacking a trusted chat binding remain rejected. Supplying
  a conversation ID does not grant ownership. No external client-wide authority
  was introduced.
- Child progress follows persisted model/tool boundaries on a 200 ms tail, not
  token-by-token streaming. Lifecycle scan cost follows retained task history.
- Main attachment replay covers the current broker turn; older completed turns
  are restored from persisted conversation history. Saved turn IDs reconcile
  overlapping history and replay.
- Agent restart interrupts unfinished execution; it never automatically replays
  tool calls or assumes partially executed tools had no side effects.
- Commits remain in the implementation worktree. Nothing was pushed or merged.
