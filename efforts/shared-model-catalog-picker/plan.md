# Shared model catalog picker implementation plan

## Goal

Deliver the approved results-and-details catalog popover for cloud account model selection and provider catalog browsing, with actionable credential/discovery states. Follow spec.md; preserve draft/save semantics and exact account/model identity.

## Phase 1 — Establish the implementation baseline

- [x] Create a feature worktree using git_worktree after plan approval; preserve unrelated work and do not implement directly on main.
- [x] Confirm overlay composition, form activation/focus restoration, discovery transport and saved-key/auth semantics; record any material mismatch with the spec before changing scope.
- [x] Add smallest reproducible tests/probes for missing-key messaging, oversized metadata dropdown labels and synchronous discovery behavior, observing current results before fixes.

## Phase 2 — Build the shared catalog component

- [x] Define typed component inputs and outcomes for selection versus browse, exact identity/scope, metadata, eligibility, discovery status and special actions; keep API clients/secrets outside the component.
- [x] Implement concise result rows, search with stable ranking, current-selection marker, independent highlighted details, filters and explicit inheritance/no-image actions.
- [x] Implement wide two-pane, stacked and compact detail layouts using existing theme tokens and no new dependencies.
- [x] Implement keyboard focus, navigation, scrolling, Enter/Cancel, refresh and narrow-view Details/Back, ensuring ordinary query characters are not intercepted as shortcuts.
- [x] Add component tests for navigation/search/resize, long and Unicode IDs, unknown metadata, incompatible rows, custom/unlisted IDs and distinct selection outcomes.

## Phase 3 — Make discovery asynchronous and actionable

- [x] Move cloud catalog RPCs out of synchronous form construction into Bubble Tea commands/messages while preserving appropriate fallback behavior.
- [x] Represent missing saved credentials, unsaved account edits, sign-in required, loading, refresh, ready, empty success, failure and partial failure distinctly; use only reliable error classification.
- [x] Wire Add API key/Sign in actions to existing account controls without silently saving or discarding drafts; show the prerequisite message even when the popover is closed.
- [x] Invalidate and refresh after successful account/key save; guard results with request generations and discard stale responses after account, endpoint, auth or lifecycle changes.
- [x] Test cancellation, responsiveness, duplicate requests, stale cache/late replies, failed save and rapid account switching with deterministic fake-agent responses.

## Phase 4 — Integrate selection and browsing

- [x] Replace cloud economy/standard/premium and image dropdowns with compact launchers for the shared picker, retaining explicit Save, inheritance, no-image override and custom-ID support.
- [x] Enforce account-scoped choices and caller-specific capability requirements without bypassing existing server validation or converting unknown metadata to unsupported/free.
- [x] Replace the existing provider-wide inline catalog fields with the shared component's read-only browse mode; retain per-account observations and partial-failure status.
- [x] Add integration tests covering launch/select/save, launch/cancel, credential-focus/return, save-triggered discovery and browse-without-mutation.

## Phase 5 — Verify presentation and deliver

- [x] Render and review representative ready, missing-key, loading, failure, no-match and partial-catalog states at 120x36, 100x24, 80x24 and 60x18; verify theme contrast and viewport bounds.
- [x] Exercise representative keyboard flows in the terminal where available; distinguish actual interactive verification from automated render fixtures in the completion report.
- [x] Run affected CLI overlay, form and UI tests and build the CLI; if any interface changed, add and run its corresponding integration checks.
- [x] Document the shared component contract and user-facing states; record verification, remaining limitations and deferred consumers.
- [x] Checkpoint solved units on the feature branch and present completion results; do not push, land, restart/install binaries or run paid inference without separate authorization.

## Execution guardrails

Keep the approved results-and-details design. Runtime/download management and conversation-model picker migrations are deferred, not implied by the word shared. The component must have both selection and browse consumers in this delivery. Do not enlarge the scope into routing, credential policy or attachment fixes. Escalate new architectural choices rather than silently redesigning those systems.
