# Remaining cloud-routing claims — bounded source audit

2026-10-23, existing `docs/core-closure-bounded` worktree. Scope is only the remaining maintained claims in `cloud-routing.md`; prior default/redirect/profile/image-selection/cache-store/snapshot comparisons are not reopened. Read `AGENTS.md`, `audit-core-remaining.md`, and latest `audit-core-guides.md` dispositions. No runtime acceptance, network, inference, build, installation or restart is claimed.

## Family 1 — invocation/class and explicit model/fallback quality

Compared `source/server/internal/capabilities/builtins/dispatch_cap.go:64–120`: omission selects Dispatch, explicit class is validated, difficulty supplies only Tier. `internal/dispatch/engine.go:414–505` defaults independently of role and selects saved assignment; explicit model clears primary Tier while preserving FallbackTier (`:250–258`). `internal/inference/resilience/resilience.go:343–369` preserves an untiered explicit primary model but resolves backup from FallbackTier. Standalone Review supplies its class (`capabilities/builtins/review.go:60–78`), Git land supplies its class (`gitflow_land.go:206`), Research RPC supplies Research (`mcp/server.go:712–719`), project extraction supplies Reconnaissance (`:838–842`), and worker watchdog supplies Watchdog (`worker/watchdog.go:65`). No correction yet required for these compared slices.

Unresolved within this family: complete deprecated Coproc wire alias and all text-helper/local caller paths, enterprise managed-routing override propagation. Symbol locations alone are not end-to-end verification.

## Family 2 — in progress

Found a material distinction: provider streaming permits one narrated same-provider restart after visible text on a retryable failure before message completion (`internal/inference/resilience/resilience.go:700–740,831–889`). This is not whole-turn or cross-destination replay. The guide's absolute interrupted-output wording needs qualification. Host cursor/invalidation/credentials/attribution and whole-turn gates remain to compare.

### Family 2 saved disposition

Corrected the absolute interrupted-response sentence: whole-turn/cross-destination gates are distinct from the stream's single narrated same-provider restart. Compared runner gates at `source/server/internal/runner/core.go:505–519,615–710`; text latches replay-unsafe and retry/fallback require it to remain false. Compared auth identity gate and compatible authorized backup selection in `internal/inference/resilience/auth_recovery.go:12–93`. Host rebuild explicitly carries the old CycleState into the replacement chain (`internal/hostsvc/providers/providers.go:369–393`) and retrieves static keys by `prof.Name` (`:396–404`), supporting cursor retention and key identity claims without redoing cycle assembly.

Unresolved: exhaustive settings/key-rotation invalidation call sites, tool-execution latch wiring, per-attempt sink/model-context attribution and endpoint unknown-context fallback. No whole-family closure claimed.

## Family 3 — identity beyond advisory Anthropic lookup

Compared ChatGPT token claims acquisition (`source/server/internal/chatgptauth/chatgptauth.go:265–286,338–360`), forgiving normalization/decoding (`pkg/accountidentity/identity.go:16–58`), and refresh preservation (`internal/chatgptauth/source.go:83–102`; Anthropic equivalent `internal/anthropicauth/source.go:95–100`). The login RPC passes even empty new identity to Commit (`internal/server/chatgpt_login.go:75`, `claude_login.go:89`); Commit replaces identity while preserving existing same-auth-path profile settings and stable profile key (`internal/hostsvc/config/cloud_login.go:42–84`). The config field is YAML display metadata (`pkg/config/config.go:68–69`). These support acquisition, malformed-identity tolerance, refresh metadata and stale-email replacement claims.

Unresolved: complete UI label collision/ID fallback implementation, settings export transport and account-creation draft behavior. Prior Anthropic advisory-lookup comparison is not repeated. No identity text changed.

## Family 4 — image endpoint evidence and inspection lifetime wiring

Compared exact identity construction (`source/server/internal/modelevidence/identity.go:13–41`): profile, provider, endpoint, route and model, without credentials. Resolver acquisition (`modelevidence.go:35–53,142–209`) checks account evidence first, otherwise shipped evidence plus bounded catalog detail; missing/failed evidence remains unknown. Host passes its live evidence callback into providers (`internal/server/server.go:1270,4706–4713`). Host creates the shared inspector once with live provider/target closures (`:1280–1286`); worker creates its own instance in turn construction (`internal/worker/worker.go:491–507`). `internal/toolstack/vision.go:71–121` constructs one attachment store and a caching locus inspector, reading targets at call time. This compares acquisition/lifetime wiring, not the previously completed image-selection/cache-store algorithms.

Unresolved: exhaustive runner/tool service injection and host reset/conversation teardown paths; authenticated Mistral catalog acquisition/credential invalidation. The worker/host construction boundary is supported, not full lifetime acceptance. No image text changed.

## Family 5 — settings drafts, no-op, failure and navigation/catalog splits

Found stale Cloud draft semantics. `source/clients/cli/internal/ui/cloud_commit.go:72–92,156–201` marks an edited field dirty, immediately invokes profile Save, and clears dirty only after success; failures preserve it, including a pending key if key storage fails after profile creation. Corrected guide to distinguish picker-local state, immediate committed Cloud saves, failed-edit recovery and task drafts. `routing_page.go:163–188,272–329` supports model-tier no-op suppression, restoration on errors, saved task isolation, task dirty comparison and availability warnings. Scope-aware draft detection/discard (`:331–350`) supports cross-page draft separation. Cloud row navigation prompts on dirty state (`cloud_commit.go:150–155`).

Unresolved: all picker cancellation/key bindings and navigation-modal caller paths, save-button enablement/render widths, hosted-vs-local catalog download/RAM paths. No UI acceptance or fixture rerun claimed.

## Family 6 — YAML/settings API atomic validation, references, keys and legacy backup

`source/server/internal/server/routing_settings.go:12–37` clones the candidate inside config mutation, validates before publishing, then rebuilds and reports provider errors as warnings. `pkg/config/routing.go:336–376` rejects missing profile references, duplicate names within each destination, and empty Primary backup entries. `:237–255` accepts the legacy scalar when the list is absent and synchronizes/clears both spellings on list updates. `internal/server/server.go:1003–1030` stores keys separately by profile name and rebuilds for referenced profiles; `:1045–1141` validates profile flavor/endpoint/model choices before upsert and warns on subsequent provider build failure. This supports the guide's API-scoped validation wording, not transactional credential-store+YAML durability. Key rotation and profile-edit refresh call sites now directly compared, supplementing Family 2.

Unresolved: YAML load-time repair/normalization versus API validation, persistence I/O failure reporting/atomic on-disk durability, full removal/rebinding and settings-client transport. No YAML example executed. No text correction required in this family.

## Family 7 — wizard markers, resume and rollback

`source/clients/cli/internal/ui/wizard_cloud_choices.go:10–46` applies only explicitly marked cloud picks; `wizard_page.go:1224–1244` records the marker and persists it. `pkg/setupstate/state.go:35–45,93–114` serializes the marker with the state, reads it on resume, and retains the summary until application clears it. Found obsolete rollback language: `state.go:43–55` explicitly retains Baseline only for decoding, not rollback; `wizard_page.go:655–681` abandons unconfirmed choices by clearing resume state without restoring accounts, and `:245–268` explains credentials cannot be restored and rejects existing account names during API-key creation. Corrected the guide's active rollback claim. The unused snapshot helpers preserve the listed metadata but do not establish a live rollback path.

Unresolved: every current/legacy flow's use of cloud picks at final application, failure handling across multi-RPC confirmation and every resume migration/cancellation key path. No wizard interaction or live rollback acceptance claimed.

## Bounded disposition

All seven requested families received bounded source comparison; none is certified exhaustively closed. Each family's remaining slices are explicit above. Corrections are limited to stream-vs-whole-turn interruption semantics, Cloud immediate-save semantics, and obsolete wizard rollback. Prior completed comparisons were not repeated. README and shared ledgers were not edited or staged by this audit. No tests/build/network/inference/install/restart/application edits were performed; no runtime acceptance claimed.

## Scoped publication checks

- Branch confirmed `docs/core-closure-bounded`.
- Reviewed scoped guide diff: four paragraph replacements only; report is a new file.
- `git diff --check -- docs/cloud-routing.md docs/audit-routing-final-closure.md` passed. Explicit whitespace scan of both files (including the untracked report) passed.
- Relative inline filesystem links checked outside fenced code: 1 target, 0 missing. Neither file contains reference-style link definitions. Anchors/external URLs excluded; this is not an anchor/content validation.
- No shared ledger/README staged by this audit; checkpoint scope is only guide and report.
