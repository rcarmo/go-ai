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
| Deferred/background response lifecycle APIs | **Implemented.** Added `DeferredHandle`, `StopReasonDeferred`, `Message.Deferred`, `StreamOptions.Deferred`/`WaitMs`, optional `ApiProvider.FetchDeferred`/`CancelDeferred`, top-level `FetchDeferred`/`CancelDeferred`, unsupported-capability errors, context cancellation, and faux pending/ready/failed/cancelled lifecycle tests. | `types.go`, `registry.go`, `inference/provider/faux/faux.go`, `inference/provider/faux/faux_test.go` |
| Model runtime/provider refresh publication changes | **Already present / adapted.** Existing Go runtime has provider-scoped `ModelRuntime`, stores, generation/in-flight dedupe, cache restore/fallback and publication tests from prior parity; v0.84 TS publication refinements do not require public Go API change. | `models_runtime.go`, `models_runtime_test.go` |
| OAuth refresh callback/auth operation cancellation changes | **Implemented/adapted.** Added context-aware OAuth refresh/runtime APIs, patched network refresh paths to honor caller context, and retained JS credential-store/prompt UI pieces as N/A. | `oauth/oauth.go`, `oauth/apply.go`, provider refresh files, `oauth/oauth_test.go` |
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
| `src/types.ts`, `src/models.ts`, `src/api/lazy.ts`, `src/providers/faux.ts`, `src/compat.ts`, `src/index.ts`, `src/env-api-keys.ts`, `src/legacy-api-aliases.ts` | Adopted where Go-facing: Baseten provider, sampling params, new compat fields, public deferred lifecycle types and optional API capabilities. TS barrel/lazy module loading is N/A; Go exposes capabilities by non-nil `ApiProvider` function fields. |
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




## Correction cycle 4 (whole-corpus Responses/Cloudflare parity)

Auditor whole-corpus parity-hardening slice addressed:

- Added exact cumulative 127-file upstream test disposition manifest: `docs/v0840-127-test-manifest.md`.
- Explicitly classified the five credential-gated E2E files as N/A/live-only, never as passing: `anthropic-eager-tool-input-e2e.test.ts`, `anthropic-long-cache-retention-e2e.test.ts`, `openai-codex-cache-affinity-e2e.test.ts`, `openai-responses-cache-affinity-e2e.test.ts`, and `openai-responses-reasoning-replay-e2e.test.ts`.
- Ported deterministic `openai-responses-compat.test.ts` gaps: `OpenAIResponsesCompat.SessionAffinityFormat`, OpenRouter provider/baseURL auto-detection, `openai-nosession`/OpenCode behavior, `cacheRetention:none` suppression, explicit header override, required `ToolChoice`, service-tier cost multipliers, off→`none` vs unsupported omission, and xAI encrypted reasoning include.
- Ported the `cloudflare-stream.test.ts` assertion-level addendum: unresolved `{VAR}` placeholders are preserved instead of becoming empty strings, and resolved/unresolved behavior is covered through ordinary stream and `streamSimple`/request dispatch.
- Focused tests: `go test ./... -run 'OpenAIResponsesCompat|OpenAIResponsesCacheRetention|OpenAIResponsesExplicitHeaders|OpenAIResponsesRequiredToolChoice|OpenAIResponsesServiceTier|OpenAIResponsesOffReasoning|XAIResponsesAlways|ResolveCloudflare|CloudflareBaseURL'` and `go test ./...` pass before full gates.

## Correction cycle 3 (OAuth refresh cancellation and telemetry context)

Auditor OAuth/telemetry correction addressed:

- Added context-aware OAuth refresh entry points: optional `ContextRefreshProvider`, `GetAPIKeyWithContext`, `GetAPIKeyWithMinValidityContext`, and `RuntimeForProviderContext`; legacy context-free APIs wrap `context.Background()` for compatibility.
- Patched real OAuth refresh paths to honor caller context where they perform network refresh: Anthropic, OpenAI Codex, Google Gemini CLI/Antigravity, GitHub Copilot, Kimi Coding, Radius, xAI; OpenRouter refresh checks context before returning the stored key.
- `RuntimeForProviderContext` propagates caller cancellation into OAuth refresh and package dynamic model refresh (`goai.RefreshModels(ctx, true)`).
- Added deterministic OAuth tests for pre-cancelled context, cancellation during refresh, retained `ModelsError` cause typing, and dynamic model refresh cancellation via `ModelRefreshContext.Signal`.
- Added typed opaque `TelemetryContext` and telemetry-aware hooks for text streams/deferred fetch/deferred cancel and image generation. `RequestMetadata` remains provider request metadata and is not used as telemetry.
- Added tests for telemetry propagation through `InvokeOnPayload`/`InvokeOnResponse`, deferred fetch/cancel faux callbacks, and OpenRouter image payload/response hooks.
- OAuth/telemetry correction gate passed; retained logs: `/workspace/tmp/go-ai-v0840-oauth-telemetry-gates/`.

## Correction cycle 2 (deferred response lifecycle)

Auditor contrary evidence from upstream commit `382aa641` inside `v0.84.0` addressed:

- Added public deferred/background response lifecycle surface: `DeferredHandle`, `StopReasonDeferred`, `Message.Deferred`, `StreamOptions.Deferred`, `StreamOptions.WaitMs`, `ApiProvider.FetchDeferred`, `ApiProvider.CancelDeferred`, top-level `FetchDeferred`, and top-level `CancelDeferred`.
- Added context cancellation and unsupported-capability errors for deferred fetch/cancel. Provider fetch failures are returned in-band as assistant messages with `stopReason: "error"`, matching the background lifecycle shape.
- Extended faux provider with deterministic deferred submission, pending polling, ready redemption, failed redemption, cancellation recording, cancelled fetch, and state counters.
- Re-audited `providers.test.ts`, `telemetry-options.test.ts`, `types.ts`, `models.ts`, `api/lazy.ts`, and `providers/faux.ts`. Go maps lazy API capability exposure to optional `ApiProvider` function fields; telemetry context is implemented as opaque `TelemetryContext` propagated through stream/deferred/image hooks.
- Verified `ProviderHeaders` null deletion is already represented by `SuppressHeaders`/`MergeProviderHeaders`, model refresh options/results by `ModelRuntimeRefreshOptions`/`ModelRuntimeRefreshResult`, and runtime API-key/OAuth refresh separation by Go's explicit OAuth runtime helpers rather than a JS `setRuntimeApiKey` mutator.
- Deferred correction gate passed; retained logs: `/workspace/tmp/go-ai-v0840-deferred-gates/`.

## Correction cycle 1 (post-`f6112ee`)

Auditor correction addressed before final acceptance:

- Ported v0.84 `src/utils/validation.ts` union semantics: nullable union arms are matched before coercion, `oneOf`/`anyOf` are traversed, and `anyOf` number/null coercion is supported. Tests: `upstream_validation_test.go` v0.84 nullable union cases.
- Expanded OpenAI `thinking_token_budget` regressions to the upstream edge matrix: capability disabled, reasoning off, xhigh/max→high, defaults, custom budgets, model/caller ceilings, answer-room clamp, and zero/no emission.
- Expanded sampling regressions: zero preservation, absent omission, model defaults, request precedence, typed-field override, OpenAI Responses override, and Anthropic/Google ignore behavior.
- Expanded Bedrock diagnostic matrix with Go/AWS-idiomatic production paths for send/stream errors, status/requestId/errorCode, Unknown suppression, oversized metadata filtering, and no-metadata suppression.
- Added `docs/v0840-release-ledger.md` changed-test delta appendix covering all 46 upstream changed test-related paths with named Go evidence or precise N/A rationale.

Final corrective gate passed before commit/push. Retained logs: `/workspace/tmp/go-ai-v0840-correction-gates/`.

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



Correction cycle 4 gate evidence:

```text
docs/v0840-127-test-manifest.md row check: 127 rows, 104 deterministic/covered, 23 N/A, 0 TODO
go test ./... -run 'OpenAIResponsesCompat|OpenAIResponsesCacheRetention|OpenAIResponsesExplicitHeaders|OpenAIResponsesRequiredToolChoice|OpenAIResponsesServiceTier|OpenAIResponsesOffReasoning|XAIResponsesAlways|ResolveCloudflare|CloudflareBaseURL'
go test ./...
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers  # 1153/1153 exact
python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840_whole_corpus.go  # 42 image models, exact diff
make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Retained logs: `/workspace/tmp/go-ai-v0840-whole-corpus-gates/`.

Correction cycle 3 gate evidence:

```text
go test ./... -run 'GetAPIKeyWithContext|RuntimeForProviderContext|TelemetryContext|GenerateImagesOpenRouterHooksAndResponse|FauxDeferred'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers  # 1153/1153 exact
python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840_oauth_telemetry.go  # 42 image models, exact diff
make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Correction cycle 2 gate evidence:

```text
go test ./... -run 'Deferred|FauxDeferred|StopReasonDeferred|UnsupportedAndContextCancellation'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers  # 1153/1153 exact
python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840_deferred.go  # 42 image models, exact diff
make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Correction cycle 1 gate evidence:

```text
go test ./... -run 'ValidationNullableUnion|ThinkingTokenBudgetUpstreamMatrix|AdvancedSamplingParamsUpstreamMatrix|IgnoresAdvancedSamplingParams|BedrockFailureDiagnostic'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers  # 1153/1153 exact
python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840_correction.go  # 42 image models, exact diff
make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```


## v0.84.0 changed-test delta appendix (46 paths)

Exact command: `git diff --name-only v0.83.0..v0.84.0 -- packages/ai/test` from upstream tag `a5f43bf8aff3c55752432655f7334e3dafd1e256`. This appendix covers all 46 changed test-related paths (45 `*.test.ts` files plus `test/oauth.ts` helper), including modified assertions rather than only newly added files.

| Upstream changed test path | Disposition | Named Go evidence / rationale |
| --- | --- | --- |
| `packages/ai/test/abort.test.ts` | N/A/live-provider expanded Baseten coverage | Live abort matrix adds Baseten; Go has existing provider abort/context tests, Baseten covered by deterministic payload/catalog tests, no credentials in native gate. |
| `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | UNCHANGED Go-facing assertions / covered | Metadata expectations remain covered by generated catalog and Anthropic thinking tests; v0.84 did not require new Go code beyond catalog regen. |
| `packages/ai/test/anthropic-auth-token.test.ts` | covered existing | Anthropic bearer/API-key precedence already ported in provider auth tests; changed assertions are auth plumbing, not new v0.84 behavior. |
| `packages/ai/test/anthropic-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Network refresh cancellation now uses `RefreshTokenContext`; JS credential-store UI prompts remain N/A. |
| `packages/ai/test/anthropic-sse-parsing.test.ts` | DETERMINISTIC-PORTED | New initial content/signature assertion ported in `inference/provider/anthropic/anthropic_v0840_test.go`; parsing no-usage/refusal cases already covered. |
| `packages/ai/test/baseten-models.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/openai_v0840_test.go` covers Baseten metadata/env/chat_template_args/reasoning_effort. |
| `packages/ai/test/bedrock-error-metadata.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `inference/provider/bedrock/bedrock_stream_test.go` covers send/stream diagnostics, requestId/status/errorCode, Unknown/oversized filtering, no metadata suppression. Abort-specific JS promise path N/A to extraction helper; production skips diagnostics on context abort. |
| `packages/ai/test/context-overflow.test.ts` | N/A/live-provider plus existing simulated coverage | Changed live matrices add Baseten/other provider observations; Go simulated overflow tests cover deterministic classification without credentials. |
| `packages/ai/test/cross-provider-handoff.test.ts` | N/A/live-provider | Live cross-provider handoff matrix; no new deterministic Go runtime semantics beyond existing transform/replay tests. |
| `packages/ai/test/deferred-tools.test.ts` | covered existing | Deferred tool planning/tool_search/Kimi/Anthropic references already covered by Go deferred tool tests; no new v0.84 assertion needing code change. |
| `packages/ai/test/empty.test.ts` | N/A/live-provider plus existing coverage | Live empty-message matrix adds Baseten; Go has deterministic empty tool-result/request tests and Baseten payload metadata. |
| `packages/ai/test/error-body.test.ts` | covered existing | Provider error-body normalization already ported; Bedrock metadata additions covered in this correction. |
| `packages/ai/test/fireworks-models.test.ts` | catalog covered | Fireworks metadata changes covered by exact catalog generation/comparator and model metadata tests where deterministic. |
| `packages/ai/test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Copilot refresh/model availability fetch now has context-aware path; JS auth-operation store details remain N/A. |
| `packages/ai/test/google-shared-gemini3-unsigned-tool-call.test.ts` | covered existing | Google signed/unsigned tool-call behavior covered by existing Google provider tests; no new Go-facing v0.84 semantic gap found. |
| `packages/ai/test/google-shared-retry.test.ts` | N/A/covered by Go retry helpers | TS Google SDK retry wrapper behavior; Go uses HTTP/context retry helpers and provider tests. |
| `packages/ai/test/google-shared-signed-empty-blocks.test.ts` | N/A/covered | TS SDK signed empty block serialization; Go signed/thinking/tool payload behavior is covered where applicable. |
| `packages/ai/test/image-tool-result.test.ts` | N/A/live-provider plus existing coverage | Live provider matrix adds Baseten; Go deterministic image tool result serialization tests remain applicable. |
| `packages/ai/test/kimi-coding-oauth.test.ts` | covered existing | Go Kimi Coding OAuth device/refresh tests cover deterministic behavior; JS auth operation option changes N/A. |
| `packages/ai/test/model-catalog-types.test.ts` | catalog covered | Generated catalog type/data metadata covered by generator, exact comparator, and compile/tests. |
| `packages/ai/test/models-runtime.test.ts` | covered existing | Go `models_runtime_test.go` covers refresh publication, generation checks, cache fallback, cancellation, in-flight dedupe; no new public API needed. |
| `packages/ai/test/oauth-auth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `GetAPIKeyWithContext`/`RuntimeForProviderContext` cover caller-owned cancellation and cause typing; TS credential-store API remains N/A. |
| `packages/ai/test/oauth-device-code.test.ts` | covered existing | Device-code interval/slow_down/cancel tests already present in Go OAuth tests. |
| `packages/ai/test/oauth.ts` | N/A helper | Shared TS test helper changes only support auth test options; no Go runtime file. |
| `packages/ai/test/openai-codex-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Codex refresh now honors context; device behavior remains covered by existing tests. |
| `packages/ai/test/openai-codex-stream.test.ts` | covered existing | Codex Responses status/raw/pending behavior already covered; lint-only error string changes do not alter public ErrorMessage. |
| `packages/ai/test/openai-completions-prompt-cache.test.ts` | covered existing | Prompt cache/session affinity behavior already covered; v0.84 changes did not create new Go gap. |
| `packages/ai/test/openai-completions-thinking-as-text.test.ts` | covered existing | Thinking-as-text compat remains covered; sampling/finish fields expanded separately. |
| `packages/ai/test/openai-completions-thinking-token-budget.test.ts` | DETERMINISTIC-PORTED | Expanded `TestOpenAIThinkingTokenBudgetUpstreamMatrix` covers disabled capability, reasoning off, xhigh/max, defaults, custom budgets, model/caller ceiling, answer room and zero omission. |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | covered/catalog | Tool choice/constrained sampling existing Go tests plus exact catalog metadata; Baseten-specific payload covered in new test. |
| `packages/ai/test/openai-completions-tool-result-images.test.ts` | covered existing | OpenAI-compatible image tool-result serialization already covered; no v0.84-specific Go behavior beyond sampling/Baseten. |
| `packages/ai/test/openai-responses-terminal-event.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openairesponses/responses_v0840_test.go` covers incomplete_details raw status and length/error mapping. |
| `packages/ai/test/openrouter-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | OpenRouter refresh checks caller context before returning stored key; key exchange tests remain covered. |
| `packages/ai/test/overflow.test.ts` | covered existing | Recoverable length/overflow helpers covered by Go overflow tests; no new v0.84 semantics beyond catalog/live provider additions. |
| `packages/ai/test/providers.test.ts` | DETERMINISTIC-PORTED | Baseten/catalog/provider metadata covered by comparators and `models_test.go`; deferred submit/poll/ready/fail/cancel lifecycle assertions ported in `inference/provider/faux/faux_test.go`. |
| `packages/ai/test/qwen-token-plan-models.test.ts` | DETERMINISTIC-PORTED/catalog | `qwen_token_plan_upstream_test.go` updated for v0.84 `qwen3.8-max` and `deepseek-v4-flash-0731` IDs. |
| `packages/ai/test/radius-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Radius refresh now uses caller context through token/config/catalog paths; existing Radius tests cover refresh/catalog behavior. |
| `packages/ai/test/sampling-options.test.ts` | DETERMINISTIC-PORTED | OpenAI/Responses sampling merge/override tests plus Anthropic/Google ignored tests added. |
| `packages/ai/test/stream.test.ts` | covered existing/live | General stream/live provider matrix changes covered by provider stream tests; Baseten live stream N/A without credentials. |
| `packages/ai/test/telemetry-options.test.ts` | DETERMINISTIC-PORTED | Added opaque `TelemetryContext` and telemetry-aware hooks for stream/deferred/image callbacks. Tests: `TestTelemetryContextHooks`, faux deferred telemetry assertions, and OpenRouter image telemetry hooks. |
| `packages/ai/test/tokens.test.ts` | N/A/live-provider plus existing simulated coverage | Live token accounting matrix adds Baseten; Go token accounting covered by simulated provider tests. |
| `packages/ai/test/tool-call-without-result.test.ts` | N/A/live-provider plus existing coverage | Live matrix additions; Go deterministic tool-call filtering tests already cover runtime behavior. |
| `packages/ai/test/total-tokens.test.ts` | N/A/live-provider plus existing coverage | Live total-token matrix adds Baseten; Go cost/token computations covered by deterministic tests. |
| `packages/ai/test/unicode-surrogate.test.ts` | covered existing | Unicode surrogate sanitization tests already ported; v0.84 changes do not alter Go-facing behavior. |
| `packages/ai/test/validation.test.ts` | DETERMINISTIC-PORTED | `upstream_validation_test.go` now includes v0.84 nullable union match-before-coerce cases and anyOf number/null coercion. |
| `packages/ai/test/xai-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | xAI refresh now uses caller context; device/refresh tests remain covered. |
