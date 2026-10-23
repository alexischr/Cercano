# README remaining-family audit — 2026-10-23

Branch `docs/core-closure-bounded`. Source comparison only, not runtime
certification. Only `docs/agent/README.md` and this report are owned by this pass.
Previously completed startup/headless, setup 2/3/6, Permission modes, direct-tool,
attachments, rename/resume parsing and history/clear local handlers are excluded.
No shared ledger or cloud-routing guide changes.

## Increment 1 — onboarding prerequisites

Both `source/server/go.mod:3` and `source/clients/cli/go.mod:3` require Go
1.26.0; the README's Go 1.26/separate modules prerequisite matches. The
Anthropic SDK version in the provider inventory also matches
`source/server/go.mod:8`. No correction for these claims.

## Remaining at this save

Setup 4/5 and capability summaries; remaining slash handlers; Cloud/Local setup;
architecture/provider/tool-loop/built-in inventories; autonomy/later permission
gating; downstream persistence/resume/rename; nonattachment limitations and auth
recovery; session model selection. These are not certified by the initial slice.

## Increment 2 — other slash handlers (partial downstream closure)

Compared maintained entries with `source/clients/cli/internal/slash/`:
`basic.go:10–42`, `color.go:13–74`, `config.go:16–141`,
`settings.go:6–14`, `theme.go:5–12`, `tools.go:14–46`,
`context.go:13–95`, `contextview.go:5–12`, `mcp.go:12–74`.
The model dispatch in `source/clients/cli/internal/ui/model.go:3225–3414`
opens the matching settings/context/MCP pages, starts context RPC commands,
applies prompt color, renders text, or quits. Ctrl+C's actual double-empty-press
contract is at `model.go:3620–3649` (selected/composed input clears first).
Corrected `/help`: it generates registered command help, not a keymap.
Cloud profile creation instructions elsewhere in README incorrectly said to add
profiles in `/cloud`: this handler only lists/selects/stores keys; the Cloud
settings editor is the creation surface already described in setup step 4.
That correction is made in the Cloud increment below, not inferred as cloud
provider/authentication closure.

No discrepancy found in other compared parser/dispatch descriptions. Still
unresolved in this family: server-side compaction/regeneration/elision semantics,
MCP management execution, config update persistence and settings-page details.
The development-mode entry remains covered by the earlier self-dev report;
previously completed handlers were not re-audited.


## Increment 3 — Cloud entry surfaces and provider inventory (partial)

`source/server/internal/cloudfactory/factory.go:19–24,68–115,120–122`
constructs four cloud flavors, including direct subscription token-source routes
for Claude and ChatGPT. Corrected the prerequisite that misleadingly required
Meridian for Claude subscriptions: built-in Max/Pro sign-in is supported.
`source/clients/cli/internal/ui/cloud_section.go:125–148,191` supplies the
Cloud Providers add-provider/sign-in surfaces; `internal/slash/config.go:73–141`
only lists/selects/keys existing profiles. Corrected direct-Anthropic creation
instructions accordingly. Provider flavor inventory matches factory wiring.
`source/server/internal/llm/messages.go:7–13,23–51` additionally defines reasoning;
added that omitted internal block kind to the provider description.

Still unresolved: full setup 4 and UI save behavior, provider presets/defaults,
OAuth/key storage and recovery, ChatGPT allowlist/eligibility assertions, Local
runtime selection/locus enforcement, and cloud resilience/retry claims. Nothing
here is live account/provider certification or a cloud-routing guide audit.

## Increment 4 — downstream persistence/resume/rename

`source/server/internal/conversation/store.go:629–655,830–858,926–947`
writes and reads block JSON and upserts user titles even before the first turn.
`source/server/internal/hostsvc/persistence/persistence.go:649–677,862–869`
forwards resume/rename to Agent; `internal/agent/agent.go:288–313,424–428`
loads stored turns/rebuilds the context meter and forwards the title update.
`internal/agent/history.go:16–46` unmarshals INTERNAL blocks, falls back to text,
and repairs pairing. `internal/compactor/compactor.go:51–64` assembles summary
plus live-tail send views. This disproves the README's provider-native JSON /
“no translation” restore claim and its unconditional “lossless” summary.
Corrected those statements, and documented durable user-title upsert behavior.
The internal schema includes reasoning and display-only metadata and excludes
ProviderExtras (`internal/llm/messages.go:35–51`), not verbatim provider payloads.

Bounded coverage: store and host downstream behavior compared; previously
completed resume/rename parsing was not repeated. Viewport-first pagination,
all client restoration/display details, raw-body retention/pruning policy and
crash durability remain unverified; this is not a byte-for-byte replay promise.


## Final disposition — PARTIAL, budget-bounded

Additional source checks: `internal/inference/provider.go:52–57` confirms the
maintained Chat/StreamChat/Capabilities methods (also Name); the stale introductory
Infer/Stream comment in `llm/provider.go` is not the interface definition and was
not used to alter the README. `internal/agent/toolloop.go:745,1312` calls
StreamChat. `internal/conversation/store.go:950–958` confirms automatic title
updates exclude user titles, and `internal/agent/agent.go:315–328` replays user/
assistant text pairs into the in-memory conversation store.

Covered slices: Go/module/SDK prerequisites; compared slash parser/dispatch
entries in Increment 2; Claude built-in subscription prerequisite and direct API
creation surface; cloud wire-flavor inventory and internal block kinds; stored
turn JSON, history reconstruction/compaction, resume service forwarding and
user-title rename persistence. Corrections are limited to these concrete claims.

**Unresolved families, not closed by this pass:**

- Onboarding setup 4 model selection, setup 5 interaction and other capability
  summaries (except the persistence wording and prerequisites compared above).
- Other slash handlers' downstream execution, especially context mutation, MCP
  lifecycle and configuration persistence; compared parser/UI routing is bounded.
- Cloud/Local provider setup beyond the compared creation surfaces/flavors:
  presets/defaults, OAuth/key handling, local runtime/locus and account claims.
- Architecture and provider wiring beyond the shared-interface/flavor/block
  slices; full tool-loop guards/parallelism and built-in tool inventory/actions;
  README Cloud resilience paragraph remains unverified here.
- Autonomous completion/continuation criteria and later permission-gating
  exceptions/UI callbacks.
- Persistence viewport pagination/client reconstruction and retention/crash
  durability details beyond the bounded downstream slice.
- Nonattachment known limitations, subscription authentication recovery, and
  conversation-scoped main-chat model selection.

Previously completed areas were excluded. Concurrent changes to cloud-routing
and its other-agent report were observed and are neither reviewed nor staged by
this pass. No application changes, broad tests, builds, installation, network,
restart, push or merge. This partial report supersedes only its own intermediate
remaining list, not shared ledger dispositions or other agents' evidence.


## Scoped publication checks

- Read project instructions and latest remaining/core-guide dispositions.
- Reviewed the README-only diff: six concrete corrections; no completed-area
  edits and no application changes.
- `git diff --check -- docs/agent/README.md` passed. Both owned Markdown files
  also passed a line-by-line trailing-whitespace check (including this report
  before this whitespace-clean checks paragraph was appended).
- Offline relative inline/reference filesystem-target check: 16 targets, zero
  missing across the two owned files. Fenced blocks, external URLs and fragment
  anchors excluded; no link fetching or anchor certification.
- Confirmed branch `docs/core-closure-bounded`. Only the README and this report
  are designated checkpoint paths. Other-agent working files are left intact.
