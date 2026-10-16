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

## Outstanding verification

Exact pinned binary architecture support; compatibility-gate update requirements; actual Qwen GGUF tensor precision and runtime/tool-calling behavior; GLM memory calculation; hardware-specific safe context defaults; smaller-machine candidates; and complete existing-user configuration-preservation audit. No shipping recommendation or application code has changed.
