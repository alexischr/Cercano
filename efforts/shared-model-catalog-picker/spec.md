# Shared model catalog picker

## Status and approved direction

Design in progress. The user approved a results-and-details popover rather than a compact dropdown with optional details. This document specifies that direction; implementation still requires plan approval.

## Problem

Cloud configuration currently turns full model metadata into long dropdown labels. Users cannot scan or compare results, and an empty list does not clearly distinguish missing credentials from discovery problems. Fetching the catalog can also block the terminal update loop.

The first delivery is a reusable catalog component with two real consumers: cloud account model selection and the provider's cross-account catalog browser. It is not a Mistral-specific widget. Runtime/download selection and conversation-model selection are future consumers, not migrations bundled into this effort.

## Use cases and acceptance criteria

| User intent | Required experience |
|---|---|
| Set up a key-backed account | Before a saved key exists, show “Add an API key to load available models.” Provide an Add API key action that focuses the existing masked credential field. Do not attempt discovery with unsaved credentials. |
| Save a newly entered key | After successful existing account Save, invalidate old discovery state and start loading for the visible account without requiring a settings reopen. A failed save must not imply that credentials were accepted. |
| Find a known model | Search receives initial focus. Search IDs, display names and aliases case-insensitively. Rank exact ID/name matches first, then prefix, then substring, with stable ordering. No network request per keystroke. |
| Compare unfamiliar models | Results are concise; the highlighted row drives a separate detail pane. Display full exact ID, capabilities, context, prices and lifecycle information without inflating result rows. |
| Choose an economy/standard/premium model | Caller supplies purpose, current override and inherited recommendation. Preserve explicit “Use inherited recommendation” as a non-model action; selection changes only the settings draft until Save. |
| Choose an image model | Keep “No image override” distinct from inheritance. Show capability requirements; incompatible/unconfirmed models cannot bypass existing server validation. |
| Browse a provider across accounts | Open the same component in browse mode. Keep account observations distinct, show account discovery failures and allow account filtering. Browsing does not change model choices. |
| Recover from failed discovery | Preserve existing configured IDs, show the real known state, offer Retry or credential recovery. Never turn a discovery failure into “no models exist.” |
| Cancel | Escape closes the picker, preserves the original draft value and restores focus to its launching control. Scrolling/highlighting is never a commit. |
| Use a small terminal or keyboard only | All actions remain accessible without mouse or color. Resize must not change selection or lose the query. |

## Grounding in current implementation

- `source/clients/cli/internal/ui/cloud_models.go`: `cloudModelMetadataLabel` serializes metadata into long labels; `modelOptionsFromCatalog` preserves unlisted current IDs.
- `cloud_section.go`: `cloudModelOptions` performs synchronous discovery with a 12-second timeout and a per-row cache. `draftHasKey` distinguishes draft edits from stored-key presence; the new discovery state must not equate an unsaved key draft with a saved usable credential.
- `cloud_routing.go`: tier/image fields use `form.Select` and separate custom-ID fields. Preserve inheritance and custom-ID semantics.
- `cloud_commit.go`: cloud choices and credentials remain drafts until explicit Save. Preserve that contract.
- `cloud_provider_catalog.go`: the Mistral combined catalog is an inline series of read-only fields with account observations. Replace this consumer with browse mode; do not invent cross-account catalog APIs for other providers.
- `source/clients/cli/internal/overlay/rowlist.go`: existing overlay conventions, theme and focus behavior are useful, but RowList is a simple key/value editor, not a ready-made searchable catalog. Do not claim that it already provides this UI.
- Existing Bubble Tea command/message patterns provide asynchronous loading; reuse the terminal framework and theme, with no new UI dependency.

## Visual and interaction design

### Closed control

Use a compact model-selection launcher in the form: field label, concise selected display name (or inherited state), and an affordance to open the picker. Keep the exact model ID in details rather than appending all metadata to the form value. Show meaningful discovery status near the controls even before the popover opens.

### Wide popover

Illustrative layout, not live model metadata:

```text
╭ Choose a model · Standard quality ─────────────────────────────╮
│ Mistral / Work account                                         │
│ Search models…                                                │
│ Filter: Agent-compatible                 24 of 40 models        │
│                                                               │
│ MODEL                         │ DETAILS                       │
│   Use inherited recommendation│ Mistral Large 4                │
│ › Mistral Large 4              │ mistral-large-4                │
│ ● Current model               │                                │
│   Another model               │ Capabilities                   │
│                               │ Context                        │
│                               │ Input / output pricing         │
│                               │ Availability and lifecycle     │
│                                                               │
│ ↑↓ Navigate  Tab Focus  Enter Select  Ctrl+R Refresh  Esc Cancel │
╰───────────────────────────────────────────────────────────────╯
```

Use existing theme tokens: strong title, one accent cursor treatment, muted secondary text, restrained warnings. Current selection and cursor are different states. Use text labels as well as color. No custom hardcoded color palette or animations beyond the established loading indicator.

Rows: one line for name plus a small current/unavailable marker; a second muted line only when necessary to disambiguate identical names by exact ID/publisher. Long IDs truncate in the list but remain fully readable/wrapped in details. Avoid decorative capability badges that make the list a second detail pane.

Details: display name; exact ID; provider/account; Chat/Tools/Vision/Reasoning as supported, unsupported or unknown; context length or Unknown; separate input/output prices with units or Unknown; aliases; deprecation/retirement/replacement; access freshness/status. Missing data must not become a false capability or a zero price.

### Responsive behavior

Use available terminal dimensions, not absolute positioning assumptions. Initial breakpoints to verify with fixtures: at least 100 columns and 24 rows uses two panes; smaller windows use a stacked list/detail presentation. At approximately 60x18 use one pane with a visible Details/Back action, retaining query and cursor across the switch. Very small windows show a readable resize hint and still allow Escape. No content should exceed viewport width; detail content scrolls rather than expanding outside the modal.

Verify representative 120x36, 100x24, 80x24 and 60x18 windows and resize transitions. Adjust the numeric breakpoints based on real rendering without changing the approved interaction model.

### Keyboard and focus

- Search initially focused, with current selection highlighted when visible. Typing ordinary characters—including j/k/q/r—edits the query, never triggers list shortcuts.
- Up/Down navigate results even while search has focus; Enter selects the highlighted eligible item (or activates the highlighted state action).
- Tab/Shift+Tab move through search, filters, result list, details/actions. Only the active region consumes scroll keys.
- PageUp/PageDown scroll the active list/detail region. Home/End respect text editing while search is focused.
- Ctrl+F returns to search; Ctrl+R explicitly refreshes discovery, except when a refresh is already pending.
- Escape returns from a nested compact detail view first, then cancels the popover; it does not clear or commit configuration silently.
- Browse mode has no implicit Select action. Enter can open details but cannot mutate configuration.
- Mouse support follows existing terminal conventions if available, but is not required for any action.

## Discovery states and copy

| State | Message and action |
|---|---|
| Key required, none saved | “Add an API key to load available models.” / Add API key |
| New/edited credentials not saved | “Save this account to load its available models.” / Return to account settings; preserve all drafts |
| Subscription authentication required | “Sign in to load available models.” / Existing sign-in action, not an API-key prompt |
| Loading | “Loading available models…” with indicator; typing, Escape and repaint remain responsive |
| Refreshing an unchanged identity | Keep prior results visible with Refreshing label; do not fabricate fresh access confirmation |
| Credential rejection, when reliably known | “The provider rejected this account’s credentials.” / Edit credentials |
| Rate limit, when reliably known | “The provider is limiting catalog requests. Try again later.” / Retry; honor known retry delay |
| Generic discovery/network failure | “Couldn’t load available models.” with sanitized provider detail / Retry |
| Successful empty catalog | “No models were returned for this account.” / Refresh and account context |
| Query/filter has no matches | “No models match your search.” / Clear search/filters, not credential editing |
| Partial provider-wide failure | Keep successful observations; show failed account statuses and Retry |
| Current ID absent from results | Preserve it with “Not in current catalog; access unconfirmed.” Never silently reset it |

Do not infer an authentication or rate-limit category from arbitrary error-string matching. Existing transport errors may support only a generic failure. Use the best structured evidence available; a generic honest failure is preferable to false precision. Do not expand server transport solely to invent classifications in this UI effort.

Discovery is permitted only for the saved account identity and appropriate auth mechanism. Public/unauthenticated catalogs and custom endpoints must not be incorrectly gated behind an API key. Use existing provider/auth semantics, not a global HasKey requirement.

## Shared component and ownership

Introduce a sibling catalog component in the existing overlay area, with a typed entry/view model rather than parsing presentation labels. The host adapts agentclient metadata to that view model. Component inputs include purpose/mode, scope, current value, requirements, entries, discovery state and explicit special actions. Outcomes are typed selection, cancel, refresh, credential-focus and scope-filter requests.

The component owns search, local filters, navigation, layout and highlighted details. It does not own API clients, secrets, global configuration or RPC mutation. Host code owns asynchronous discovery and settings drafts. Reuse existing overlay composition and focus restoration; do not overload RowList's key/value editing with model semantics.

Identity for selection includes exact model ID and caller's account scope. Provider-wide rows aggregate only identity/display, while details preserve per-account observations; do not flatten conflicting metadata. The provider browse mode is read-only. An account-scoped selection cannot silently switch to another account.

Use request generations tied to the saved profile/provider/endpoint/auth revision. Discard late replies after scope/identity changes, save, deletion or close. A refresh must not overwrite a newer query/cursor unnecessarily. Never use a raw credential as a cache key or log field. Bound requests, coalesce duplicate opens/refreshes, and cancel obsolete work where supported. Do not auto-retry indefinitely.

## Selection semantics and compatibility

- Highlighting does not change drafts. Enter returns an explicit selection; the host updates only the relevant draft field and closes the popup. Existing explicit Save persists it.
- Distinguish select-model, inherit and no-image-override outcomes instead of overloading an empty model ID inside the shared component.
- Preserve explicit custom model-ID entry for endpoints with incomplete/no catalog support. Keep it separate and clearly marked as unverified; retain server validation, especially Mistral availability and confirmed image capability.
- Existing configured IDs remain visible and untouched on errors. Their display does not assert renewed access.
- Caller requirements default to compatible results. An optional All models filter allows inspection of incompatible entries with disabled selection and a reason. Known unsupported and unknown capability states remain distinct; caller policy determines eligibility, with server validation authoritative.
- Existing curated fallback catalogs must be labeled as static/unverified, never portrayed as live authenticated availability.
- No paid model inference is necessary to render, browse, search or verify this UI.

## Scope and non-goals

In scope: shared component, asynchronous catalog state for cloud configuration, credential guidance, tier/image launchers, provider catalog browse consumer, focus/resize behavior, focused tests, visual validation and documentation.

Not in scope: changes to recommendation algorithms, billing/routing, credential persistence policy, provider discovery semantics, attachment/OCR issue #60, runtime download/acquisition workflow redesign, migration of every model selection surface, full mouse feature expansion, or new dependencies. Runtime and conversation pickers should be able to reuse the component later without those migrations being claimed complete now.

## Verification and completion

Unit tests cover search ordering, exact-ID preservation, capability/unknown states, account separation, special outcomes, navigation, cancel and responsive rendering. State tests cover key absence versus unsaved keys, subscription auth, empty success versus failure, no matches, stale replies, rapid account switching, save-triggered refresh and duplicate requests. Integration tests exercise both selection and browse consumers with deterministic fake-agent responses and no paid requests.

Review rendered examples in available theme variants at the specified viewport sizes, including long identifiers, Unicode labels, no metadata, partial failures and all empty states. Validate a representative interaction in the actual terminal where feasible; report the difference between render-fixture coverage and manual visual testing. Run the affected CLI form/overlay/UI suites and build the CLI. Server tests are required only if an interface changes; do not change the interface without documenting why.

Deliver code through a feature worktree, checkpoint solved units, and report verification/limitations. Do not push, land, restart the agent, install binaries or make paid API calls without separate authorization.
