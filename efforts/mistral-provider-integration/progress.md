# Mistral integration verification

## Delivery

Implementation is on `feat/mistral-provider-integration` in
`/Users/bryancostanich/git_repos/cercano-ai/Cercano-mistral-provider`.
No merge or push was performed. Work was done directly after the user prohibited
further dispatch. No credentials or paid inference were used.

The user's UI clarification is implemented: model selection stays within the
selected provider/account. An explicit read-only combined catalog lives inside
Mistral's existing Cloud Providers settings, not a separate model browser.
Local Models remains unchanged. The master catalog merges access, not conflicting
account metadata. The credential-generation boundary is represented internally by
replacement account entries; obsolete responses cannot publish into a new entry.

## Automated gates (passed, 2026-10-16)

Server:

```
go test ./internal/catalog ./internal/mistralcatalog ./internal/cloudcatalog ./internal/cloudfactory ./internal/modelevidence ./internal/modelmetadata ./internal/server ./internal/worker ./internal/llm/openai ./internal/inference/profilechain ./pkg/agentclient ./pkg/config -count=1
go test -race ./internal/mistralcatalog ./internal/cloudfactory ./internal/llm/openai -count=1
go test -race ./internal/server -run 'TestMistral|TestSessionModel' -count=1
make build VERSION=mistral-fixture
```

CLI:

```
go test ./internal/ui ./internal/form -count=1
make build VERSION=mistral-fixture
```

Bindings regenerated with `make proto`. Both builds succeeded through the
repository build/signing workflow. No running agent was restarted or replaced.

## Coverage and review

- Authenticated catalog discovery, response bounds, redirects, malformed data,
  absent metadata, fine-tuned IDs, aliases and retirement boundaries.
- Shared union and account-filtered views, conflicting context/vision evidence,
  private model visibility and explicit partial-discovery errors.
- Key rotation, endpoint changes, account deletion/recreation, coalesced refreshes,
  cancellation and obsolete in-flight responses (race-enabled).
- Exact model IDs through provider-specific UI and RPC mappings; optional zero
  prices remain distinguishable from unknown. No Mistral price table or automatic
  tier recommendation is invented.
- Saved tier/image overrides and session overrides recheck account access;
  unchanged pinned IDs remain stored, and missing catalog entries are labeled.
- Worker evidence serialization includes profile identity; two profiles with the
  same endpoint/model retain different evidence.
- Integrated two-account server fixture browses the union, filters accounts,
  resolves evidence, executes a tool round trip, rotates a key and checks stale
  access/evidence removal.
- Factory fixtures cover streaming split tool calls, result correlation, text,
  usage, HTTP errors and documented structured text/unsigned thinking content.
- Existing server/configuration/factory/OpenAI-compatible/UI suites cover other
  provider and downloadable-model regressions.

Direct probes found and fixed: exact retirement boundary handling; unknown prices
collapsed to zero in transport; absent retirement dates and capability-only
metadata; stale CLI account model caches; worker account identity dropped during
serialization; explicit `direct` route recognition; and structured content arrays
rejected by the OpenAI-compatible SDK. Regression tests cover these cases.

## Remaining limitations / live gate

- Provider remains **untested**: catalog/chat/tool/vision compatibility has not
  been exercised against a real Mistral account.
- Signed reasoning replay is not implemented. Signed reasoning or unsupported
  output chunk types fail explicitly, rather than silently losing signatures or
  inventing replay behavior. Structured text and unsigned thinking are supported
  by fixture-tested conversion; this does not establish every Magistral model's
  compatibility.
- Initial setup saves the account/key before selecting model overrides. Mistral
  has no invented price-based default model recommendations.
- Discovery uses a per-account TTL; saves invalidate that account, and new model
  selections bypass stale catalog freshness for verification. No new global
  Local Models refresh behavior was added after the provider-specific UI
  clarification.
- No manual terminal visual inspection or production/live end-to-end run is
  claimed. Verification used local HTTP/RPC fixtures and UI/form tests.

User documentation: `docs/agent/mistral.md` (linked from the agent README).
Implementation checkpoints include `3072a980bd89`, `8313f559ffaa`, `49522a47fc79`,
`692617819dcd`, `fa62b87da5e2`, and `a5e013759379`, following the earlier metadata
and catalog commits.
