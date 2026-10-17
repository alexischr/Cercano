# Setup recommendation evidence

## Model identity and artifact check — 2026-10-15

The official Hugging Face API lists `Qwen/Qwen3.8-27B`; the name from the conversation is confirmed, not a transcription error.

Sources inspected:
- https://huggingface.co/api/models?author=Qwen&search=27B&limit=30
- https://huggingface.co/Qwen/Qwen3.8-27B/raw/main/config.json
- https://huggingface.co/api/models/unsloth/Qwen3.8-27B-GGUF/tree/main?recursive=false&expand=false
- https://huggingface.co/api/models/unsloth/Qwen3.8-27B-GGUF?expand=gguf

The official config specifies 64 layers, every fourth layer full attention (16 full-attention layers), four key/value heads, head dimension 256, and maximum position embeddings 262144. Other layers use linear attention. GGUF metadata reports architecture `qwen35` and context length 262144.

Available third-party Unsloth artifacts include `Qwen3.8-27B-UD-Q6_K.gguf` (21,983,677,344 bytes) and `Qwen3.8-27B-Q8_0.gguf` (29,047,086,048 bytes). The Q6 file's API-reported SHA256 is c9c206812fbe4ac7b76a729e25928b63f2ae89d37f69da7a71c20aec763cd436. These are verified listings, not downloaded or tested artifacts. Dynamic quantization names do not establish uniform per-tensor precision.

Analytical cache component, assuming one sequence and 16-bit unquantized K and V: 16 full-attention layers × 4 KV heads × 256 dimensions × 2 (K and V) × 2 bytes = 65,536 bytes per token. Thus full-attention cache alone is 2 GiB at 32,768 tokens, 4 GiB at 65,536, 8 GiB at 131,072, and 16 GiB at 262,144. This excludes linear-attention state, runtime buffers, graph allocations, vision allocations, additional sequences, and operating-system/application headroom. It is not a fit guarantee or a measured allocation. No simulation or benchmark was run.

## Compatibility boundary

`source/server/internal/llamacompat/arch.go` implements the curated architecture allowlist. It contains qwen3 and qwen3moe but not qwen35. `Supported` rejects unknown architectures. Therefore this Qwen candidate currently fails the application's compatibility gate; this does not by itself establish whether the pinned underlying binary can load it. Do not bypass that gate or mark the candidate tested. Establish pinned binary support and runtime/tool-calling evidence before adding it to curated recommendations.

`source/server/internal/localruntime/llamaserver/catalog.json` already contains GLM-4.5-Air Q4_K_M, marked tested with plain_chat_ok true. The comments in catalog.go use GLM as a historical example of failed plain chat; those comments are not the current entry's capability value. Existing catalog status is not fresh runtime verification in this effort.

## Wizard trace (partial)

`source/clients/cli/internal/wizard/wizard.go` currently implements conditional locus-first navigation: StepLocus, StepCloud/StepOpen, StepDone. Adjacent navigation tests are in wizard_test.go. UI, persistence, cancellation, and account-preservation audit remains incomplete. An initial broad read-only delegation exhausted its budget without a usable completed report; it made no edits. No preservation claims are inferred from that unsuccessful audit.

## Follow-up runtime probe

`llama-server --version` on this machine returned version 9890 (74976e1ae), Darwin arm64, at /opt/homebrew/bin/llama-server. The corresponding upstream source https://raw.githubusercontent.com/ggml-org/llama.cpp/74976e1ae/src/llama-arch.cpp explicitly maps LLM_ARCH_QWEN35 to qwen35. Thus the application allowlist is stale relative to this installed binary's architecture registry. This is not yet evidence that the candidate artifact loads or its tools work. Do not infer support from Qwen3 catalog entries: qwen3 and qwen35 are distinct architecture identifiers.

A host resource probe reports 137438953472 bytes (128 GiB) RAM and approximately 1.2 TiB available disk. This host can potentially support bounded candidate validation but is not a physical 48/64/96 GB test machine. No downloads or runtime changes have been performed. A subsequent vm_stat/ps probe found an existing llama-server process (PID 14864) reporting 83,865,248 KiB RSS (about 80 GiB); the host also reported about 84.7 GiB wired memory and 13.9 GiB compressor occupancy. RSS, wired memory, and compressor figures must not be summed as independent model allocations. Nevertheless, there is no safely established headroom for another Qwen load with a large unquantized cache. Runtime validation needs an isolated host or a user-approved window to free the existing runtime's memory. Do not stop or restart the active runtime implicitly.

## Wizard audit and recommendation logic — 2026-10-16 (dispatch session)

### Phase 1 completion: wizard trace and characterization

The wizard trace is now complete. `source/clients/cli/internal/ui/wizard_page.go` implements the apply, cancel, resume, and reset separation: `applyConfig` pushes a finish patch first (locus mode plus, only for open-using modes, `OpenRuntime: llama_server`), then explicit cloud-choice updates, then sorted tier patches; the abandon trapdoor restores the pre-run cloud profile baseline via `rollbackBaseline` before clearing wizard state; a failed apply or finish leaves the persisted resume state intact for nondestructive re-entry. No path overwrites unrelated user settings, and applying a preview remains an explicit user action.

New characterization tests in `source/clients/cli/internal/ui/wizard_apply_test.go` pin these behaviors by reflection against `agentclient.ConfigUpdate` rather than by string matching:

- `TestWizardApplyPatchesPreservesExistingUserConfig` — patches touch only locus-mode, runtime, tier, and cloud-provider fields for cloud-only, mixed, and open-only modes; every other config field is left untouched.
- `TestWizardApplyPatchesOrderAndShape` — finish patch first, then cloud choices, then tier patches in sorted key order; no duplicate or malformed tier keys.
- `TestWizardCloudOnlyNoOpenRuntime` — cloud-only recommendations never set `OpenRuntime`, so no local runtime is restarted.
- `TestWizardFinishLeavesResumeStateOnFail` — a failing apply keeps `wp.state.Step` advanced and keeps the persisted resume file so re-entry resumes at the open step.

### Phase 1 completion: hardware probe inventory

- Darwin (`source/server/internal/sysram/sysram_darwin.go`): `Total()` reads `hw.memsize` (unified memory); `NonEvictable()` sums wired plus compressor pages from `vm_stat`.
- Linux (`source/server/internal/sysram/sysram_linux.go`): `Total()` reads `MemTotal`; `NonEvictable()` approximates committed memory as `MemTotal − MemAvailable` (conservative direction; returns ok=false on old kernels without MemAvailable).
- Other platforms (`sysram_other.go`): both probes report unknown, and callers render "unknown" instead of a fit verdict.

Gap recorded honestly: no code path probes discrete-GPU (VRAM) memory anywhere in the tree. The server's RAM-band profile selection (`llamaserver/catalog.go ProfileForRAMEntries`) uses system RAM only; on a Linux box with a discrete GPU that is system RAM, not VRAM. The new recommendation engine does not extrapolate — it defers model fit to the server and reports hardware-unknown conditions with an explicit note — but a discrete-GPU-aware fit verdict would require a new server-side capability and is a remaining Phase 5 concern, not something improvised here.

### Phase 2 implementation: recommendation engine

`source/clients/cli/internal/ui/wizard_recommend.go` implements the pure recommendation engine within the existing architecture:

- `recommendSetup(inputs wizardRecommendInputs) wizardRecommendation` is a pure function of state, shipped tier recommendations (`config.LoadTierRecommendations`), and the agent's runtime model catalog (`ListRuntimeModels`, which carries `SystemRamBytes` and `RecommendedOpenModels`). It performs no RPCs, config writes, downloads, or runtime restarts; `TestRecommendSetupPerformsNoWrites` pins that.
- Local model selection defers to the server's memory-band selection (`RecommendedOpenModels`), falling back to `config.PickFirst` over shipped open candidates. When no server band answer is available the recommendation is annotated with the honest note `"Local picks fall back to shipped defaults; memory fit not yet verified on this machine."` rather than presenting a fit verdict; `TestRecommendDefersToServerMemoryBand` and `TestRecommendUnknownHardwareNotesHonestly` pin this.
- Preference handling: local preference overrides a mixed default and vice versa; an unknown provider recommendation does not present a cloud route; cloud use in mixed presets is explicit (`CloudUsed`) and cloud-only sets no local tiers. `TestRecommendLocalPreferenceHonored`, `TestRecommendUnknownProvider`, `TestRecommendMixedDisclosesCloudUse`, `TestRecommendCloudOnlyOneAccount`, `TestRecommendLocalOnlyHasNoCloudRoutes` pin these.
- No usable resources yields explicit empty picks with a note, never a fabricated default (`TestRecommendNoUsableResources`).
- Already-downloaded artifacts are reused (`estimateIsLocal`), keeping model reuse visible and avoiding re-download recommendations (`TestRecommendReusesInstalledArtifacts`).
- Tier picks use the existing `llama_server.<tier>` / `<tier>.cloud` slot conventions consumed by `applyConfig` (`TestRecommendTierPicksUsesSlotConventions`).
- One bug found and fixed, disclosed here: `recommendSetup` previously could carry a stale `CloudProvider` into open-only recommendations (misrepresenting a cloud route that is not used); it now clears `CloudProvider` when the mode uses no cloud. Found by the local-only test, not by inspection alone.

`wizard_recommend_test.go` uses a purpose-built `testCatalog` fixture whose `DownloadState` correctly reflects which models are on disk; an earlier fixture bug (marking all models downloaded, then clearing) was corrected in this session.

Test results on this machine: `go test ./internal/ui/ ./internal/wizard/` passes; `go vet ./internal/ui/` passes; `go build ./...` (CLI module) passes. No server code was modified.

### User-authorized deferral — Qwen shipping default gated

The user explicitly authorized deferring memory-intensive runtime validation and continuing implementation with the Qwen shipping default gated. In line with that:

- No candidate validation was performed. Nothing was downloaded, no models were loaded, the existing llama-server runtime (PID 14864, ~80 GiB RSS) was not stopped or restarted, and no claim is made that the Qwen candidate loads or works.
- No Qwen catalog default was added: `llamacompat/arch.go` still rejects architecture `qwen35`, so adding it to curated recommendations would fail the compatibility gate; the stale-allowlist question remains open and no gate change was made.
- The only evidence-supported 128 GiB local candidate remains the existing catalog's GLM-4.5-Air Q4_K_M entry (marked tested in the shipped catalog, though not freshly re-verified in this effort).
- Smaller-machine defaults remain the existing catalog profiles (`ProfileForRAMEntries` picks the largest profile at or below machine RAM; smallest profile below the smallest tier is a catalog-data question, not changed here).

Remaining before the Qwen gate can be lifted: pinned-binary architecture verification (allowlist update decision), actual Qwen GGUF tensor precision and runtime/tool-calling validation on an isolated host or user-approved memory window, GLM memory recalculation, and hardware-specific safe context defaults. These belong to Phase 1 verification tasks left pending and Phase 5 acceptance, and are out of scope for this dispatch.

## Outstanding verification

Exact pinned binary architecture support; compatibility-gate update requirements; actual Qwen GGUF tensor precision and runtime/tool-calling behavior; GLM memory calculation; hardware-specific safe context defaults; smaller-machine candidates; discrete-GPU-aware fit (new server capability, not improvised here). Application code changed in this dispatch is limited to `wizard_recommend.go` and its tests plus wizard characterization tests in the CLI UI package.


## Guided re-entry preservation follow-up

Active-account reuse is implemented without reauthentication or cloud-choice writes (1cdec719251e). Wizard API-key and subscription sign-in now use existing create-only contracts to prevent replacement of credentials that cancellation cannot restore (bedfbc466d9c). Focused UI/wizard tests and CLI builds passed.

A fake-RPC probe found a separate destructive cancellation defect: starting with an empty baseline, then observing an account named `added-in-another-window`, `rollbackBaseline` sends RemoveCloudProfile for that unrelated account. The failing probe reported `cancellation deleted unrelated account(s): [added-in-another-window]`. No live configuration or credentials were touched. The reproducer is retained as `rollback-repro.go.txt`; copy it to the UI test package to run TestWizardRollbackProbe. It is not enabled in the normal suite while the cancellation contract is awaiting a decision.

Current rollback infers ownership solely from absence in the baseline and restores all captured account metadata. Safe cancellation needs either persisted ownership/conflict-aware cleanup (including interrupted/legacy runs), or an explicitly disclosed contract that account connections persist while unconfirmed routing selections are discarded. This changes destructive/user-data behavior and requires approval before implementation. Merely adding name checks or snapshot comparisons does not establish ownership.


## Approved cancellation contract implemented

User approved keeping completed account connections on cancellation. Removed live baseline capture/rollback and all cancellation account writes. Legacy baseline fields are tolerated but not acted upon. Before authentication, in preview, and on the first q, users are told connected accounts persist and can be managed in /config. Cancellation clears only local resume state; unconfirmed selections are discarded. Regression tests exercise actual double-q cancellation with fake RPC and legacy baseline/current metadata differences. Clear failure keeps the wizard open for retry. Test HOME is isolated. The production build caught a delegated test-only clear hook; it was replaced with an instance-scoped optional seam defaulting to wizard.Clear.

Verification: go test ./internal/ui ./internal/wizard ./internal/slash -count=1 and go build ./... in the CLI module passed. Earlier rollback-repro.go.txt is historical evidence; the removed rollbackBaseline function is no longer callable.


## Preview/apply integration follow-up

Actual wizard autofill now consumes the pure recommendation helper, fills empty slots only, and retains explicit current local overrides using the existing llama_server.<tier> taxonomy. Recommendations from the runtime catalog work even when shipped recommendation data is unavailable. A configured-choice regression initially failed because the delegated implementation tested a suffix rather than the actual key format; the corrected implementation enumerates known open tier keys. Existing custom picks are no longer normalized or replaced merely because they are absent from recommendations.

A failing cloud-only regression demonstrated that stale hidden local picks were applied. wizardApplyPatches now emits only the mode patch for cloud-only, leaving those local settings untouched. The catalog fixture now explicitly selects open-only mode, matching the resource-filtering contract.

Verification: CLI go test ./internal/ui ./internal/wizard ./internal/slash -count=1 and go build ./... passed. No runtime candidate validation or live model downloads were performed. This is a bounded integration slice, not completion of the remaining plan or full preview/apply acceptance coverage.


## Direct implementation follow-up: preview and documentation

Preview download disclosures now resolve actual selected tier picks against the catalog, deduplicate by artifact ID, skip downloaded artifacts, and flag unresolved selections. Unknown RAM is disclosed independently of whether downloads are required. Removed claims that downloaded models are immediately ready or that an unconfigured cloud account can serve while downloads run. Preview snapshot tests serialize state before/after rendering to detect mutation rather than comparing maps or slice-containing structs illegally. Regression checks distinguish repeated tier assignments from duplicate download entries.

Updated setup documentation with tier meanings, preset families, cloud-use implications, custom-choice preservation, explicit apply/download timing, context safety, gated Qwen validation, and nondestructive guided re-entry. Verified slash/setup.go uses the documented Run setup again help text. Pending plan items have not been blanket-marked complete.

Verification in this follow-up: CLI UI/wizard/slash tests and production build pass. Server tests passed for pkg/setupstate, internal/managedrouting, internal/localruntime/llamaserver, and internal/llamacompat. These are package tests, not candidate runtime-load validation. No live runtime operations, downloads, or secrets access were performed.


## Apply/resume verification and hardware interface gap

Fake-server regression tests now exercise apply across all four locus modes. A failing probe showed unrelated/retired resumed tier keys were sent to UpdateConfig; apply now permits only current wizard-owned local slots. Another failing probe showed conflicting cloud Economy picks caused a locus write before validation; validation now occurs before the first write. A real-constructor resume probe showed an empty saved local slot received a shipped default before current configuration was discovered; discovery now precedes resumed autofill, preserving the explicit configured assignment. CLI UI/wizard/slash tests and production build pass after these changes (fe5c42ac6ad8 and 4e75fd9504a4).

Remaining interface gap: agentclient.RuntimeModelCatalog (client.go) exposes SystemRAMBytes and RecommendedOpenModels, but no unified-memory/discrete-GPU topology or GPU memory. ListRuntimeModels maps only those fields from the protocol. llamaserver.RecommendedOpenModels accepts only totalBytes. Therefore equal-system-RAM machines with different GPU memory topology cannot be distinguished by these recommendation inputs. Existing runtime allocation checks remain intact, but are not evidence for a topology-aware setup recommendation. Completing the approved topology-aware memory-band requirement needs either an additive server/catalog capability interface or an explicit narrowing to provisional, topology-unknown suggestions. Qwen runtime validation remains separately deferred; this interface gap is not resolved by that deferral.


## Memory topology interface implemented (user-authorized extension) — 2026-10-17

The user approved extending the existing server/catalog/protocol/client path rather than deferring topology-aware recommendations. The interface gap above is closed additively; all pre-existing fields, explicit picks, context/runtime safety checks, and RAM-band behavior are unchanged, and every new field is additive and backward-compatible.

### Server-side probe — `source/server/internal/sysram`

- `topology.go` defines the three-valued model: `TopologyUnified`, `TopologyDiscrete`, `TopologyUnknown` (never "" and never treating total system RAM as GPU RAM), plus `GPUDevice`/`MemoryTopology` structs. `TopologyForLinux` classifies from sysfs-integrated/discrete signals without ever synthesizing a unified pool from discrete devices, and `parseNvidiaSmiGPUs` parses `nvidia-smi` output per device (name may contain commas; unparsable memory yields a device with size 0 — known device, unknown pool — rather than a fabricated size).
- `topology_darwin.go`: Apple Silicon reports unified with `UnifiedMemoryBytes = hw.memsize`; Intel Macs report discrete with an empty GPU list; any probe failure reports unknown.
- `topology_linux.go`: probes nvidia-smi with a timeout; discrete devices are reported individually with their own memory; failure or no discrete GPUs on a non-integrated-probe yields unknown or unified respectively per the classification function. `topology_other.go` reports unknown on other platforms.
- Tests (`topology_test.go`, `topology_darwin_test.go`) are deterministic — no live nvidia-smi or hardware dependency; parse cases cover real-world output, comma-bearing names, unparsable memory, and garbage. A live-probe consistency test on the dev host confirmed the Darwin unified path matches `sysram.Total()`; it passes in the suite run on this machine (unified-memory host) without asserting any committed hardware figure.

### Protocol — `source/proto/agent.proto` (regenerated via `source/proto/generate.sh`, protoc 34.1 / protoc-gen-go v1.36.11 / grpc v1.6.2)

`ListRuntimeModelsResponse` gains additive fields: `memory_topology` ("unified"/"discrete"/"unknown"), `unified_memory_bytes`, repeated `gpu` entries (`name`, `memory_bytes`), and `hardware_ok` (server-side probes succeeded). Old servers omitting these fields are indistinguishable from unknown, which is exactly the semantics clients apply.

### Server — `source/server/internal/server/server.go` `ListRuntimeModels`

Populates the new response fields from `sysram.Topology()`; sets `HardwareOk` only when the RAM and topology probes actually succeeded. The server's RAM-band picks (`recommendedOpenModels` via `catalogdefaults.ForRuntime`) are unchanged — the server does not fabricate topology-aware bands it cannot support; it reports the facts and lets the client disclose honestly.

### Client — `source/server/pkg/agentclient/client.go`

- `RuntimeModelCatalog` gains `Hardware HardwareTopology` and `HardwareOK bool`; `HardwareTopology{Kind, UnifiedMemoryBytes, GPUs []HardwareGPU}` with `HardwareTopologyUnified/Discrete/Unknown` constants and `Insufficient()` (empty or unknown kind).
- `mapHardwareTopology` maps the proto response; a response with no topology fields (old server) maps to unknown with `HardwareOK` false — no invented unified or discrete claim. `runtime_hardware_test.go` pins unified, discrete, unknown, and old-server-default mappings.

### Recommendation logic — `source/clients/cli/internal/ui/wizard_recommend.go`

- `hardwareNotes(cat)`: unknown RAM keeps the existing smallest-profile note; `!HardwareOK || Insufficient()` (including old servers) yields "memory topology unknown: hardware fit not verified for this machine". Probed unified machines get an explicit deferral note ("unified memory: preliminary weight-budget screen only — context/KV-cache headroom is not checked and runtime fit is not verified; runtime checks still apply") — the RAM-band picks are never presented as runtime-verified. Discrete machines get the preliminary weight-budget screen below.
- `discreteFitNotes(cat, picks)`: the budget is the single largest GPU pool × the existing usable fraction — never system RAM, never a sum across devices; the screen is labeled "discrete GPUs: preliminary weight-budget screen against the largest GPU's …, not system RAM (weights + estimated overhead only; context/KV-cache not included, runtime fit not verified)". Picks larger than that budget are disclosed by tier and name ("larger than any single GPU: …"); a discrete topology with no known pool size is disclosed as "per-GPU memory unknown: hardware fit not verified". Picks whose catalog size is unknown get an explicit unknown-size disclosure naming the model ("model size unknown: … fit not verified") — never an invented verdict either way, and never silence. Server band picks still defer unchanged (the server is not contradicted), but the client refuses to present them as machine-fit-verified. The screen is driven by the picks actually passed in — for the preview path these are the user's selected local picks, not merely the server's recommended defaults.

### Preview — `source/clients/cli/internal/ui/wizard_page.go`

`addModelDownloadDisclosure` extends the existing unknown-RAM branch: unknown topology (including old servers) prints "Memory topology is unknown; hardware fit unknown…"; unified machines print the preliminary weight-budget deferral line ("Unified memory: preliminary weight-budget screen only; context/KV-cache headroom and runtime fit are not verified here. Runtime checks still apply."); discrete machines print the preliminary weight-budget screen against the user's selected local picks (never mutating them). The catalog-unavailable branch is retained unchanged.

### Tests added

- `sysram` deterministic topology tests (parse + classification, all platforms).
- `agentclient/runtime_hardware_test.go` — proto mapping incl. old-server defaults.
- `wizard_recommend_test.go` — `TestRecommendEqualRAMDifferentTopology` (same 64 GiB RAM: probed-unified discloses the preliminary-screen context deferral; discrete-fits passes the per-GPU budget screen; discrete two×8 GiB cards must not be summed into a 16 GiB budget and the too-large pick is named per tier; discrete with unknown pool; topology-unknown note) and `TestRecommendOldServerDefaultsDisclosed` (old server: picks defer but "memory topology unknown" is disclosed, never a discrete/unified claim).
- `wizard_recommend_test.go` honesty contract — `TestRecommendDiscreteScreenIsPreliminaryWeightBudget` (the discrete note must name a "preliminary weight-budget screen" and must never claim "fit checked"), `TestRecommendDiscreteUnknownModelSizeDisclosed` (unknown-size picks get an explicit disclosure, not silence), `TestRecommendUnifiedDisclosesContextDeferral` (probed unified machines get the context/KV-cache deferral note, not silence). These were written as failing reproductions of the misleading "fit checked" disclosures first, then satisfied by the wording/behavior fixes.
- `wizard_preview_test.go` — `TestWizardPreviewDisclosesUnknownTopology`, `TestWizardPreviewDisclosesDiscreteFitPerGPU` (2×8 GiB GPUs, 64 GiB RAM, 32 GiB pick: must disclose per-GPU budget and name the unfit pick; never promise system-RAM fit), and `TestWizardPreviewDiscreteScreensSelectedPicks` (the preview screens the user's actual selected local picks — an unfit custom pick is named — the screen is labeled preliminary, and `wp.state.TierPicks` is unchanged after screening).

### Verification

- `gofmt -l` clean for all touched packages; `go vet` clean for `internal/ui`.
- Builds: `go build ./...` in both `source/server` and `source/clients/cli` modules pass.
- Tests: `go test ./internal/sysram/... ./pkg/agentclient/...` (server module) and `go test ./internal/ui/...` (CLI module) all pass, including all pre-existing recommendation/preview/apply/resume/cancellation tests (no regressions to explicit-pick preservation, context/runtime safety checks, or no-side-effect guarantees).

### Remaining limitations (honest disclosure)

- The server's band selection itself remains RAM-only; topology-awareness is disclosure + per-GPU preliminary screening at the client, not a new topology-based band generator.
- nvidia-smi is the only discrete-GPU probe (no AMD `rocm-smi` or Windows `nvidia-smi` path verified); AMD/other discrete GPUs on Linux report unknown.
- The discrete screen uses weights + a heuristic overhead fraction against one GPU pool; it does not verify multi-GPU sharding, runtime allocation, or actual context/KV-cache residency. Context validation remains deferred everywhere: neither unified nor discrete paths check context/KV-cache headroom; both say so explicitly.
- No specific platform/model compatibility is claimed anywhere in the wizard surface: all fit language is a preliminary weight-budget screen with "runtime checks still apply" caveats.
- Qwen default remains gated: no `qwen35` allowlist change, no downloads, no runtime restart/stop, no live credential reads, no paid inference, no push/merge. Runtime validation stays deferred per the user's earlier approval.

## Review fix — hardware disclosure honesty (2026-10-17, post-slice acceptance review)

The first implementation of the hardware slice shipped misleading disclosures: `discreteFitNotes` said "fit checked" although it only compared weights plus heuristic overhead (no context-cache check), it silently skipped picks whose catalog size was unknown, it screened only the server's `RecommendedOpenModels` rather than the user's actual preview selections, and `hardwareNotes` returned no caveat for probed unified machines even though context validation is deferred. Failing reproduction tests were written first (`TestRecommendDiscreteScreenIsPreliminaryWeightBudget`, `TestRecommendDiscreteUnknownModelSizeDisclosed`, `TestRecommendUnifiedDisclosesContextDeferral`, `TestWizardPreviewDiscreteScreensSelectedPicks`), then satisfied:

- `wizard_recommend.go` — the screen is now textually a "preliminary weight-budget screen" (never a fit check), for both unified and discrete; unknown model sizes are disclosed by name instead of skipped; `hardwareNotes` takes the picks to screen so the preview path can pass actual selected local picks.
- `wizard_page.go` — the preview screens `wp.selectedOpenPicks()` (actual selected local picks; explicit picks preserved, no state mutation) and prints the unified deferral line.
- Server probe-to-wire coverage added in `server/internal/server/runtime_hardware_wire_test.go`: `applyHardwareProbes` (the fake probe seam — no live hardware needed) pins unified, discrete (per-GPU pools, unknown-size GPU kept as explicit unknown), unknown, and RAM-probe-failed mappings plus `hardware_ok` gating; the real `ListRuntimeModels` handler is exercised for internal consistency only (no committed hardware figures, no platform/model compatibility claims); the no-manager path returns no hardware claims.

Verification for this review: `go test ./internal/ui/ -count=1` (full CLI UI package) passes; `go test ./internal/server/ -run 'TestApplyHardwareProbes|TestListRuntimeModels' -count=1`, `go test ./pkg/agentclient/ -count=1`, and `go test ./internal/sysram/... -count=1` pass; `go build ./...` passes in both `source/server` and `source/clients/cli` modules. Remaining scope: integrated end-to-end wizard scenario coverage (Phase 5 unchecked items), candidate runtime-load validation (deferred), and Phase 3/4 unchecked UX/persistence tasks.

## Direct acceptance follow-up — 2026-10-17

Two preview/apply mismatches were reproduced with failing tests and fixed: embedding assignments were applied but absent from confirmation, and edited cloud choices on reused accounts were hidden behind the previous effective model (52492ae1b31f). Preview now includes all owned local slots and uses the cloud apply calculation for pending choices, including clear-to-default and shared Economy conflicts. UI/wizard/slash tests passed; CLI build passed.

Fresh focused integration verification passed: server sysram and agentclient packages; server TestApplyHardwareProbes and TestListRuntimeModels tests; llama-server TestRecommendedOpenModels and TestProfileForRAM tests. No runtime inference, downloads, or active-runtime changes were performed.

Remaining topology policy gap: RecommendedOpenModels(totalBytes) still selects RAM-band defaults. CLI discreteFitNotes only warns when weights plus overhead exceed the largest individual GPU budget; it does not choose smaller recommendations. A machine with ample RAM and a small discrete GPU therefore retains large default picks. The runtime exposes configurable GPU-layer offload, so single-GPU-resident recommendations and RAM-based recommendations with partial CPU offload represent distinct quality/performance policies. Hardware API support alone does not decide which automatic default policy to use. Explicit existing assignments must remain untouched under either policy.
