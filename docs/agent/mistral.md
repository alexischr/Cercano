# Mistral cloud accounts

Mistral is available in **Cloud Providers** using API-key authentication and
`https://api.mistral.ai/v1`. It uses the existing chat-completions adapter and
cloud-profile secrets store. This cloud provider is separate from the local
`mistral.rs` runtime.

## Setup and model selection

1. Select Mistral in Cloud Providers and create a named account.
2. Enter its API key in the masked key field and save the account. Do not paste
   credentials into chat. Save the account/key before choosing model overrides.
3. Choose models within that account's provider settings and save. The list is
   fetched from the authenticated `/models` endpoint. Newly selected models are
   checked against that account again before saving; image models require
   affirmative vision evidence. Session model overrides also recheck access.
4. Add another Mistral account if needed. Selection never chooses another account
   merely because it can access the requested model.

Mistral discovery does not publish token prices. The UI displays **pricing
unknown**, not zero, and no price-based quality recommendations are invented.
Choose explicit economy/standard/premium overrides for the qualities you use.

## Shared catalog, separate account evidence

**Show Mistral catalog across accounts** opens a read-only view within Mistral's
provider settings. It shows each model once, with account-specific context and
capability information underneath. This explicit combined view can include
private/fine-tuned model names owned by any configured Mistral account. A failure
for one account is shown rather than presenting a partial refresh as complete.

The model dropdown used to configure an account remains filtered to that
account. Different context or vision capabilities reported for the same ID are
not merged into one universal answer. Runtime metadata and delegated-worker
snapshots retain account identity. The Local Models browser is unchanged;
Mistral cloud models are not downloadable entries.

IDs, names, aliases, context limits, capability presence, and retirement dates
are preserved where supplied by the API. Unknown capabilities remain unknown.
Retired models and models without affirmative chat/tool capabilities are not
new agent-model choices. Existing pinned IDs are retained rather than silently
replaced; new selections fail with a diagnostic when access cannot be verified.

Successful discovery is cached for 15 minutes per account configuration; failed
refreshes briefly back off and remove that account's available observations.
Changing a key or endpoint, removing an account, or recreating its name invalidates
its observations. An old in-flight response cannot restore obsolete access.

## Verification status

The provider remains **untested** in the provider catalog: local fixtures do not
prove live API compatibility. Automated coverage includes authenticated discovery,
account isolation, sparse metadata and lifecycle fields, redirects and response
limits, credential changes, concurrency, client/server metadata mapping, worker
account identity, and streamed text/tool-result continuation through the factory.

A user-authorized live smoke test on 2026-10-16 passed authenticated discovery
through the production parser (45 models). Streamed chat through
`BuildCloudProvider` and the Mistral adapter returned HTTP 429, provider code 1300
(`Rate limit exceeded`), classified by the adapter as `busy`. The test stopped;
live chat completion, tool continuation, vision and reasoning remain unverified.
The provider's verification label remains unchanged.

The opt-in `TestMistralLiveAdapterSmoke` test requires both
`CERCANO_MISTRAL_LIVE_SMOKE=1` and `MISTRAL_API_KEY` in its environment. It makes
one discovery request and at most three bounded inference requests, stopping on
the first failure. Normal test runs skip it. It does not persist the credential. Documented structured text and unsigned thinking chunks are normalized by a
Mistral-only adapter. **Signed reasoning and unsupported content chunk types are
rejected explicitly**; signed reasoning replay is not implemented or claimed.
The local fixtures do not establish compatibility with every Magistral model.
