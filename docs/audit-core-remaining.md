# Remaining core guides — bounded continuation, 2026-10-23

Baseline `9965e5b8716693f33a69f34b87788b0299da48f0`, branch
`docs/core-closure-bounded`. Source comparison only, not live acceptance.
Earlier startup/headless, direct-tool, permission-mode, specialist/features,
decision-fidelity and self-dev completion reports remain authoritative; none is
reopened here. Self-dev has no outstanding maintained-claim family in its saved
report, so no repeated review was performed.

## Batch 1 — cloud routing defaults, redirects, profile quality/image selection

- `source/server/pkg/config/routing.go:19–155,158–184,236–391`: compared
  task defaults, difficulty mapping, destination bindings, ordered legacy
  backups, redirect resolution/validation, routing validation and sparse reset.
  Corrected Chat's default quality from Standard to **Premium**. Unknown
  difficulty maps to Economy; missing class defaults are not inferred from roles.
  The latter invocation-boundary claims still require caller comparison.
- `source/server/internal/inference/router.go:70–152`: compared final destination
  selection and locus enforcement. Secondary/Local do not implicitly cross;
  final Primary uses existing main locus policy. This is selection comparison,
  not whole-turn retry certification or atomic settings API validation.
- `source/server/pkg/config/config.go:175–224` and
  `pkg/config/image_models.go:1–32`: sparse text overrides precede provider
  recommendations, legacy pins have no precedence, and image inheritance now
  exists. Corrected the stale statement that an empty image selection cannot
  inherit a model: selected chat quality, then image/text recommendations are
  candidates; a derived choice does not write an override.
- `source/server/internal/inference/profilechain/chain.go:14–76`,
  `cycle.go:47–170,198–253`, `image_models.go:12–33`, `vision.go:13–69`:
  compared independent Secondary assembly, finite ordered Primary traversal,
  sticky quota cursor/generation fencing, backup quality re-resolution,
  account-scoped image evidence and request guards. Inherited image choices
  also apply independently to backups. Cursor retention through host rebuilds,
  auth/transient resilience and partial-output quota behavior are not certified
  by cycle assembly alone.
- `source/server/internal/toolstack/vision.go:71–140`,
  `internal/visioninspect/locus.go:44–126`, `cache.go:53–130`: compared confirmed
  target selection, cloud-before-local permitted fallback and successful-answer
  cache key (conversation, image ID, normalized question; no model ID).
  Inspection request/store lifetime wiring and provider evidence acquisition
  are not implicitly certified by these sections.

## Batch 2 — README attachment lifetime (no text correction needed)

Compared the image-attachment bullet in `docs/agent/README.md` under **Known
limitations** against `source/server/internal/visionattach/store.go:1–244`
(store creation, Add/deduplication, on-demand Lookup, Clear and Close),
`internal/agent/vision_placeholder.go:18–88` (nil-store/empty-conversation
passthrough and omitted placeholders), `internal/server/server.go:577–589`
(server-owned store closure), and `internal/worker/worker.go:268–285`
(worker-owned store closure and malformed-history rejection).

The documented metadata-only retention, temporary files, process-local IDs,
no cumulative count/byte cap, same-live-ID restoration on reattachment,
lookup misses and owner cleanup match those paths. No README edit was needed.
Worker stores close at turn exit; this is not durable cross-turn attachment
storage. This batch does not certify every image ingestion/inspection call site,
OS cleanup after a crash, or payload memory bounds outside the store. No UI,
provider request, restart or actual crash was exercised.

## Batch 3 — isolation codecs and per-turn configuration

Compared `source/server/internal/worker/wire.go:28–207,214–502` with the
codec/transport and worker lifecycle passages in isolation architecture;
`internal/llm/messages.go:19–51` and `internal/runner/event.go:15–108` supply
the underlying block/event fields. Corrected the all-fields/lossless language:
JSON block fields cross the message codec, but transient ProviderExtras do not;
content is informational and malformed JSON fails. WorkerEvent is a subset:
task-change/authentication kinds and their payloads, plus runner persistence,
epoch/generation/sequence/background metadata, are not mapped. Unsupported
outgoing kinds become UNSPECIFIED; incoming kinds become EventKind(-1).
This documents transport limits, not an application fix or proof that those
features are absent from other host/control paths. Source comments claiming
zero information loss were not treated as evidence over the actual mappings.

`internal/worker/host.go:567–598` builds a fresh snapshot with host-resolved
Local tier IDs, adds a session-pinned profile missing from the normal graph,
and attaches model evidence when the callback exists. Updated the architecture
snapshot call description to match its three-argument signature. Compared
`internal/routingwire/routing.go:74–123`: snapshot normalizes a clone and omits
missing backups; application validates a replacement graph before committing.
`ConfigFromSnapshot` reconstructs effective Local overrides, routing and
sanitized DeepInfra profile stubs, including re-appending nonreferenced stubs
after routing application; invalid routing clears cloud selections.

This closes the snapshot serialization/reconstruction portion of cloud routing's
**Failure, refresh, and attribution** and the corresponding architecture codec
claims, not the entire refresh paragraph. Configuration edit invalidation,
credential retrieval/rotation, provider rebuilding, retry visibility boundaries,
request-level attribution and endpoint evidence acquisition still need their
caller-level comparisons. Credential-on-demand/control proxies and runner core
were not reviewed in this batch. No process isolation acceptance was run.

## Current remaining checklist (supersedes overlapping historical pending lists)

The requested closure remains **incomplete**. Completed source slices above and
in linked earlier reports must not be repeated as pending whole-section audits.
Self-development remains closed within its saved report's stated limitations.

- [ ] README introduction/setup: prerequisites, step 4 model selection and step 5
  interaction, plus capability summaries not already covered by specialist/features
  or startup reports. Do not reopen setup steps 2/3/6 or Quick start launcher,
  interactive launch and headless examples.
- [ ] README Slash commands: remaining handlers and argument behavior, excluding
  the completed direct-tool and permission-mode units.
- [ ] README Cloud setup and Local inference: provider/route inventory, OAuth,
  API key handling and model/runtime defaults; no live eligibility certification.
- [ ] README Architecture/Provider layer/Tool loop/Built-in tools: maintained
  inventories and wiring; Autonomous run completion stop/continuation criteria;
  Persistence restore/resume and the later Permission gating exceptions and UI
  callback claims not certified by the Permission modes unit.
- [ ] README Known limitations other than the attachment-storage bullet;
  Subscription authentication recovery; Conversation-scoped main-chat model
  selection (commands, persistence, availability and background-run boundaries).
- [ ] Cloud routing: invocation/class/default-difficulty callers and explicit
  model override/fallback quality propagation; whole-turn retry/partial-output
  boundaries, auth/transient resilience and sticky cursor across host rebuilds.
- [ ] Cloud routing: identity acquisition/storage beyond the completed advisory
  Anthropic lookup; other provider identity fields; image evidence acquisition
  and complete inspection/cache lifetime wiring beyond compared store/cache code.
- [ ] Cloud routing Editing settings (Model tiers/Task routing/Cloud): draft
  isolation, unchanged/no-op edits, failure preservation/reversion, validation
  and availability warnings, navigation confirmation, hosted/local catalog split.
- [ ] Cloud routing YAML/settings API: atomic validation and duplicate/reference
  rejection at callers, key-store boundary and legacy backup save/load behavior;
  Setup wizard inherited/edit markers, resume and rollback metadata preservation.
  Its fixture-verification claim has not been exhaustively independently rerun.
- [x] Isolation architecture: [remaining maintained-claim source closure](audit-core-isolation-closure.md)
  completes provider construction, credential/control proxies, runner execution/
  persistence and phase/invariant reconciliation. Earlier broker/pool/orphan,
  control receiver, host authorization, startup/default, codec and protocol
  comparisons remain bounded completed evidence. Source closure does not certify
  complete-file correctness, live isolation, event parity or crash durability.

## Checks and exclusions for this continuation

The saved Batch 1 corrections were rechecked against TaskChat's Premium default
in `pkg/config/routing.go:50–61`, inherited candidates in
`pkg/config/image_models.go:3–32`, and account-scoped confirmation selection in
`internal/inference/profilechain/image_models.go:12–33`.
Further focused offline test and documentation-check results are recorded below.
No application/dependency edits, installation, network/live provider testing,
paid inference, restart, push, merge or land. Earlier test results in other
reports are historical, not new results here. Source comparison is not live
acceptance, runtime certification or a repair of the codec implementation.

### Final offline checks for this bounded checkpoint

- With `GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`, ran:
  `go test ./internal/visionattach ./internal/worker -run 'Test(Add_|Lookup|Conversations|ID_|MarshalMessage|MarshalEvent|SnapshotConfig|DestinationRedirectWorkerSnapshot|TaskTaxonomyWorkerSnapshot|CompactionTaskWorkerSnapshot|ProgressCategoryRoundTrip)' -count=1`.
  Both packages passed (visionattach 0.286s; worker 0.486s). These selected
  tests check fixtures, not unreviewed call paths or live acceptance.
- Checked seven scoped Markdown files (the three target guides, this report,
  core-guide ledger, agent ledger and publicity ledger): 84 relative inline/
  reference filesystem targets, zero missing. Fragment anchors and external
  URLs were excluded; nothing was fetched.
- `git diff --check` passed. Changes are documentation only. The README had
  no stale attachment claim requiring an edit; its other unchecked families
  remain explicitly listed above. The three cumulative ledgers point here for
  the same completed/remaining split. This checkpoint is partial, not closure.
