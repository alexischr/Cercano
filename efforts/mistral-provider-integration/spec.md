# First-class Mistral provider with full metadata

## Problem and motivation

Cercano supports named cloud profiles, securely stored API keys, OpenAI-compatible inference, and a unified model catalog. DeepInfra additionally supplies live catalog metadata used for model browsing, context budgeting, and vision decisions. Mistral should have an equally integrated experience rather than being merely a custom endpoint.

Mistral's authenticated `GET https://api.mistral.ai/v1/models` lists models available to the requesting account, including fine-tuned models. Unlike DeepInfra's public index, its results cannot safely be treated as a provider-wide catalog. Published model cards expose identifiers, context limits, capabilities, aliases, and lifecycle information, but the inspected schema does not supply token prices.

## Goals

A user can add a Mistral cloud profile through the normal onboarding/configuration interface, save its API key in the existing secrets store, browse the models available to that profile, select an exact model ID, and use streaming chat and tool calls through Cercano's existing provider architecture.

Full metadata integration includes profile model selection, shared catalog browsing and model detail, context-window evidence, and vision capability evidence. Discovery must preserve the originating account/profile through selection and inference. Both main-chat and delegated routing must use evidence for the actual selected profile and model, not whichever account was most recently browsed.

Model details preserve and expose upstream identifiers, display names, context limits, chat/function-calling/vision/reasoning capabilities, aliases, and deprecation/replacement information where supplied. Missing values remain unknown. Reasoning capability metadata alone does not enable an untested inference parameter or imply support for exposing private model reasoning.

Agent-model selection requires affirmative chat and function-calling support. Non-agent models must not be offered as usable agent choices. Scheduled retirement is distinguished from already-retired models. A pinned retired or inaccessible model produces a useful explanation; aliases and replacements never silently rewrite the user's selected model ID.

Prices absent from the upstream catalog are displayed as unknown, not free. Pricing-dependent ranking, cost reporting, or automatic tier assignment must not interpret missing Mistral prices as zero cost or manufacture prices or quality levels. Explicit user tier assignments remain available.

## Account isolation and credential lifecycle

Keys are saved only through the existing profile/secrets-store flow. They must not appear in repository files, model identities, catalog references, logs, error messages, or client-facing metadata. No new credential-storage mechanism is required.

Catalog contents, model availability, and capability evidence must be scoped to the relevant profile and endpoint. Two accounts at the same endpoint may expose the same model ID with different metadata; they must not share cached results by model ID or provider alone. Fine-tuned model names must not leak into another account's catalog results.

Replacing or removing a key, changing an endpoint, or deleting a profile invalidates its cached discovery and evidence. Late responses from an old credential configuration must not repopulate current caches. Switching the active profile must not rebind a catalog selection to a different account.

Discovery uses bounded network requests and bounded responses. Credentials must not follow redirects, and catalog requests must use the same explicit profile endpoint/credential ownership rules as inference. Official Mistral recognition uses parsed, exact host matching rather than substrings. Errors are sanitized and distinguish authentication failure, discovery failure, and a successful empty catalog. An authentication failure must not silently return another profile's results or a stale list as current authenticated availability.

Concurrent browsing and capability requests should reuse bounded discovery/cache work rather than fetching the entire catalog on every turn. Cancellation and timeout are honored. Cached data is not a substitute for successful inference authorization.

## Inference and compatibility

Reuse the existing chat-completions provider, factory, profile configuration, and secrets infrastructure. Add Mistral-specific protocol behavior only where documented fixtures or a focused compatibility probe demonstrate it is needed. Verify streamed text, streamed tool-call assembly, tool-result continuation, supported message shapes, usage/error handling, and reasoning-related response shapes relevant to supported models.

Do not silently rewrite tool-call IDs or replay reasoning content on assumptions about compatibility. Any necessary adaptation must preserve call/result correspondence and be covered by a focused regression test.

Existing DeepInfra, Cerebras, Anthropic, OpenAI-compatible/custom endpoint, and local/downloadable catalog behavior must remain compatible. Account-aware catalog/evidence changes must preserve existing callers' behavior when profile information is absent, without permitting ambiguous Mistral lookups to select an arbitrary account. Hosted models remain non-downloadable.

Changes to client/server metadata transport must preserve safe defaults: older or incomplete metadata cannot grant vision support, invent context capacity, or imply a known price. Unknown vision does not authorize sending images. Unknown context uses the existing conservative fallback policy rather than another account's metadata.

## Verification and completion criteria

Automated verification requires no real API key. Synthetic fixtures based on the published API schema and local HTTP test servers cover catalog parsing, optional and unknown fields, eligible/ineligible models, fine-tuned models, aliases, retirement dates, replacement hints, context limits, capability states, and absent pricing.

Security and lifecycle tests cover two profiles sharing an endpoint and model ID with different results, credential rotation/removal, profile deletion, endpoint changes, in-flight invalidation, redirect refusal, sanitized errors, cancellation, response bounds, and authentication failure without cross-account or stale-success fallback.

Integration tests cover discovery through the agent API into the CLI, profile-preserving catalog selection, error display, context/vision evidence for main-chat and delegated execution, and streaming/tool-call round trips through the provider factory. Existing affected providers receive focused regression coverage. If shared transport contracts change, generated artifacts and both client/server integration tests are included.

Live verification is a separate, explicitly reported gate. It requires a user-saved Mistral profile key and approval for any paid inference requests. It checks authenticated catalog access, streamed chat, and a tool-call continuation; vision is checked with a supported model when available and authorized. No key is needed to implement or run automated tests. Missing live credentials must be reported as an unverified live gate, never as a successful compatibility test. Provider verification labels must reflect the evidence actually obtained.

Documentation explains profile setup, metadata availability, account isolation, unknown pricing, and verification limitations. Implementation is committed locally; publishing is outside this effort unless requested.

## Non-goals

This effort does not add Mistral OCR, speech/audio, embeddings, fine-tuning management, Agents APIs, or a generic native inference tool. Listing an account's already-existing fine-tuned chat models is in scope; creating or managing them is not.

No new third-party SDK, external pricing scrape, benchmark-derived model quality classification, default global routing change, or automatic migration of existing custom profiles is required. It does not redesign unrelated providers or obtain/store a user's API key through chat.

## Decisions

### Full metadata integration — approved

The user chose full metadata integration over a provider preset and model-picker-only launch ("full metadata, fersure"). This includes shared browsing and capability consumption, not just an authenticated list of model names.

| Axis | Profile/model-picker only | Full metadata integration — chosen |
|---|---|---|
| Complexity | Reuses the narrower Cerebras-style discovery path | Also integrates account-aware browsing, metadata, and evidence |
| Risk | Leaves useful metadata disconnected from runtime decisions | Requires account-isolation and lifecycle regression tests |
| Outcome | Provider setup and model selection | Setup, selection, browsing, context, and vision integration |
| Side effects | Defers richer catalog work | Establishes support for an authenticated model catalog |
| Dependencies | Existing HTTP/provider infrastructure | Existing HTTP/provider infrastructure |
| Pricing limitation | No upstream prices | No upstream prices; unknown values must remain explicit |

### Implementation without credentials; live checks separately — agreed

Development and automated verification use fixtures and mock servers. The user need not save a key now. Live verification uses a key saved through Cercano's existing profile setup and remains separately reported until performed. Paid inference is not implicit permission to spend unbounded quota.

### One Mistral catalog with per-account access — approved

The user proposed and approved one master Mistral catalog, with each model recording which configured accounts can access it. The catalog is the union of those accounts' authenticated responses, not an assumed public inventory. A combined browse view shows a model once with its accessible accounts; an account-filtered view exposes only that account's models. Selection uses the account the user is selecting for and never silently switches accounts.

Account-specific metadata remains associated with the account underneath the combined view. When two accounts report different capabilities or context limits, runtime decisions use the selected account's evidence, not a provider-wide merge or the most permissive value. A combined view must label differing information rather than assert one account's values for everyone. Private model names can appear in the explicit combined view, with ownership shown, but never leak into another account's filtered view. Endpoint ownership remains part of the underlying observation identity; custom endpoints cannot overwrite official Mistral observations.

Changing or removing credentials removes only that account's access and observations. A model with no remaining current account observations disappears from the available catalog. Discovery errors remain distinguishable from successful empty results; partial failure must not be presented as a complete successful refresh.

| Axis | Explicit profile-scoped catalog entries | Separate source per account | Shared catalog with account access — chosen |
|---|---|---|---|
| Complexity | Carry account identity through catalog references and consumers | Manage dynamic source instances and account ownership | Aggregate account observations into one model list and carry access information through consumers |
| Risk | Dropping account identity can mix evidence | Stale source registration can retain old access | Collapsing differing observations can mix evidence; preserve account-specific values |
| Outcome | Correct account-bound entries | Correct account-bound catalogs | One model entry with visible access and account filtering |
| Side effects | Account identity shapes each catalog entry | Accounts multiply catalog sources | Aggregation and partial-failure handling are explicit; execution still uses a specific account |
| Dependencies | No new external dependency | No new external dependency | No new external dependency |

The earlier alternatives were not accepted as the desired catalog organization. The chosen approach preserves one provider catalog while separating model identity from account access. This is not permission to merge private metadata into a globally authoritative record.

### Requirements boundary

The spec and shared-catalog design are approved. Account isolation, safe unknown metadata, credential lifecycle handling, and compatibility are correctness constraints, not optional alternatives. A genuinely consequential new architecture choice discovered during implementation must be surfaced before it is adopted.

## Audited integration anchors

`source/server/internal/cloudcatalog/cloudcatalog.go` owns provider presets and grouping. `source/server/pkg/config/config.go` contains provider inference/default selection. `source/server/internal/cloudfactory/` owns provider construction and static-key requirements. `source/server/internal/server/cloud_models.go` exposes profile model discovery; `source/server/internal/server/server.go` contains profile/key lifecycle integration.

`source/server/internal/catalog/catalog.go` defines shared discovery and metadata. Its current model price fields do not distinguish unknown prices explicitly. `source/server/internal/deepinfracatalog/` is the rich public-catalog precedent. `source/server/internal/modelevidence/` consumes catalog evidence, and `source/server/internal/modelmetadata/modelmetadata.go` defines identities/snapshots; its current identity has provider, endpoint, route, and model, but no profile dimension.

`source/clients/cli/internal/ui/cloud_section.go` integrates profile model fetching. Shared catalog browsing, routing, metadata transport, and their callers require a focused trace when drafting the implementation phases. No further audit delegation is authorized by the user's earlier instruction.

Upstream references inspected during the preceding audit include the official Mistral Python SDK README and `docs/models/basemodelcard.md`, `docs/models/modelcapabilities.md`, and `docs/models/modellist.md` at `https://github.com/mistralai/client-python`. Published schema facts are not evidence of a successful live API request.
