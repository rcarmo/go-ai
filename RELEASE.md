# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.1`
- Upstream tag/SHA: `53fa77ccd8a279eb87e92294ef3687b03ff80112`
- Published: 2026-08-07T06:07:00Z
- Previous accepted baseline: `v0.84.0` / `a5f43bf8aff3c55752432655f7334e3dafd1e256`
- Exact upstream checkout used: `/workspace/tmp/pi-v0841`
- Exact previous checkout used: `/workspace/tmp/pi-v0840`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/pi-ai-0.84.1-package/package`
- Previous official npm data artifact: `/workspace/tmp/pi-ai-0.84.0-package/package`
- Detailed path matrix: `docs/v0841-release-ledger.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from official `v0.84.0` to official `v0.84.1`, no unpublished `main` changes.

Audited scope:

- 25 changed `packages/ai` paths.
- Official tag SHA verified: `53fa77ccd8a279eb87e92294ef3687b03ff80112`.
- Material upstream deltas: new `qwen-token-plan-individual` provider/models, generator/model-data/env/type/provider registration updates, 14 modified AI tests plus new `generate-models-strict.test.ts`, and package/docs/version updates.

## v0.84.1 Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| `qwen-token-plan-individual` provider | Implemented. Added `ProviderQwenTokenPlanIndividual`, `QWEN_TOKEN_PLAN_API_KEY` reuse, OpenAI Completions catalog entries, and exact international endpoint `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1`. |
| Exact Individual seven-model allowlist | Implemented mechanically from exact v0.84.1 provider data: `deepseek-v4-flash-0731`, `deepseek-v4-pro`, `glm-5.2`, `qwen3.6-flash`, `qwen3.7-max`, `qwen3.7-plus`, `qwen3.8-max`. Tests: `qwen_token_plan_upstream_test.go`. |
| Qwen request fields | Implemented/fixed. Qwen `thinkingFormat:"qwen"` now emits `enable_thinking` and, when compat supports it, `reasoning_effort`. Tests: `inference/provider/openai/qwen_token_plan_individual_upstream_test.go`. |
| Text catalog refresh | Implemented mechanically. Exact npm artifact comparison: `1153/38` → `1220/39`, with 70 added, 3 removed, 9 metadata-changed pairs. Go comparator: `1220/1220` exact. |
| Image catalog | Proved unchanged. v0.84.1 image generation writes 42 image models and diffs clean against `images/models_generated.go`. |
| Strict generator rollback/failure policy | N/A/adapted-generator-policy. Upstream test targets private TS `generate-models.ts --strict` rollback behavior; Go consumes exact release artifacts and proves final output via exact comparators rather than claiming helper-policy parity. |
| 128-file corpus | Updated. `docs/v0841-128-test-manifest.md` has exact 128 rows, 101 deterministic/covered, 27 N/A/adapted, 0 TODO. Credential-gated live Qwen Individual additions are labeled, not claimed as passing. |

## v0.84.0 Go implementation and decisions (retained baseline history)

| Upstream delta | Go disposition |
| --- | --- |
| Baseten provider and models | Implemented. Added `ProviderBaseten`, `BASETEN_API_KEY`, exact generated Baseten models, Baseten `thinkingFormat`, `chat_template_args`, and reasoning-effort payload behavior. Tests: `inference/provider/openai/openai_v0840_test.go`. |
| Advanced sampling parameters | Implemented for OpenAI-compatible adapters. Added `Model.SamplingParams` and `StreamOptions.SamplingParams`; model defaults merge with request overrides and marshal last so advanced keys override typed fields. Tests cover OpenAI Completions and Responses. |
| `thinking_token_budget` | Implemented. Compat flag emits top-level `thinking_token_budget` and reserves 1024 answer tokens. Test: `TestOpenAIThinkingTokenBudgetLeavesAnswerRoom`. |
| Missing `finish_reason` compatibility | Implemented. `SupportsFinishReason=false` allows non-standard OpenAI-compatible streams to finish without `finish_reason`, inferring `stop` or `toolUse`. |
| Anthropic initial block assembly | Implemented. `content_block_start` text/thinking/signature are preserved and signature deltas append. Test: `TestAnthropicContentBlockStartInitialContentAndSignature`. |
| OpenAI Responses incomplete details | Implemented. `rawStopReason` preserves `status.reason`; `max_output_tokens` maps to `length`; other incomplete reasons map to error with an error message. Test: `TestResponsesIncompleteTerminalReasonMapping`. |
| Bedrock diagnostics metadata | Adapted. Go AWS/Smithy errors produce bounded `bedrock_response_failure` diagnostics with status, provider error code, and request id where available. Test: `TestProcessConverseStreamAddsFailureDiagnosticForStreamErr`. |
| Text model catalog refresh | Implemented mechanically. `models_generated.go` regenerated from exact v0.84 source plus official package provider data. Comparator: `1153/1153` provider/id pairs, exact. Provider count now 38 including Baseten. |
| Image model catalog refresh | Implemented mechanically. `images/models_generated.go` regenerated from exact v0.84 image metadata: 42 image models, adding `qwen/qwen-image-3` and `qwen/qwen-image-3-pro`. |
| Model runtime/provider refresh publication changes | Already present/adapted. Existing Go runtime has provider-scoped refresh, generation checks, in-flight dedupe, cache fallback and deterministic tests; no public Go API change required. |
| OAuth refresh callback/auth-operation cancellation changes | Implemented/adapted. Added context-aware OAuth refresh/runtime APIs, patched network refresh paths to honor caller context, and retained JS credential-store/prompt UI pieces as N/A. |
| `message_update` delta-only semantics | N/A for this Go AI library. Upstream change is in `packages/agent`/`packages/coding-agent` JSON/RPC session events, not `packages/ai`; Go emits typed in-process stream events rather than Pi coding-agent JSON timeline events. |
| JS package/docs/test harness changes | N/A or documented unless a runtime behavior above was ported. See detailed matrix. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0841/packages/ai/src/providers
upstream pairs: 1220
generated pairs: 1220
model provider/id pairs match exactly

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841.go
wrote /workspace/tmp/images_v0841.go with 42 image models
# diff against images/models_generated.go: exact

0.84.0 -> 0.84.1 text artifact diff: 1153/38 -> 1220/39, 70 added, 3 removed, 9 metadata changed.
```




## v0.84.0 correction cycle 4 (whole-corpus Responses/Cloudflare parity)

Auditor whole-corpus parity-hardening slice addressed:

- Added exact cumulative 127-file upstream test disposition manifest: `docs/v0840-127-test-manifest.md`.
- Explicitly classified the five credential-gated E2E files as N/A/live-only, never as passing: `anthropic-eager-tool-input-e2e.test.ts`, `anthropic-long-cache-retention-e2e.test.ts`, `openai-codex-cache-affinity-e2e.test.ts`, `openai-responses-cache-affinity-e2e.test.ts`, and `openai-responses-reasoning-replay-e2e.test.ts`.
- Ported deterministic `openai-responses-compat.test.ts` gaps in `inference/provider/openairesponses`: session-affinity formats, OpenRouter provider/baseURL auto-detection, `openai-nosession`/OpenCode behavior, `cacheRetention:none` header/body suppression, explicit header override, required `ToolChoice`, service-tier cost multipliers, off→`none` vs unsupported omission, and xAI encrypted reasoning include.
- Corrected whole-corpus evidence labels: `image-model-data.test.ts`, `model-data-validation.test.ts`, and `reasoning-options.test.ts` are classified N/A/adapted-generator-policy because upstream exercises private TS generator/script helpers; exact final Go artifacts remain verified separately by catalog tests and 1153/1153 plus 42/42 comparators.
- Strengthened `xai-responses.test.ts` evidence with executable request-shape coverage for exact low/medium/high-only levels, bearer auth, developer prompt, `/responses`, `session_id` + `prompt_cache_key`, `store=false`, medium reasoning, encrypted reasoning include, and no long retention.
- Ported `cloudflare-stream.test.ts` placeholder parity: unresolved `{VAR}` placeholders are preserved, and resolved/unresolved behavior is covered through ordinary stream and `streamSimple`/request dispatch.
- Focused tests (`OpenAIResponsesCompat|OpenAIResponsesCacheRetention|OpenAIResponsesExplicitHeaders|OpenAIResponsesRequiredToolChoice|OpenAIResponsesServiceTier|OpenAIResponsesOffReasoning|XAI|ResolveCloudflare|CloudflareBaseURL`) and `go test ./...` passed before full gates.

## v0.84.0 correction cycle 3 (OAuth refresh cancellation and telemetry context)

Auditor OAuth/telemetry correction addressed:

- Added context-aware OAuth refresh entry points: optional `ContextRefreshProvider`, `GetAPIKeyWithContext`, `GetAPIKeyWithMinValidityContext`, and `RuntimeForProviderContext`; legacy context-free APIs wrap `context.Background()` for compatibility.
- Patched real OAuth refresh paths to honor caller context where they perform network refresh: Anthropic, OpenAI Codex, Google Gemini CLI/Antigravity, GitHub Copilot, Kimi Coding, Radius, xAI; OpenRouter refresh checks context before returning the stored key.
- `RuntimeForProviderContext` propagates caller cancellation into OAuth refresh and package dynamic model refresh (`goai.RefreshModels(ctx, true)`).
- Added deterministic OAuth tests for pre-cancelled context, cancellation during refresh, retained `ModelsError` cause typing, and dynamic model refresh cancellation via `ModelRefreshContext.Signal`.
- Added typed opaque `TelemetryContext` and telemetry-aware hooks for text streams/deferred fetch/deferred cancel and image generation. `RequestMetadata` remains provider request metadata and is not used as telemetry.
- Added tests for telemetry propagation through `InvokeOnPayload`/`InvokeOnResponse`, deferred fetch/cancel faux callbacks, and OpenRouter image payload/response hooks.
- OAuth/telemetry correction gate passed; retained logs: `/workspace/tmp/go-ai-v0840-oauth-telemetry-gates/`.

## v0.84.0 correction cycle 2 (deferred response lifecycle)

Auditor contrary evidence from upstream commit `382aa641` inside `v0.84.0` addressed:

- Added public deferred/background response lifecycle surface: `DeferredHandle`, `StopReasonDeferred`, `Message.Deferred`, `StreamOptions.Deferred`, `StreamOptions.WaitMs`, `ApiProvider.FetchDeferred`, `ApiProvider.CancelDeferred`, top-level `FetchDeferred`, and top-level `CancelDeferred`.
- Added context cancellation and unsupported-capability errors for deferred fetch/cancel. Provider fetch failures are returned in-band as assistant messages with `stopReason: "error"`, matching the background lifecycle shape.
- Extended faux provider with deterministic deferred submission, pending polling, ready redemption, failed redemption, cancellation recording, cancelled fetch, and state counters.
- Re-audited `providers.test.ts`, `telemetry-options.test.ts`, `types.ts`, `models.ts`, `api/lazy.ts`, and `providers/faux.ts`. Go maps lazy API capability exposure to optional `ApiProvider` function fields; telemetry context is implemented as opaque `TelemetryContext` propagated through stream/deferred/image hooks.
- Verified `ProviderHeaders` null deletion is already represented by `SuppressHeaders`/`MergeProviderHeaders`, model refresh options/results by `ModelRuntimeRefreshOptions`/`ModelRuntimeRefreshResult`, and runtime API-key/OAuth refresh separation by Go's explicit OAuth runtime helpers rather than a JS `setRuntimeApiKey` mutator.
- Deferred correction gate passed; retained logs: `/workspace/tmp/go-ai-v0840-deferred-gates/`.

## v0.84.0 correction cycle 1 (post-`f6112ee`)

Auditor correction addressed before final acceptance:

- Ported v0.84 `src/utils/validation.ts` union semantics: nullable union arms are matched before coercion, `oneOf`/`anyOf` are traversed, and `anyOf` number/null coercion is supported. Tests: `upstream_validation_test.go` v0.84 nullable union cases.
- Expanded OpenAI `thinking_token_budget` regressions to the upstream edge matrix: capability disabled, reasoning off, xhigh/max→high, defaults, custom budgets, model/caller ceilings, answer-room clamp, and zero/no emission.
- Expanded sampling regressions: zero preservation, absent omission, model defaults, request precedence, typed-field override, OpenAI Responses override, and Anthropic/Google ignore behavior.
- Expanded Bedrock diagnostic matrix with Go/AWS-idiomatic production paths for send/stream errors, status/requestId/errorCode, Unknown suppression, oversized metadata filtering, and no-metadata suppression.
- Added `docs/v0840-release-ledger.md` changed-test delta appendix covering all 46 upstream changed test-related paths with named Go evidence or precise N/A rationale.

Final corrective gate passed before commit/push. Retained logs: `/workspace/tmp/go-ai-v0840-correction-gates/`.

## Validation evidence

Focused validation and full gate passed before commit/push:

```text
docs/v0841-128-test-manifest.md row check: 128 rows, 101 deterministic/covered, 27 N/A/adapted, 0 TODO
TMPDIR=/workspace/tmp go test ./... -run 'QwenTokenPlan|QwenTokenPlanIndividual|RegisterBuiltinModels|OpenAICompletionsEmptyTools'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0841/packages/ai/src/providers
# upstream pairs: 1220
# generated pairs: 1220
# model provider/id pairs match exactly

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841_gate.go
# wrote /workspace/tmp/images_v0841_gate.go with 42 image models
# diff against images/models_generated.go: exact

make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Retained logs: `/workspace/tmp/go-ai-v0841-gates/`.



Correction cycle 4 gate evidence:

```text
docs/v0840-127-test-manifest.md row check: 127 rows, 101 deterministic/covered, 26 N/A/adapted, 0 TODO
go test ./... -run 'OpenAIResponsesCompat|OpenAIResponsesCacheRetention|OpenAIResponsesExplicitHeaders|OpenAIResponsesRequiredToolChoice|OpenAIResponsesServiceTier|OpenAIResponsesOffReasoning|XAI|ResolveCloudflare|CloudflareBaseURL'
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

Retained logs: `/workspace/tmp/go-ai-v0840-evidence-label-gates/`.

Prior whole-corpus logs retained: `/workspace/tmp/go-ai-v0840-whole-corpus-gates/`.

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


## Maintenance policy

For every future upstream release audit, update this file in the same release commit before declaring completion. The update must include:

- upstream release tag and SHA;
- previous accepted baseline tag and SHA;
- exact checkout paths or reproducible source locations;
- changed-path matrix link;
- catalog comparator counts;
- every Go implementation, fix, adaptation, and N/A decision;
- tests and full gate evidence.
