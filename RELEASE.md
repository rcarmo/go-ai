# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.2`
- Upstream tag/SHA: `914cf1472e715297caa30db4b9535d534a9eb718`
- Published: 2026-08-14T10:14:32Z
- Previous accepted baseline: `v0.84.1` / `53fa77ccd8a279eb87e92294ef3687b03ff80112`
- Exact upstream checkout used: `/workspace/tmp/pi-v0842`
- Exact previous checkout used: v0.84.1 tag in `/workspace/tmp/pi-v0842` plus accepted local baseline commit `870cc2f628275ceb8020ee50091b40344ae1a28f`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/v0842/npm0842/package`
- Previous official npm data artifact: `/workspace/tmp/v0842/npm0841/package`
- Official npm tarball SHA-256: `0262785a76b0eb2eec596cd8a7ab2ee23eef89d2ef1bb1211c4f0a1944dacf41`
- Detailed path matrix: `docs/v0842-release-ledger.md`
- Whole-corpus test crosswalk: `docs/v0842-131-test-manifest.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from official `v0.84.1` to official `v0.84.2`, no unpublished `main` changes.

Audited scope:

- 42 changed `packages/ai` paths: 18 source, 21 tests, 3 package/docs/generator paths.
- Changed tests: 21 total, 18 modified plus 3 new (`cloudflare-gateway-binding.test.ts`, `mistral-http-transport.test.ts`, `openai-responses-namespace.test.ts`).
- Whole test corpus: 131 `packages/ai/test/*.test.ts` files, fully classified with 0 unclassified rows.
- Text model catalog: 1267 models across 39 providers, exact provider/id pair parity with upstream.
- Image model catalog: 45 OpenRouter image models, exact regeneration from upstream `image-models.generated.ts`.
- Material runtime deltas: strict JSON-schema tool conversion, optional non-nullable null omission, Responses namespace replay and `additional_tools`, Codex `end_turn` plus browser-safe `pi (...)` user agent, Bedrock schema/input sanitization, Google stop/toolUse mapping, Mistral exact HTTP/SSE wire contract, Copilot OAuth policy concurrency cap, retry buffer-limit classification, and Cloudflare Workers binding N/A rationale.

## v0.84.2 Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| Strict JSON-schema tool conversion | Implemented in `schema_strict.go`. Strict conversion clones schema, rejects unsupported constructs (`$ref`, `$defs`, `allOf`, `oneOf`, schema-valued/true `additionalProperties`, structured unions, tuple schemas, etc.), makes object properties required, adds `additionalProperties:false`, and widens optional non-nullable properties to `anyOf[..., {type:"null"}]`. Tests: `schema_strict_test.go`, provider strict-schema tests. |
| Optional non-nullable null omission | Implemented in `ValidateToolArguments`: nulls on optional non-nullable properties are deleted before coercion/validation, including nested objects; nullable/reference nulls are preserved. Tests: `upstream_validation_v0842_test.go`. |
| Provider strict schema propagation | Implemented/adapted for OpenAI Completions, OpenAI Responses, Codex/Responses tool conversion path, Anthropic, Bedrock, Google, and Mistral. Tests: `openai/v0842_strict_schema_test.go`, `mistral/v0842_strict_schema_test.go`, `mistral/v0842_wire_contract_test.go`, `bedrock/v0842_sanitization_test.go`, existing constrained sampling tests. |
| OpenAI Responses namespace | Implemented. `ToolCall`/`ContentBlock` now include `Namespace`; streamed `output_item.done` namespace is persisted and same-model replay emits it. Test: `openairesponses/v0842_namespace_additional_tools_test.go`. |
| OpenAI Responses `additional_tools` | Implemented. `SupportsAdditionalTools` compat is generated, deferred tool planning now emits message-anchored `additional_tools` instead of `tool_search_*` where supported; tool-search fallback retained. Test: `openairesponses/v0842_namespace_additional_tools_test.go`. |
| Codex `end_turn` and user-agent | Implemented. Codex SSE/WS terminal responses preserve `Message.EndTurn`; Codex headers use shared `pi (...)` shape instead of `go-ai (...)`. Test: `openaicodex/v0842_endturn_useragent_test.go`. |
| Bedrock tool input/schema sanitization | Implemented. Bedrock tool-use replay strips empty object keys recursively, and strict provider schema conversion feeds tool input schemas when supported. Test: `bedrock/v0842_sanitization_test.go`. |
| Google strict tool schema / stop mapping | Implemented. Google tools receive strict-converted JSON schemas for Gemini 3+ strict sampling, and tool calls only upgrade stop reason to `toolUse` when the provider stop reason maps to `stop`, not for error finish reasons. Test: `raw_stop_reason_upstream_test.go`. |
| Mistral HTTP transport rewrite | Implemented/adapted. Go already used direct HTTP/SSE rather than the TS SDK; v0.84.2 Go work proves exact wire behavior for `x-affinity`, prompt cache key, byte-split UTF-8, abort/timeout while awaiting chunks, bounded 403 JSON body, retry replayability, usage cache-read mapping, replayed messages, and tool fields. JS fetch injection/camelCase SDK remapping is N/A to Go. |
| Cloudflare Workers AI Gateway binding | N/A/JS-Workers-binding. Upstream adds a Workers `env.AI.gateway().run()` fetch shim; Go has no Workers binding/fetch abstraction. Existing Cloudflare HTTPS gateway base URL/auth/placeholder behavior remains covered; ledger records the precise N/A boundary. |
| GitHub Copilot OAuth policy enablement | Implemented/adapted. Go policy enablement now batches model policy calls at concurrency 4, matching upstream throttling intent; live GitHub policy endpoint remains credential/network-bound. |
| Retry classification | Implemented. `exceeded request buffer limit while retrying upstream` is classified retryable. Test: `retry_assistant_test.go`. |
| Text catalog refresh | Implemented mechanically. `models_generated.go` regenerated from exact v0.84.2 source and official npm provider shards. `compare-upstream-models.py`: 1267/1267 exact. Portable full metadata gate passes from local exact inputs and empty self-fetch cache. |
| Image catalog refresh | Implemented mechanically. `images/models_generated.go` regenerated from exact v0.84.2 `image-models.generated.ts`: 45 image models. `scripts/check-model-regeneration.sh` now also verifies image catalog regeneration. |
| Upstream test corpus | Updated. `docs/v0842-131-test-manifest.md` has 131 rows, 103 deterministic/covered, 28 N/A/adapted, 0 unclassified. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data \
  python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0842/packages/ai/src/providers
upstream pairs: 1267
generated pairs: 1267
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/models.generated.ts \
PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/image-models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data \
TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed

unset PI_AI_MODELS_GENERATED_TS PI_AI_IMAGE_MODELS_GENERATED_TS PI_AI_MODEL_DATA_DIR
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-v0842-gate-cache TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed

text pairs: 1267 / 1267, providers: 39
image pairs: 45, providers: 1
```


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

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0841/packages/ai/src/models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data \
./scripts/check-model-regeneration.sh
model regeneration metadata comparator passed

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841.go
wrote /workspace/tmp/images_v0841.go with 42 image models
# diff against images/models_generated.go: exact

0.84.0 -> 0.84.1 text artifact diff: 1153/38 -> 1220/39, 70 added, 3 removed, 9 metadata changed.
Pair equality is verified by `scripts/compare-upstream-models.py`; full Go-representable metadata equality is verified by normalized regeneration diff in `scripts/check-model-regeneration.sh`.
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
docs/v0841-128-test-manifest.md row check: 128 rows, 101 deterministic/covered, 27 N/A/adapted, 0 unclassified
TMPDIR=/workspace/tmp go test ./... -run 'QwenTokenPlan|QwenTokenPlanIndividual|RegisterBuiltinModels|OpenAICompletionsEmptyTools'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data scripts/compare-upstream-models.py /workspace/tmp/pi-v0841/packages/ai/src/providers
# upstream pairs: 1220
# generated pairs: 1220
# model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0841/packages/ai/src/models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data ./scripts/check-model-regeneration.sh
# model regeneration metadata comparator passed

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841_gate.go
# wrote /workspace/tmp/images_v0841_gate.go with 42 image models
# diff against images/models_generated.go: exact

make check  # includes check-model-regeneration
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Portability/fault-injection proof:

```text
# Clean worktree copy with no PI_AI_* overrides and no pre-existing fixtures:
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-clean-cache-wt make check GO_TMPDIR=/tmp
# check-model-regeneration fetched exact tag/package into the cache and passed

# Fault injection in clean worktree copy:
# openrouter/google/gemini-3-flash-preview MaxTokens 65536 -> 65537
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-clean-cache-fault ./scripts/check-model-regeneration.sh
# failed with normalized regeneration diff showing 65537 vs 65536
```

Retained logs: `/workspace/tmp/go-ai-v0841-portable-gates/` and `/workspace/tmp/go-ai-v0841-metadata-gates/`.

Prior v0.84.1 logs retained: `/workspace/tmp/go-ai-v0841-gates/`.



Correction cycle 4 gate evidence:

```text
docs/v0840-127-test-manifest.md row check: 127 rows, 101 deterministic/covered, 26 N/A/adapted, 0 unclassified
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
