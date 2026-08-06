# v0.84.0 release ledger

Audit target: official `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.84.0`, SHA `a5f43bf8aff3c55752432655f7334e3dafd1e256`, published 2026-08-06.

Previous accepted baseline: `v0.83.0`, SHA `845d6ff1f6643aba440341cce877ce1c43ebbc39`.

Exact checkouts/artifacts:

- Source tag checkout: `/workspace/tmp/pi-v0840`
- Previous tag checkout: `/workspace/tmp/pi-v0830-fresh`
- Official npm package artifact for generated provider JSON data: `/workspace/tmp/pi-ai-0.84.0-package/package`

## Material delta disposition

| Upstream area | Disposition | Go files/tests/evidence |
| --- | --- | --- |
| Baseten provider (`src/providers/baseten.ts`, `baseten.models.ts`, `test/baseten-models.test.ts`) | **Adopted.** Added `ProviderBaseten`, `BASETEN_API_KEY`, catalog generation support, Baseten `thinkingFormat`, `chat_template_args`, max-token field and long-cache metadata. | `types.go`, `env.go`, `compat.go`, `models_generated.go`, `inference/provider/openai/openai.go`, `inference/provider/openai/openai_v0840_test.go` |
| Advanced sampling parameters (`StreamOptions.samplingParams`, `Model.samplingParams`) | **Adopted for OpenAI-compatible adapters.** Go exposes `StreamOptions.SamplingParams` and `Model.SamplingParams`; request-level keys merge over model defaults and marshal last so they override typed fields. | `types.go`, `inference/provider/openai/openai.go`, `inference/provider/openairesponses/responses.go`, `openai_v0840_test.go`, `responses_v0840_test.go` |
| vLLM `thinking_token_budget` guard | **Adopted.** Go emits `thinking_token_budget` when compat flag is true and reasoning requested, clamping `xhigh/max` via high and always reserving 1024 answer tokens. | `compat.go`, `simple_options.go`, `openai.go`, `openai_v0840_test.go` |
| Missing `finish_reason` compatibility for non-standard OpenAI streams | **Adopted.** `SupportsFinishReason=false` now infers stop/toolUse instead of erroring on streams that end without finish_reason. | `compat.go`, `openai.go`, `openai_v0840_test.go` |
| Anthropic `content_block_start` initial text/thinking/signature and signature delta | **Adopted.** Initial block payload is preserved before deltas; thinking signatures append signature deltas. | `inference/provider/anthropic/anthropic.go`, `anthropic_v0840_test.go` |
| OpenAI Responses incomplete status details | **Adopted.** `rawStopReason` preserves `status.reason`; only `max_output_tokens` maps to length, other incomplete reasons map to error with message. | `inference/provider/openairesponses/responses.go`, `responses_v0840_test.go` |
| Bedrock failure diagnostics metadata | **Adapted.** JS SDK `$metadata` diagnostics mapped to Go AWS/Smithy metadata/error interfaces; bounded diagnostic details include status, modeled/unmodeled `*Exception` code, and request id without changing main error text. | `inference/provider/bedrock/bedrock.go`, `bedrock_stream_test.go` |
| Text model catalog/provider metadata refresh | **Adopted mechanically.** Generated from exact v0.84 source plus official npm data shards; 1153 text models across 38 providers. | `models_generated.go`, `models_test.go`, `qwen_token_plan_upstream_test.go`, comparator exact `1153/1153` |
| Image model catalog refresh | **Adopted mechanically.** Exact v0.84 image catalog is 42 OpenRouter image models, adding `qwen/qwen-image-3` and `qwen/qwen-image-3-pro`. | `images/models_generated.go`, `images_test.go`, image generator diff exact |
| Model runtime/provider refresh publication changes | **Already present / adapted.** Existing Go runtime has provider-scoped `ModelRuntime`, stores, generation/in-flight dedupe, cache restore/fallback and publication tests from prior parity; v0.84 TS publication refinements do not require public Go API change. | `models_runtime.go`, `models_runtime_test.go` |
| OAuth refresh callback/auth operation cancellation changes | **Adapted / mostly N/A.** Go OAuth providers expose context-aware internal network paths and deterministic cancellation tests; TS `AuthOperationOptions`/prompt abort plumbing is JS app/store API and has no direct Go store analogue. | `oauth/*`, existing `oauth/*_test.go`; docs N/A rationale |
| Google shared retry/signed empty block tests | **Audited.** Go Google provider already has context/retry and signed thinking/tool-block behavior coverage where applicable; TS SDK-specific retry wrapper surfaces are N/A. | `inference/provider/google/*`, existing Google tests |
| Monorepo `message_update` delta-only JSON protocol | **N/A for go-ai.** The change lives under `packages/agent`/`packages/coding-agent` JSON/RPC event transport, not `packages/ai`; Go AI emits typed in-process events, not Pi coding-agent JSON session events. | Upstream evidence: `packages/coding-agent/src/modes/json-event.ts`; no Go agent protocol in this repo |
| Documentation/package/test harness changes | **N/A or documentation-only.** README/changelog/vitest/package updates do not alter Go runtime except where specific behavior above was ported. | This ledger and `RELEASE.md` |

## Changed-path matrix (101 `packages/ai` paths)

| Upstream changed paths | Disposition |
| --- | --- |
| `CHANGELOG.md`, `README.md`, `package.json`, `vitest.config.ts` | Docs/package/test-runner metadata; N/A to Go runtime, recorded in release docs. |
| `scripts/generate-models.ts`, `src/model-catalog.ts`, `src/models.generated.ts`, `src/models-store.ts`, `src/models.ts`, `src/providers/*.models.ts`, `src/providers/data/*.json` via npm artifact | Adopted/adapted by `scripts/generate-models.go`, generated `models_generated.go`, runtime publication already present, exact comparator. |
| `src/image-models.generated.ts` | Adopted by `scripts/generate-image-models.py` and `images/models_generated.go`; image count 42. |
| `src/providers/baseten.ts`, `src/providers/baseten.models.ts`, `src/providers/all.ts` | Adopted as catalog/env/provider constant and OpenAI-compatible Baseten request semantics. |
| `src/api/openai-completions.ts`, `src/api/simple-options.ts` | Adopted: sampling params, Baseten chat_template_args/reasoning_effort, thinking_token_budget, supportsFinishReason. |
| `src/api/openai-responses.ts`, `src/api/azure-openai-responses.ts`, `src/api/openai-responses-shared.ts` | Adopted: sampling params and incomplete detail mapping; Azure shares Go Responses path. |
| `src/api/anthropic-messages.ts` | Adopted content_block_start initial content/signature behavior; other TS auth/header/cache-control refinements audited against existing Go compat fields. |
| `src/api/bedrock-converse-stream.ts` | Adapted Bedrock diagnostics with Go AWS SDK metadata and stream errors. |
| `src/api/google-shared.ts`, `src/api/google-vertex.ts`, `src/api/openai-codex-responses.ts`, `src/api/openai-codex.ts`, `src/api/openai-codex-websocket.ts`, `src/api/cloudflare-ai-gateway-stream.ts` | Audited. Existing Go provider paths already cover current material behavior or JS SDK/transport-specific deltas are N/A; no untested Go behavior identified. |
| `src/auth/**`, `src/oauth.ts`, `src/bun-oauth.ts`, `src/utils/abort.ts` | Adapted/N/A. Go has direct provider OAuth helpers and context-aware network functions; JS app credential-store/prompt abort interfaces do not map 1:1. |
| `src/types.ts`, `src/compat.ts`, `src/index.ts`, `src/env-api-keys.ts`, `src/legacy-api-aliases.ts` | Adopted where Go-facing: Baseten provider, sampling params, new compat fields. TS barrel/lazy aliasing is N/A. |
| All changed `test/*.test.ts` | Classified: deterministic Go regressions added for adopted behavior; live-provider Baseten/tokens/abort/empty/image/tool-result cases are represented by payload/catalog/env tests and existing provider stream tests rather than hidden skips. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers
upstream pairs: 1153
generated pairs: 1153
model provider/id pairs match exactly

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840.go
wrote /workspace/tmp/images_v0840.go with 42 image models
# images/models_generated.go regenerated to the same 42-model catalog.
```

## Validation gate

Passed before commit/push:

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers
# upstream pairs: 1153
# generated pairs: 1153
# model provider/id pairs match exactly

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840_gate.go
# wrote /workspace/tmp/images_v0840_gate.go with 42 image models
# diff against images/models_generated.go: exact

make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Retained logs: `/workspace/tmp/go-ai-v0840-gates/`.
