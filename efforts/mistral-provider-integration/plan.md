# First-class Mistral provider with a shared account-aware catalog

Implement the approved spec: one Mistral catalog assembled from configured accounts, with account access shown per model and account-specific metadata retained underneath. Account filtering determines available choices; inference never silently changes accounts. No further audit delegation. Implementation work may be delegated only for well-specified edits, not additional audit work. No real key or paid request is required for automated completion; live verification is a separately reported gate.

## Phase 1 — Metadata and account-access contracts

Objective: establish the data contracts needed for one combined catalog without merging account-specific evidence. Files: source/server/internal/catalog/catalog.go, source/server/internal/modelmetadata/modelmetadata.go, source/proto/agent.proto, generated bindings through the repository's generation workflow, and associated contract tests. Preserve existing public/downloadable source behavior and field numbers. Tests cover absent values, account observations with differing metadata, and backward-compatible decoding.

- [x] Establish an isolated implementation worktree from the approved planning commit and read the executing-plans protocol
- [x] Trace the remaining exact transport and selection consumers directly before changing their contracts
- [x] Add model metadata presence semantics for unknown capabilities and prices, display names, aliases, and lifecycle information
- [x] Define per-account access and observations under one provider/model catalog entry, retaining profile and endpoint ownership plus credential-generation freshness internally without exposing keys
- [x] Extend transport additively for combined access information, account-filtered results, metadata details, and discovery status; keep old callers conservative
- [x] Extend evidence identity to distinguish the actual account configuration used for execution
- [x] Generate bindings and add serialization and compatibility tests; verify affected client and server packages compile

## Phase 2 — Authenticated discovery and master catalog

Objective: fetch and aggregate authenticated Mistral model lists safely. Files: new source/server/internal/mistralcatalog/ package, source/server/internal/catalog/ contracts as necessary, and focused fixtures/tests. Use the existing HTTP stack, not a new SDK. Expected fixture outcome: accounts A and B listing X/Y and X/Z produce three combined models, with X accessible to both, Y only to A, and Z only to B. If X has different metadata, each account retains its own values.

- [x] Implement bounded authenticated model discovery with cancellation, timeout, response-size limits, redirect refusal, and sanitized status errors
- [x] Parse documented base and fine-tuned model shapes, absent fields, capabilities, context limits, aliases, and retirement/replacement metadata
- [x] Preserve exact IDs; distinguish scheduled retirement from already-retired models and unknown prices from known zero
- [x] Cache observations per account configuration and combine successful current observations into one Mistral catalog with accessible-account lists
- [x] Implement account-filtered list/detail access without revealing other accounts' private model names or metadata
- [x] Keep account-specific capability differences explicit in combined details; never take the largest context or most permissive capability as universal
- [x] Bound and coalesce concurrent refresh work, report per-account discovery failures, and prevent old in-flight responses from publishing after invalidation
- [x] Test the union/filter fixture, conflicting metadata, fine-tuned models, missing fields, malformed responses, authentication errors, partial failure, redirect refusal, cancellation, and response bounds
- [x] Run focused discovery tests and race-enabled concurrency tests

## Phase 3 — Provider setup and credential lifecycle

Objective: make Mistral a normal cloud account and connect all credential changes to catalog/evidence freshness. Files: source/server/internal/cloudcatalog/cloudcatalog.go, source/server/pkg/config/config.go, source/server/internal/cloudfactory/, source/server/internal/server/server.go, source/server/internal/server/cloud_models.go, provider registration wiring under source/server/cmd/, and tests. Tests exercise two configured accounts through real profile-management entry points with fake HTTP endpoints/secrets fixtures.

- [x] Add the Mistral preset with the official base URL and chat-completions flavor; recognize the official host using parsed exact matching
- [x] Reuse existing static-key storage, validation, provider construction, and account onboarding paths; do not migrate custom profiles implicitly
- [x] Wire one shared Mistral catalog into discovery and route ListCloudProfileModels through its selected-account view
- [x] Invalidate only the affected account's observations and evidence on key replacement/removal, endpoint changes, or profile deletion
- [x] Prevent profile deletion/recreation and late responses from reviving obsolete access; remove models when no current account observation remains
- [x] Ensure switching the active profile cannot rebind a pending model selection or change another account's cache ownership
- [x] Test missing keys, key rotation/removal, profile deletion/recreation, endpoint changes, failures during refresh, and concurrent invalidation
- [x] Run provider/configuration/server lifecycle tests and applicable race tests

## Phase 4 — Shared browsing and account-filtered selection

Objective: present one Mistral model list with account access and select models only for the intended account. Files: source/server/internal/server/server.go catalog browse/mapping, metadata/detail handlers identified in Phase 1, source/clients/cli/internal/ui/cloud_section.go, runtime_dashboard.go, related client transport/mapping code, and UI/server tests. The current browse loop deduplicates bare IDs across sources and the mapper assumes chat support; address those assumptions for authenticated hosted results without changing unrelated downloadable behavior.

- [x] Expose combined Mistral entries once with accessible accounts and explicit per-account refresh failures
- [x] Prevent cross-provider bare-ID deduplication from hiding Mistral entries while preserving established local/downloadable inventory handling
- [x] Render account-access information and account filters in shared browsing; keep account model pickers strictly filtered
- [x] Show model details, capability availability, context, aliases, retirement notices, and unknown prices without inventing defaults
- [x] Offer only eligible chat/tool models for agent selection; retain useful diagnostics for pinned inaccessible or retired IDs
- [x] Carry the user's intended account through selection and verify access when applying it; never choose the first accessible account automatically
- [x] Verify hosted entries have no download action and account-filtered views contain no foreign private model names
- [x] Add agent-to-client mapping and UI integration tests for combined browsing, filtering, selection, partial errors, unknown pricing, and stale selections
- [x] Run affected server transport and CLI integration tests, including existing served/downloadable catalog regressions

## Phase 5 — Runtime evidence and inference compatibility

Objective: use metadata for the actual execution account and verify Mistral's chat-completions behavior. Files: source/server/internal/modelevidence/, source/server/internal/modelmetadata/, source/server/internal/server/ evidence consumers, routing/delegation consumers traced directly, source/server/internal/llm/openai/, source/server/internal/cloudfactory/, and focused tests. Expected result: two accounts with the same model ID but different context/vision support retain those differences in both main-chat and delegated execution.

- [x] Resolve context and vision evidence from the selected account's observation; reject ambiguous account-less authenticated lookups rather than probing for the first match
- [x] Connect evidence invalidation to the same account configuration lifecycle as discovery
- [x] Verify conservative behavior when metadata is absent or discovery fails; do not borrow another account's capabilities
- [x] Audit affected pricing/ranking consumers directly and keep unknown Mistral prices out of zero-cost assumptions or manufactured automatic tier assignments
- [x] Build local HTTP fixtures for streamed text, split tool calls, tool-result continuation, usage/errors, and relevant reasoning response shapes through the provider factory
- [x] Probe any compatibility failure before adding narrowly scoped Mistral adaptations; preserve exact tool-call correspondence and avoid speculative reasoning replay
- [x] Test main-chat and delegated account selection with conflicting metadata, credential changes, pinned IDs, and absent evidence
- [x] Run focused evidence, factory, OpenAI-compatible client, and routing integration tests with affected existing-provider regressions

## Phase 6 — Verification, documentation, and delivery

Objective: verify the integrated experience, document limitations, and commit the completed work locally. Files: focused tests from preceding phases, docs/features/cli/README.md, docs/agent/README.md or their linked provider documentation, and this effort's verification notes. Match checks to the changed interfaces: package tests for internals, client/server integration for transport, and race tests for shared caches. Do not claim live compatibility from fixtures.

- [x] Run the end-to-end local fixture scenario: configure two accounts, browse the union, filter/select per account, execute a tool round trip, rotate one key, and confirm no foreign or stale access/evidence remains
- [x] Run all affected server and CLI package tests plus the relevant cross-interface integration tests; build server and CLI using their repository workflows
- [x] Verify existing DeepInfra, Cerebras, Anthropic, custom-endpoint, and downloadable-model behavior with focused regression coverage
- [x] Review security boundaries, generated transport changes, unknown-value handling, partial failures, and account ownership; address findings with regression tests
- [x] Document Mistral setup, the combined catalog/account filters, private model visibility, unknown pricing, and actual verification status
- [x] Record automated results and commit solved implementation units with checkpoint; do not push
- [x] Report the live verification gate separately: use a user-saved profile and separately authorized bounded paid requests if available, otherwise explicitly record that live catalog/chat/tool/vision compatibility remains unverified
- [x] Present completion results, remaining limitations, and local commit/worktree details without marking the provider live-verified unless those checks actually passed
