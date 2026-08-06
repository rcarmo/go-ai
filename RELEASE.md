# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.0`
- Upstream tag/SHA: `a5f43bf8aff3c55752432655f7334e3dafd1e256`
- Published: 2026-08-06
- Previous accepted baseline: `v0.83.0` / `845d6ff1f6643aba440341cce877ce1c43ebbc39`
- Exact upstream checkout used: `/workspace/tmp/pi-v0840`
- Exact previous checkout used: `/workspace/tmp/pi-v0830-fresh`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/pi-ai-0.84.0-package/package`
- Detailed path matrix: `docs/v0840-release-ledger.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from `v0.83.0` to `v0.84.0`, no unpublished `main` changes.

Audited scope:

- 101 changed `packages/ai` paths.
- Official tag SHA verified: `a5f43bf8aff3c55752432655f7334e3dafd1e256`.
- Material upstream deltas: Baseten provider, advanced sampling params, vLLM thinking-token budget, missing-finish compatibility, Anthropic initial `content_block_start` assembly, Responses incomplete detail mapping, Bedrock diagnostics, model/image catalog refresh, runtime publication/auth refresh changes, and JS-only monorepo protocol changes.

## Go implementation and decisions

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
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.0-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0840/packages/ai/src/providers
upstream pairs: 1153
generated pairs: 1153
model provider/id pairs match exactly

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0840/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0840.go
wrote /workspace/tmp/images_v0840.go with 42 image models
```




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

## Validation evidence

Focused validation already run during implementation:

```text
go test ./...
```

Final full gate passed before commit/push:

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
