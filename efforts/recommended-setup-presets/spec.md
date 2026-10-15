# Resource-aware recommended setup

## Problem and motivation

Cercano should get people running with the accounts and hardware they already have, without requiring them to understand routing strategy, acquire a secondary provider, or download a local model before they can start using a working cloud account. The current wizard is organized around a locus choice before cloud and local setup. We want an accessible introduction followed by resource-aware recommendations and an explicit preview.

## Goals

Introduce the three task tiers—Deep reasoning, Everyday development, and Lightweight tasks—with a short branded explanation of delegation. Explain that one account or one suitable local model is enough and that assignments can change later.

Discover existing configured accounts and machine capabilities. Ask new users to connect an account or choose local setup. Recommend populated cloud-only, mixed, or local-only configurations using those resources, with optional customization rather than a required strategy questionnaire. Multiple available accounts may be used, but no secondary provider is required. Model selection must be provider-independent in concept: task tiers express work quality, not a particular vendor or a requirement for three distinct models.

Honor explicit local preference. Local-only configurations must contain no cloud backups. Mixed configurations must make cloud use and backups visible. Capable hardware alone must not initiate a large download; offer local setup separately when a working cloud account provides the fastest start. Reuse compatible installed models when appropriate rather than forcing duplicate downloads.

Preview all three assignments and any backups before applying the recommendation. Make clear that presets are editable starting configurations, not permanent modes with hidden enforcement. Point users to the existing configuration surfaces for later changes.

Expose a discoverable Run setup again entry point using the existing guided-setup path. Preserve accounts and existing settings by default, show proposed assignment changes, and require confirmation before replacing them. Keep guided reconfiguration distinct from destructive setup reset. Verify cancellation, resume, and existing-user behavior rather than assuming documentation proves preservation.

## Local recommendation policy and evidence gate

Use a small number of practical memory bands rather than recommending a different model at every RAM increment. Distinguish unified memory from separate GPU memory and system RAM; nominal system RAM alone is not a model-fit guarantee.

The discussion established provisional validation targets: below 48 GB, select suitable smaller defaults; at 48–96 GB unified memory, evaluate the reported Qwen 27B model as the common default; at 128 GB and above, evaluate GLM 4.5 Air as a stronger candidate. Extra memory in the middle band can support more context and application headroom without changing model. These bands describe a proposed practical default, not a proven minimum or an exhaustive claim about available models.

The reported name “Qwen 3.8 27B” is unverified and must not become a catalog or download identifier without authoritative confirmation. Verify exact identities, quantizations, downloadable artifacts, native context, tool-calling capability, and compatibility with Cercano's pinned runtime. Record sources and assumptions. If the evidence contradicts a target, surface that finding before substituting a materially different recommendation.

Evaluate Q6-or-higher Qwen weights as the quality-first default. Keep the key-value cache unquantized when targeting full context. A model fitting in 48 GB does not establish that its full context fits. Calculate weights, cache, runtime overhead, and operating-system/application headroom before running validation. Prefer recommending a smaller context when memory is tight rather than silently lowering weight precision or quantizing the cache. Show the proposed context explicitly; do not silently shrink an existing user's intentional override or weaken runtime allocation checks.

A local-only preset may assign one capable model to all three tiers. Three task tiers do not require three downloads or concurrent model residency.

## Constraints and compatibility

Build on the existing wizard, setup-state persistence, routing configuration, and curated compatibility catalog rather than introducing a parallel configuration system. Preserve existing accounts, unrelated settings, explicit overrides, permission settings, and credentials. Never read or log secrets during discovery or tests.

Retain the bundled llama-server compatibility gate and existing planned-versus-server-confirmed context safety guarantees. Recommendations must not bypass memory checks or treat planned capacity as confirmed serving capacity. Existing external-provider configuration may be reused where supported; adding a new external-runtime onboarding system is not part of this effort.

Changes to persisted wizard state must handle old saved progress safely. The exact migration mechanics should follow the existing state and persistence interfaces after focused inspection. If this requires a new consequential storage or interface design, obtain approval before implementing it.

Do not automatically overwrite existing routes merely because a new preset is available. Applying a preview is an explicit user action. Downloads and runtime restarts must not occur as undisclosed effects of merely viewing a recommendation.

## Decisions already agreed in conversation

Recommend based on available resources rather than requiring users to design a routing strategy. Use cloud-only, mixed, and local-only template families without requiring every user to navigate a template menu. Ask preference questions only when they materially affect the recommendation.

Make setup reversible and provide nondestructive guided re-entry. Keep customization available after applying a preset.

Simplify local defaults by practical memory bands, subject to exact-model and memory validation. Treat the thread as a source of candidates, not verified release evidence. Retain quality-first Qwen weights and unquantized cache for full-context targets while sizing context honestly.

Use existing architecture for this effort. Read-only inspection has not established a new solution-shape fork requiring an additional decision; implementation must escalate any such fork rather than silently expand scope.

## Current implementation anchors

The documented entry points are cercano -s and cercano --setup, and the setup documentation says rerunning edits existing configuration. The navigation implementation in source/clients/cli/internal/wizard/wizard.go currently sequences StepLocus, conditional StepCloud and StepOpen, and StepDone; persisted types live in source/server/pkg/setupstate.

Relevant local-model seams include source/server/internal/localruntime/llamaserver/catalog.go and detect.go. Existing setup reset is separate, under source/server/internal/setupreset and setupresetcmd. These are starting points for focused implementation reconnaissance, not an assertion that every desired behavior is missing.

The setup documentation at docs/features/setup-wizard/README.md describes curated model compatibility, explicit versus automatic context selection, memory checks, and serving-capacity confirmation. Those safety contracts remain in force.

## Acceptance criteria

A new user with one supported cloud account can accept a complete three-tier recommendation without adding another account or downloading a model. A local-only user receives a usable recommendation with no cloud route or backup. Mixed recommendations disclose local assignments and cloud use, and explicit local preference affects selection.

Existing users can rerun guided setup, inspect changes, cancel without losing existing configuration, or explicitly apply changed assignments while retaining accounts and unrelated settings. Interrupted and older saved wizard states have tested handling.

Local recommendations have verified identifiers and artifacts, explicit memory/context assumptions, and compatibility and task-suitability evidence. Tests distinguish unified memory from discrete GPU configurations and cover the memory-band boundaries, unknown hardware, insufficient memory, duplicate model reuse, and intentional context overrides.

Focused unit tests cover recommendation and navigation logic. Interface-level tests cover preview/apply, preservation, resume, routing persistence, and runtime configuration boundaries. Targeted local runtime smoke tests verify candidate models where suitable hardware is available; unavailable hardware or unverified bands are documented rather than claimed as validated.

User-facing documentation explains the tiers, recommendations, local limitations, configuration editing, and the difference between rerunning setup and resetting it.

## Non-goals

No exhaustive model leaderboard or benchmark program, automatic model downloading solely because hardware is capable, mandatory multi-provider signup, wholesale runtime replacement, unrelated routing redesign, or destructive migration of existing user configuration. No shipping of guessed model names or unsupported full-context promises.
