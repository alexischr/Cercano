# Cloud sub-agent compaction omitted assigned-task context

## Observed evidence

Investigated durable dispatch events for task
`task_3c74d3dbf3c59e4280e8ea164a59e8a1`, child
`0ee7fc2e381739e2857b5877` (quiet-task UX implementation).

- The selected model was `zai-org/GLM-5.3-Flash` on the DeepInfra profile.
- Iterations 1–34 returned tool calls. Iteration 35 returned repetitive text,
  `stop_reason=length`, and 8,192 output tokens. The task correctly became failed
  with `output_truncated`, not canceled or silently completed.
- All 11 recorded cloud summarizer requests omitted the assigned task and the
  read-only task-reference section. The summarizer was `openai/gpt-oss-120b`.
- One summary incorrectly identified the goal as preserving working memory;
  others had empty goals or an unsupported awaiting-instructions state. The
  original task nevertheless remained pinned as the first user message in the
  final model request. This was not wholesale deletion of the task.
- Another recent task, `task_3b179ffbbdaa75efbf2b5528f1199ebf`, failed on the same
  model with 8,192 output tokens at iteration 46 after 12 summarizer calls.
  These are two concrete failures, not a population-wide failure-rate estimate.

## Confirmed integration defect and fix

`agent.RunToolLoop` supplies `compaction.WithTaskReference`; local summarization
uses `BuildSummaryPromptWithTask`. The cloud branch in
`loopcompact.BuildSummarizer` instead called `BuildSummaryPrompt`, discarding the
reference. Fix the cloud branch to use the same task-aware prompt builder. No
routing, model, sampling, lifecycle, or permission changes are required.

`TestCloudSummarizerRetainsAssignedTaskReference` exercises the actual cloud
factory and captures the provider request. It failed before the fix and passes
after it. It also checks that an unassigned/main-chat context does not invent an
assigned task. Older tests injected a summarizer and therefore did not exercise
this divergent cloud branch.

## Limits of the conclusion

The omitted reference is reproduced and fixed. It can degrade relevance and
working memory, but the trace does not establish that it caused the final
repetition. The worker was sampling at temperature zero; changing sampling or
reasoning policy without controlled provider evidence would be speculative.
No paid model trial was run, no model route was changed, and the running agent
was not restarted. The patch does not claim that all GLM repetition is resolved.

Verification: race-enabled server, loopcompact, compaction, taskrun, OpenAI
adapter, agent, and CLI UI tests passed. CLI and server builds are checked
separately from deployment.
