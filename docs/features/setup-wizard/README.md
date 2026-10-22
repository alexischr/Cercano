# Setup Wizard

Setup is **resources → preference → confirmation**, not a routing-template quiz.
It reuses the existing account editor, curated llama-server catalog, wizard resume
file, and routing APIs. No new credential store or parallel configuration system
is involved.

## Step 1 — Cloud providers

Hardware and account discovery are read-only. Setup has one **Add cloud provider**
screen for Claude, ChatGPT/Codex, DeepInfra, Mistral, and other supported providers.
The provider picker starts with an empty search field. Existing accounts with
stored credentials start selected; they can be deselected without deleting them.
Add as many providers and additional accounts with the same provider as needed.
Additional accounts can be configured as backups in `/config`. The shared account
popover returns to step 1, where **Add cloud provider** remains available.
Subscription connections use its OAuth flow, and disconnected existing accounts
open their existing editor for reconnection rather than creating duplicate names.
Provider editors show Connection controls first. **Model tiers** and **Advanced
settings** start collapsed; their section headers expand or collapse the entire
section. They share consistent label and value columns rather than manual label
indentation. Right Arrow or Enter expands a focused collapsed section;
Left Arrow collapses it. Repeated Right Arrow does not toggle it closed.

Successful sign-in replaces the starting status with success feedback and focuses
**Done**. Done returns from the provider editor to setup; it does not accept the
wizard's routing changes. Unsaved edits still require an explicit keep/discard
choice. A new draft is not a failed edit: **Retry saving** appears only after an
actual save error. ChatGPT's canonical and legacy subscription-route labels do not
create a false pending-edit warning after successful authentication.

Credential presence is not proof of current entitlement, quota, or token validity.
The resource summary says **credentials connected**; access is checked when used.
A subscription must support Cercano's integration. Unconnected profiles do not
qualify a cloud setup merely because their metadata exists.

Local eligibility requires **at least 48 GiB of system memory** (shown as the
48 GB product baseline) and a known supported GPU memory topology. Apple Silicon
uses its unified pool; discrete GPUs use the largest individual known pool, never
summed GPU memory or system RAM as extra graphics memory. Unknown probes and
CPU-only/unknown-GPU configurations do not qualify. The selected or curated local
models must also pass the existing preliminary weight-plus-overhead budget screen.
Model sizes and compatibility metadata must be known. Local-model customization is available only after choosing a configuration that
uses local models; explicit model choices are not silently replaced.

This is eligibility, **not runtime-fit validation**. Context, cache, allocation,
and serving readiness still use the existing runtime checks. No new model default
or full-context guarantee is implied. Unsupported hardware probing stays unknown.

## Step 2 — Configure Cercano

Continue from cloud providers refreshes discovery automatically and opens step 2
of 3. There is no separate resource-summary screen or manual Recheck button.
A short capability summary appears above the viable configurations. Local
eligibility explains the detected total system RAM, the 48 GB minimum, GPU
presence, and unified or dedicated GPU memory; it does not claim a wired/free
memory measurement. Account addition stays in step 1, and model customization
comes after choosing a configuration.

Only viable choices are shown:

| Preference | Resources required | Default behavior |
| --- | --- | --- |
| Frontier-first | A selected Claude/OpenAI account with credentials | Primary and delegated work use the frontier account; no local downloads |
| Balanced | At least two of frontier, other hosted providers, eligible local models | Frontier primary when available, hosted routine work when available, local helpers when eligible |
| Cloud open models / Hosted cloud models | A selected non-frontier provider with credentials | Hosted work without requiring a frontier subscription or local downloads |
| Local only | Eligible local hardware and model choices | All inference remains local; cloud routes and backups are removed from the proposed routing, not account storage |

The generic Hosted cloud models label is used for other provider types rather than
assuming every non-frontier provider serves open weights. Descriptions name the
resources that will actually be used. Accounts default to
the active account within their resource class; setup does not benchmark or
optimize account prices. Existing accounts remain available in configuration.
A no-resource result explains the limitation and offers a return to cloud
providers. It cannot apply an unusable setup.

## Step 3 — Review and confirm

The compact review names primary/routine resources, local helpers, existing cloud
backups that remain selected, and required downloads. It explicitly distinguishes
cloud use from local-only operation. **View details** shows task assignments,
redirects, local model choices, and technical readiness limitations.

Existing explicit task assignments are preserved by default, and the review says
they take precedence over template defaults. **Use template task assignments
instead** is an explicit, pending choice to replace them. It does not write until
confirmation. Local model/context overrides, unrelated settings, account metadata,
and credentials are otherwise preserved.

Confirmation rechecks selected credentials/account identity, routing snapshots,
and local capability before the first write. A changed routing snapshot or removed
credential requires reviewing resources again rather than overwriting concurrent
changes. Apply uses the existing sequential APIs; it is **not an atomic transaction**.
A later failure may leave earlier accepted changes saved. Downloads begin only
after confirmed settings have been saved; local inference is not ready until the
required downloads and runtime checks succeed.

Escape backtracks or pauses. Pressing `q` twice discards unconfirmed choices and
clears the resume file. Completed account connections persist, as disclosed before
authentication. Old wizard progress returns to resource discovery while retaining
pending model choices and account names; current resource/preference progress
resumes in place. Neither discovery, migration, nor cancellation invokes setup reset.

## Triggers

- First run: no `~/.config/cercano/config.yaml` (or config present but no
  usable provider) → wizard offers to run before the first turn.
- Explicit: `cercano -s` / `cercano --setup` re-enters the wizard any time;
  re-running edits the existing config rather than starting blank.
- The legacy `cercano setup` Ollama flow (`runSetup` in
  `cmd/cercano/main.go`) is **retired** — `setup` enters this wizard. No path
  detects, installs, or pulls from Ollama during setup.

## Engine posture: llama-server is the open runtime

Cercano ships and supervises `llama-server`. It is the one open-weight runtime
the wizard configures, and open models are acquired as **GGUF downloads**
(direct HuggingFace `resolve/main/...gguf` URLs, downloaded into
`llama_server.model_dirs`). The wizard never checks for a running Ollama daemon
and never pulls from the Ollama library.

Hosted providers such as DeepInfra and Mistral are part of resource discovery.
External local engines remain managed through the existing model/runtime surfaces;
the wizard's local recommendation path uses the curated llama-server catalog.

## Automatic context selection

`llama_server.context_size` is optional. Omit it (or use YAML `null`) for
**automatic selection**; a positive integer is an intentional override. Zero
and negative overrides are rejected. Setup and unrelated settings saves retain
this distinction instead of writing an implicit 8K value.

For a new managed process, selection is:

1. An explicit `llama_server.context_size` override.
2. The applicable model/RAM-profile context setting.
3. A model-specific context launch argument.
4. The model's native context length from GGUF metadata.

There is no universal 8K fallback. Automatic selection requires enough model and
memory information to validate the allocation. Missing evidence or an unsafe
allocation produces an error rather than launching with an arbitrary size or
silently shrinking it. An explicit override still undergoes applicable memory
checks. Context flag aliases and inherited context/cache environment settings
cannot override the checked launch allocation.

A selected size is **planned**, not yet a serving limit. Cercano confirms the
per-request capacity using the running server's properties and slot reports.
Missing or contradictory confirmation blocks inference. Budgeting, research
requests, worker dispatch, and the context meter use confirmed capacity—not a
fresh guess from config. Editing config does not change an already-serving
process's capacity; a subsequent runtime start/restart applies the new setting.
The runtime dashboard distinguishes planned, server-confirmed, and unknown
capacity. Stopped/replaced processes cannot lend their old capacity to a new
request.

### Existing accidental 8K overrides

Older versions could write `context_size: 8192` during an unrelated save, even
when automatic mode was intended. Cercano cannot distinguish that accidental
value from a deliberate 8K override, so it does **not** remove it automatically.
If you know it was accidental, remove only that key from your config and restart
the affected model runtime when convenient. Keep intentional overrides intact.

## Model catalog & the compatibility gate

**The problem this closes.** llama-server (llama.cpp) is not a catalog — it
runs whatever GGUF you hand it, *if its architecture is compiled into the
build*. A GGUF's first four bytes being `GGUF` only proves it's a GGUF
container; the `general.architecture` metadata field inside names the model
architecture, and llama.cpp only loads architectures it supports. Ollama has
built its **own** inference engine, so its library now includes architectures
only Ollama can run (e.g. qwen3-next). "In the Ollama library" therefore means
"works in Ollama," **not** "works in llama-server" — the two have diverged, and
pulling an Ollama-library model into llama-server can fail with "unknown
architecture." The old `ollamacatalog`-as-discovery assumption is retired.

**Two layers, one gate:**

| Layer | Source | Guarantee |
|---|---|---|
| Recommended set (setup + tiers) | Curated list we author & test | Verified on our build |
| Browse / advanced (`/m`) | HuggingFace GGUF index + gate | Gate blocks incompatible |

- **Curated compatibility catalog.** A hand-maintained list of GGUFs we have
  verified load-and-run on Cercano's pinned llama.cpp build, one per open tier,
  with tool-calling flagged. This is the only source setup ever touches, so the
  guided path is structurally foolproof. It supersedes the two-entry
  `defaultCatalog` stub in `llamaserver/provider.go`.
- **HuggingFace browse.** For power users on `/m`, discovery switches from the
  Ollama library to the HuggingFace GGUF index (`hf.co/models?library=gguf`) —
  API-backed, the real home of GGUFs, no Ollama dependency. Because that index
  is large and noisy, browse filters to known-good uploaders
  (bartowski / unsloth / ggml-org) and sorts by popularity, *on top of* the
  gate.
- **The architecture gate** is the shared primitive: before committing a
  multi-gigabyte download, read `general.architecture` from the GGUF header via
  an HTTP Range request (the header holds the metadata KV block; no full
  download needed) and refuse/warn if our bundled llama.cpp doesn't support
  that architecture. The in-tree `headerIdentity` parser in `provider.go`
  already extracts architecture from on-disk files; it needs to also accept a
  remote Range reader. This gate is what would have caught qwen3-next *before*
  the failed pull.

This whole subsystem is a re-architecture of the model-catalog track; the
`ollamacatalog` package and `docs/features/cli/model-catalog-online/design.md`
need a follow-up rewrite to match (Ollama-library discovery → HuggingFace +
gate). This doc records the decision; that track carries the implementation.

## Implementation and verification

- `setupstate` stores non-secret progress, including selected preference and
  excluded accounts; the CLI wizard owns navigation.
- Resource eligibility and preference selection live in `wizard_resources.go`.
- Routing proposals and pre-write revalidation live in `wizard_resource_routing.go`.
- Shared account connection UI remains in `wizard_provider_popover.go` and the
  configuration account editor.
- Tests cover the account/capability matrix, 48 GiB boundary, discrete-memory
  non-summing, confirmation-only writes, explicit override replacement, concurrent
  routing changes, credential removal, hardware changes, resume, old-state migration,
  cancellation, and OAuth reconnection dispatch.

## Historical decisions

The entries below describe previous iterations. The resource-first flow above
supersedes their locus/template-first navigation and eligibility rules.


### 2026-07-11 — RAM-tiered open recommendations + enroll-on-finish (implemented)

Wires the previously planned open-model path end to end:

- **Open recommendations are RAM-tiered and curated.** The dead-but-tested
  `CuratedCatalog.ProfileForRAM` is now wired through a new
  `llamaserver.RecommendedOpenModels(totalRAM)` helper and surfaced on
  `ListRuntimeModels` as `recommended_open_models` (tier → stable inventory id).
  The wizard autofills its open tier slots from it, so every open recommendation
  is a gate-verified model that fits the machine — closing the gap where the
  shipped recs recommended the gate-incompatible `qwen3-coder-next`.
- **Downloads actually enroll on finish.** `applyConfig` calls
  `enrollOpenDownloads`, firing `DownloadRuntimeModel` per distinct open pick
  (deduped, skipping models already on disk, non-fatal) — the finish message's
  "downloads run in the background" promise is now backed by code.
- **Catalog populated.** `llamaserver/catalog.json` carries six curated GGUFs
  across 24 / 48 / 96 / 128 GB profiles; the two-entry `defaultCatalog` stub is
  gone.
- **Readiness = on-disk** (`dispatch.OpenModelReady`) is in place, so routing
  treats a still-downloading open model as absent and crosses to cloud.

### 2026-07-10 — locus-first rewrite

- **Locus is the organizing first question.** The old "open or cloud?"
  `StepPrimary` is removed; the four locus modes carry the whole "how do you
  want to use Cercano" decision, with `cloud_primary` and `open_only`
  recommended, `open_primary` as cost-saver, `cloud_only` discouraged.
- **Catalog: curated set + HuggingFace browse + architecture gate (Option C).**
  Open models come from a curated compatibility catalog we author and test —
  the only source setup touches. `/m` browse switches from the Ollama library
  to the HuggingFace GGUF index, filtered to known-good uploaders. Every
  download passes an architecture gate: read `general.architecture` from the
  GGUF header via a Range request and refuse if our llama.cpp build can't load
  it. Ollama is dropped as a catalog source entirely (still allowed as a
  post-setup external server). The `ollamacatalog` package and the
  `model-catalog-online` design need a follow-up rewrite to match.
- **Cloud covers the download gap via readiness = on-disk.** An open tier
  registers as present only when its GGUF is downloaded; while downloading it
  reads absent, so routing crosses to cloud automatically with no new fallback
  logic. `open_only` has no cover and the completion gate says so.
- **No download screen.** Open-model downloads start in the background at
  finish; `/m` shows progress; the closing message tells the user cloud covers
  the gap until they land.
- **Tier-mapped set, not a single pick.** The open step fills the `.Open`
  slots of the everyday / fast_light / fast_light_text / embedding tiers from a
  recommended, editable set. This also removes the `fast_light_text` compaction
  warning at its source.
- **Cloud step supports multiple profiles** — one required, additional ones as
  backup/switchable.
- **Hard completion gate.** Success is reported only when the locus's required
  path resolves; otherwise an explicit incomplete screen and non-zero exit.
- **Legacy `runSetup` retired**; `cercano setup` enters this wizard.
- **Catalog expansion is in scope.** Per-tier GGUF entries must be added to the
  curated compatibility catalog (superseding the two-entry `defaultCatalog`
  stub) for the recommended set to be real.

### 2026-07-05 — auth & OAuth (carried forward)

- OAuth sign-in flows (ChatGPT, Copilot device flow) run **agent-side**: the
  agent owns the OAuth dance (callback server / device polling), token storage
  (same place API keys live), and request-time refresh. The CLI renders the
  verification URL + user code and waits on flow status via RPC. Tokens never
  cross the gRPC boundary outbound; every client gets the flows for free;
  refresh works with no client attached. Profiles select the adapter via the
  Route field ("direct" | "meridian" | "chatgpt" | …).
- ChatGPT sign-in ships in the default provider list with the "unofficial"
  label; no config flag gate.
- Tier autofill for cloud providers comes from the shipped recommendations
  file; Copilot additionally filters against its live catalog.
- Quitting mid-wizard resumes where the user left off (wizard state persisted
  alongside config; a completed run clears it).

### Cancellation (updated 2026-10-16)

- Press `q` twice to discard unconfirmed routing selections and clear the local
  resume file. `esc` still goes back or pauses for later resume.
- Completed account connections **persist** after cancellation. This is disclosed
  before authentication and in the preview and cancellation prompt. Manage or
  remove connected accounts explicitly in `/config`.
- Cancellation does not delete profiles, restore captured account metadata, or
  change the active account. This also applies to old saved wizard runs containing
  a baseline snapshot: legacy data is readable but never used for rollback.
- New sign-in uses create-only account operations; it cannot silently overwrite
  an existing account's credentials. Existing users can reuse their active account.
- `/setup` is the **Run setup again** entry point; it resumes a paused run or starts
  fresh without resetting configuration.

- **Meridian profiles carry the proxy base URL** (`base_url` +
  `route: meridian`); an empty route on update preserves the existing profile's
  route so route-unaware clients can't demote meridian to direct.

### Recommended-first entry

Setup opens with four selectable templates: **Cloud**, **Cloud + local helpers**, **Local + cloud fallback**, and **Local only**. The current or resource-recommended template is marked. Selecting a template uses discovered resources and preserves existing model choices; an existing active cloud account can reach the preview without signing in again or downloading local models when the selected mode is cloud-only. If an account is needed, setup asks for a connection before previewing the result.

Customization retains the cloud/local strategy and individual model screens. The recommended path skips optional model editing, not consent: only **Use recommended setup** applies the preview. Press Esc from the preview to customize. Assignments remain editable in `/config`, and local models can be managed later through `/m` or `/setup`. Resumed runs use the existing saved steps; the shortcut itself is not a new persisted configuration setting.

### Choosing among connected cloud accounts

Templates default to the active cloud account. When multiple accounts are connected, **Customize cloud account** on the confirmation screen lets you choose another existing account. The preview names the selected account and shows the previous default. The choice is saved with wizard progress but does not activate the account until **Use recommended setup** is pressed. Cancellation discards the pending choice; it does not switch accounts. If the selected account has disappeared or changed provider, acceptance fails rather than silently substituting another account.

This selects the cloud **default**. Explicit task-account routing overrides remain unchanged, and setup does not automatically optimize or distribute tasks across accounts. Use the existing routing configuration surface for those overrides. Accepted templates still apply their previewed placement and model assignments. Account credentials and metadata are not rewritten. Apply uses existing sequential configuration operations, not a new atomic transaction; an error can leave already accepted operations applied, and setup remains open for correction/retry.

### Shared provider connection popover

The setup introduction lists the three task tiers as bullets with bold tier names. The cloud step explains provider support and fallback, and offers **Add cloud provider…** alongside already-connected accounts instead of maintaining a separate provider picker. Add opens the same searchable provider picker and account editor used by configuration, including its subscription OAuth actions, API-key entry, paste handling, and asynchronous save behavior.

ChatGPT (`openai-responses`) and Anthropic subscription (`anthropic-subscription`) connections use the shared OAuth sign-in actions, not the wizard's legacy API-key fallback. Returning from or cancelling the provider editor returns to the cloud step and refreshes connected accounts; it does not accept routing or advance setup automatically. Completed account connections retain the existing cancellation contract.

Regression coverage includes subscription catalog aliases, shared-picker cancellation, OAuth action dispatch without an API-key field for subscription accounts, refreshed accounts on return, and rendered tier bullets. Automated checks do not constitute a live browser/OAuth login test.

### Compact setup review

Setup headings are bold and task-tier bullets are indented. The final screen is titled **Review your setup**, not “done”: no settings have been applied yet. Its overview shows the selected template, cloud-use boundary, account and task models, grouped local model assignments, and download/readiness information. Shared local artifacts are shown once even if different slots use their ID and display-name aliases.

**View details** exposes the complete assignment/change preview and technical diagnostics. **Customize local models** and **Customize cloud account** open the existing editing paths; **Use this setup** is the explicit apply action. Browsing templates/details and opening customization do not write server configuration. Runtime validation remains deferred and is not implied by the compact presentation.

### Automatic cloud image understanding

Setup does not need to pin a separate image model. An empty image choice means
**Automatic**: use the account's configured chat-quality model when its image-input
capability is confirmed, otherwise try that provider's image/default tier
recommendations with the same capability check. Each backup resolves independently
using its own account/endpoint evidence. Unknown capability never authorizes an
image request; transport support alone is insufficient.

An explicit image override remains authoritative and is not silently replaced.
Clear it with **Automatic image model** in Cloud → Model tiers to restore
inheritance. These choices are computed at request time, not saved as setup-time
model pins. If no confirmed candidate is available, select an image-capable model
there (or configure local vision for local-only use). This policy handles image
understanding, not image generation, and does not change local-only privacy rules.

## Rendering and account defaults

Subscription sign-in and API-key-switch actions explain the authentication mode;
the provider editor does not repeat a separate Authentication / Subscription
sign-in row when those actions are present. Capability bullets use bold,
accent-colored labels with hanging descriptions. Setup choices and actions use
aligned name/description columns, with stacked rows on narrow terminals.

ChatGPT sign-in does not choose one catalog model for every quality tier. Unset
Economy, Standard, and Premium slots inherit the configured per-tier defaults.
Reauthentication preserves explicit overrides. An account affected by older
all-SOL defaulting can be corrected by choosing **Use inherited recommendation**
in each of its three Model tiers menus. This clears those explicit quality
overrides without reauthenticating or changing image choices or credentials.

Rerunning `/setup` returns to the existing chat without replaying the opening
animation above its existing banner. Startup setup still reveals one welcome
when it closes.
