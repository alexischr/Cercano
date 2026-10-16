# Resource-aware recommended setup

Implement the approved spec.md in this directory. Use the existing wizard, persistence, routing, and curated llama-server catalog. All tasks are pending; planning has not verified model candidates or changed application code. Preserve existing configuration and runtime safety contracts. Escalate evidence that contradicts the approved premises or requires a consequential new interface/storage decision before implementing that change.

## Phase 1 — Establish evidence and implementation boundaries

Objective: replace provisional model and rerun assumptions with recorded evidence before selecting shipping defaults. Files: existing wizard and UI implementation under source/clients/cli, source/server/pkg/setupstate, relevant routing/configuration handlers, source/server/internal/localruntime/llamaserver/catalog.go and its tests, and an effort-local evidence document. Tests: characterize current setup rerun, cancellation, persistence, context override preservation, and resource detection using isolated fixtures, never live credentials.

- [x] Create an isolated implementation worktree after plan approval and read applicable development instructions
- [~] Trace wizard entry, UI, account discovery, apply, cancellation, resume, reset separation, and routing persistence
  - [ ] Identify exact files and existing tests before edits; record existing behavior and missing acceptance coverage
  - [ ] Confirm hardware detection distinguishes unified memory, discrete GPU memory, and unknown hardware; identify reusable probes
- [ ] Verify exact Qwen candidate identity and compatible Q6-or-higher artifacts using authoritative model and artifact sources
- [ ] Verify GLM 4.5 Air artifacts, runtime compatibility, and practical memory requirements
- [ ] Calculate candidate weight, unquantized cache, runtime, and application-headroom budgets before runtime experiments
  - [ ] Record candidate context defaults for 48, 64, and 96 GB unified memory and the 128 GB candidate band
  - [ ] Identify supported smaller-machine defaults without extrapolating unified-memory results to discrete GPUs
  - [ ] Record unknowns, hardware availability, download sizes, and validation requirements; do not advertise full context merely because weights fit
- [ ] Resolve contradictory model identities or memory evidence with the user before materially changing recommendations
- [ ] Add focused characterization coverage for existing-user preservation and explicit context overrides

## Phase 2 — Resource-aware recommendation logic

Objective: produce complete, inspectable three-tier recommendations from available accounts, compatible installed models, hardware, and explicit preference. Files: existing wizard/recommendation helpers and catalog, relevant routing configuration types, and adjacent tests located in Phase 1. Tests: deterministic table-driven recommendations using fake accounts and hardware; no paid requests or model downloads.

- [ ] Implement recommendation selection within the existing architecture for cloud-only, mixed, and local-only presets
  - [ ] Populate all three tiers using one supported cloud account when that is all the user has
  - [ ] Use multiple existing accounts where suitable without requiring a secondary provider
  - [ ] Honor local preference and ensure local-only recommendations contain no cloud routes or backups
  - [ ] Allow one suitable local model to serve all three tiers and reuse compatible installed artifacts
- [ ] Encode only evidence-supported local model, quantization, and context defaults
  - [ ] Distinguish unified-memory and discrete-GPU fit and handle missing or insufficient hardware information honestly
  - [ ] Preserve intentional context overrides and enforce existing allocation and compatibility checks
  - [ ] Prefer explicitly recommended smaller context over silent precision or cache changes
- [ ] Keep recommendation generation free of configuration writes, downloads, and runtime restarts
- [ ] Test account combinations, preferences, no usable resources, model reuse, memory boundaries, unknown hardware, and override preservation

## Phase 3 — Recommended setup experience

Objective: introduce tiers, discover resources, recommend, and preview rather than require a routing-strategy questionnaire. Files: source/clients/cli/internal/wizard/wizard.go, its tests, the existing setup UI/rendering files identified in Phase 1, and source/server/pkg/setupstate where needed. Tests: navigation and UI-state tests across new-user, existing-account, and local-only paths, including back navigation and validation errors.

- [ ] Add a concise branded introduction to Deep reasoning, Everyday development, and Lightweight tasks
- [ ] Discover existing resources and provide account connection or local setup when needed
- [ ] Present a ready-to-use recommendation with optional customization
  - [ ] Ask preference questions only where answers affect assignments
  - [ ] Let working cloud users start without a local download, with local setup available later
  - [ ] Make all tier assignments, cloud use, backups, context recommendations, and download requirements visible
- [ ] Add an explicit Use recommended setup confirmation after preview
- [ ] Explain that assignments remain editable and link to existing configuration surfaces
- [ ] Test navigation, preference changes, preview accuracy, and absence of hidden download or restart side effects

## Phase 4 — Safe apply and guided re-entry

Objective: apply confirmed changes using existing persistence paths and make rerun discoverable without conflating it with reset. Files: existing setup apply/server handlers and tests, CLI setup entry points and configuration UI, source/server/pkg/setupstate, and relevant wizard persistence tests. Tests: interface-level preview/apply and rerun tests with temporary configuration and fake credential stores.

- [ ] Connect confirmed recommendations to existing routing and configuration persistence
- [ ] Show changes against current assignments before replacing them
- [ ] Preserve accounts, credentials, unrelated settings, permission choices, and intentional overrides
- [ ] Expose Run setup again through the existing guided-setup path and document existing command entry points
- [ ] Handle cancellation, interruption, resume, and older saved wizard states safely
- [ ] Test rerun accept/cancel, apply failure, interrupted setup, old saved progress, and unchanged unrelated configuration
- [ ] Confirm local-only persistence cannot introduce a cloud backup and that rerun never invokes destructive setup reset

## Phase 5 — Validate models and integration

Objective: verify actual runtime behavior and user-facing setup paths without overstating unsupported hardware results. Files: adjacent wizard, routing, catalog, and runtime tests; effort-local evidence and validation notes. Tests: focused package tests plus interface integration coverage; bounded runtime smoke tests only where appropriate hardware and artifacts are available.

- [ ] Run focused tests for each changed package and build affected CLI/server targets
- [ ] Exercise integrated setup scenarios for one cloud account, multiple accounts, mixed use, local-only use, and existing-user rerun
- [ ] Validate candidate runtime loads and tool-calling suitability against the calculated budgets using available suitable hardware
  - [ ] Compare planned memory/context assumptions with observed runtime allocation and server-confirmed serving capacity
  - [ ] Verify Qwen weight precision and full-context cache settings where full context is actually supported
  - [ ] Verify shared-model tier assignments do not require redundant downloads or unintended concurrent residency
- [ ] Record unavailable hardware or unverified bands explicitly; do not label extrapolation as validation or ship unsupported recommendations
- [ ] Review preservation, cloud-use disclosure, and context safety regressions; fix any failures with focused reproductions

## Phase 6 — Documentation and completion

Objective: leave an understandable setup experience and an auditable verification record. Files: docs/features/setup-wizard/README.md, applicable CLI/configuration help, effort evidence notes, and this plan's task statuses. Tests: documented commands and UI labels checked against implementation; focused regression tests rerun after final fixes.

- [ ] Update documentation for tier meanings, template families, resource discovery, optional customization, and model/context limitations
- [ ] Document Run setup again versus destructive reset and how to change assignments later
- [ ] Complete an acceptance-criteria review against spec.md and report remaining limitations honestly
- [ ] Checkpoint solved implementation units with scoped commits and provide final verification results without pushing
