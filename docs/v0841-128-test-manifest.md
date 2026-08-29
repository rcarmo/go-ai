# v0.84.1 whole-corpus test disposition manifest

Audit target: official upstream `github.com/earendil-works/pi` / `@earendil-works/pi-ai` tag `v0.84.1`, SHA `53fa77ccd8a279eb87e92294ef3687b03ff80112`.

Source enumerated: `/workspace/tmp/pi-v0841/packages/ai/test/*.test.ts`.

Command:

```text
find /workspace/tmp/pi-v0841/packages/ai/test -name '*.test.ts' | sort
```

## Summary

- Upstream test files: **128**
- DETERMINISTIC-PORTED / DETERMINISTIC-PORTED-ADAPTED / covered by deterministic local tests: **101**
- N/A credential/live/JS-runtime/generator-policy-adapted only: **27**
- TODO / needs classification: **0**
- Achieved (`DETERMINISTIC + N/A`): **128 / 128**

The five credential-gated E2E files newly called out by the whole-corpus audit are explicitly classified **N/A/live-only** and are not represented as passing tests:

- `test/anthropic-eager-tool-input-e2e.test.ts`
- `test/anthropic-long-cache-retention-e2e.test.ts`
- `test/openai-codex-cache-affinity-e2e.test.ts`
- `test/openai-responses-cache-affinity-e2e.test.ts`
- `test/openai-responses-reasoning-replay-e2e.test.ts`

## Manifest

| # | Upstream test file | Disposition | Local Go evidence / N/A reason |
| ---: | --- | --- | --- |
| 1 | `test/abort.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live abort matrix. Go uses `context.Context` cancellation and deterministic cancellation tests (`TestRetryAssistantCallAbortedBackoffReturnsAbortedAndUnsuccessful`, OAuth context tests), but this file is not a portable wire fixture. |
| 2 | `test/anthropic-adaptive-thinking-models.test.ts` | DETERMINISTIC-PORTED | `TestAnthropicAdaptiveThinkingModels` covers generated adaptive-thinking metadata and regex classification. |
| 3 | `test/anthropic-auth-token.test.ts` | DETERMINISTIC-PORTED/covered | Anthropic auth/header precedence is covered by Anthropic provider request/auth tests and header suppression tests. |
| 4 | `test/anthropic-cache-write-1h-cost.test.ts` | DETERMINISTIC-PORTED | `TestAnthropicCacheWrite1hCost*` in `inference/provider/anthropic/anthropic_cache_write_1h_cost_test.go`. |
| 5 | `test/anthropic-eager-tool-input-compat.test.ts` | DETERMINISTIC-PORTED | `TestAnthropicEagerToolInputCompatSendsPerToolEagerInputStreamingByDefault`, legacy-beta, and no-tools cases. |
| 6 | `test/anthropic-eager-tool-input-e2e.test.ts` | N/A/live-only | Requires live Anthropic credentials/networked service. Do not count as passing; deterministic eager-input request shape is covered by the compat tests. |
| 7 | `test/anthropic-empty-thinking-signature-compat.test.ts` | DETERMINISTIC-PORTED | Empty-signature replay cases in Anthropic request-compat tests. |
| 8 | `test/anthropic-force-adaptive-thinking.test.ts` | DETERMINISTIC-PORTED | `anthropic_force_adaptive_thinking_test.go` force/adaptive thinking matrix. |
| 9 | `test/anthropic-long-cache-retention-e2e.test.ts` | N/A/live-only | Requires live Anthropic credentials/networked 1h cache service. Do not count as passing; deterministic cache-control TTL request shape is covered by cache-retention tests. |
| 10 | `test/anthropic-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Context-aware refresh/cancellation tests cover Go OAuth network behavior; JS credential-store/browser prompt UI is N/A to Go. |
| 11 | `test/anthropic-opus-4-8-smoke.test.ts` | N/A/live-only | Live Anthropic smoke test requiring credentials. Catalog/thinking metadata is covered deterministically. |
| 12 | `test/anthropic-sse-parsing.test.ts` | DETERMINISTIC-PORTED | Anthropic SSE parser, refusal/no-usage, tool JSON repair, and initial content/signature tests. |
| 13 | `test/anthropic-temperature-compat.test.ts` | DETERMINISTIC-PORTED | `anthropic_temperature_compat_test.go` temperature omission/preservation matrix. |
| 14 | `test/anthropic-thinking-disable.test.ts` | DETERMINISTIC-PORTED | `anthropic_thinking_disable_test.go` thinking-off/adaptive output effort matrix. |
| 15 | `test/anthropic-tool-name-normalization.test.ts` | DETERMINISTIC-PORTED | `anthropic_tool_name_normalization_test.go`. |
| 16 | `test/azure-openai-base-url.test.ts` | DETERMINISTIC-PORTED | `azure_openai_base_url_test.go` URL/deployment/version/cache/store cases. |
| 17 | `test/azure-openai-responses-reasoning-replay.test.ts` | DETERMINISTIC-PORTED | `azure_reasoning_replay_upstream_test.go`. |
| 18 | `test/baseten-models.test.ts` | DETERMINISTIC-PORTED | `openai_v0840_test.go` Baseten metadata/env/payload behavior. |
| 19 | `test/bedrock-convert-messages.test.ts` | DETERMINISTIC-PORTED | `bedrock_convert_messages_upstream_test.go`. |
| 20 | `test/bedrock-credentials.test.ts` | DETERMINISTIC-PORTED/covered | Bedrock credential/profile precedence tests cover explicit/scoped/ambient resolution. |
| 21 | `test/bedrock-custom-headers.test.ts` | DETERMINISTIC-PORTED | `TestApplyCustomHeaders*` in Bedrock tests. |
| 22 | `test/bedrock-endpoint-resolution.test.ts` | DETERMINISTIC-PORTED | `bedrock_endpoint_resolution_upstream_test.go`. |
| 23 | `test/bedrock-error-metadata.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `TestProcessConverseStreamAddsFailureDiagnosticForStreamErr` and diagnostic metadata matrix. |
| 24 | `test/bedrock-models.test.ts` | N/A/live-provider | AWS credential/region availability matrix; deterministic Bedrock catalog/metadata and request-shape tests exist, but live availability is not simulated. |
| 25 | `test/bedrock-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestProcessConverseStreamPreservesRawStopReason`. |
| 26 | `test/bedrock-thinking-payload.test.ts` | DETERMINISTIC-PORTED | `bedrock_thinking_payload_upstream_test.go`. |
| 27 | `test/cache-retention.test.ts` | DETERMINISTIC-PORTED | Anthropic cache-retention tests plus Responses/OpenAI cache tests. |
| 28 | `test/cloudflare-stream.test.ts` | DETERMINISTIC-PORTED | `TestResolveCloudflareBaseURLPreservesUnresolvedPlaceholders`, `TestCloudflareBaseURLResolvedAndUnresolvedThroughDispatch`. |
| 29 | `test/compat-env.test.ts` | N/A/JS-runtime | JS legacy registry/env compatibility surface (`registerApiProvider`/module runtime); Go uses typed provider registration. |
| 30 | `test/constrained-sampling.test.ts` | DETERMINISTIC-PORTED | `TestConstrainedSamplingConvertsStrictAndGrammarTools`, rejection matrix. |
| 31 | `test/context-estimate.test.ts` | DETERMINISTIC-PORTED | `tests/estimate_upstream_test.go`, `TestUpstreamV0806ContextEstimateIgnoresUsageBeforeNewerPrefixMessage`. |
| 32 | `test/context-overflow.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Existing deterministic overflow fixtures remain ported in `tests/context_overflow_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live-provider addition requires credentials and is N/A/live-only. |
| 33 | `test/cross-provider-handoff.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual cases to a live cross-provider handoff matrix requiring credentials/network. Deterministic replay/handoff transforms are covered locally. |
| 34 | `test/deferred-tools.test.ts` | DETERMINISTIC-PORTED | `tests/deferred_tools_upstream_test.go` and provider deferred-tool tests. |
| 35 | `test/empty.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live empty-message matrix. Deterministic empty tool-result/request behavior is covered locally. |
| 36 | `test/env-api-keys.test.ts` | DETERMINISTIC-PORTED | `tests/env_api_keys_test.go`. |
| 37 | `test/error-body.test.ts` | DETERMINISTIC-PORTED | `tests/error_body_test.go`. |
| 38 | `test/faux-provider.test.ts` | DETERMINISTIC-PORTED | `inference/provider/faux/faux_test.go`. |
| 39 | `test/fetch-option.test.ts` | N/A/JS-runtime | Custom `fetch` injection into JS SDK adapters has no Go public API analogue; Go uses `http.Client`/retry transport hooks. |
| 40 | `test/fireworks-models.test.ts` | DETERMINISTIC-PORTED | Fireworks catalog/env/compat assertions via `tests/models_catalog_upstream_test.go`. |
| 41 | `test/generate-models-strict.test.ts` | N/A/adapted-generator-policy | Upstream exercises the private TS generator `--strict` rollback/failure path for Qwen Token Plan Individual allowlist mismatch. Go generator is a separate artifact consumer; final release artifacts are verified by exact source/package comparator (1220/1220) and no image diff, while helper rollback policy is documented as N/A/adapted rather than claimed as passing. |
| 42 | `test/github-copilot-anthropic.test.ts` | DETERMINISTIC-PORTED | `github_copilot_anthropic_test.go`. |
| 43 | `test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Go Copilot OAuth/token/header/model filtering tests; live GitHub policy UI remains N/A. |
| 44 | `test/google-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestGoogleRawStopReason`. |
| 45 | `test/google-shared-convert-tools.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go`. |
| 46 | `test/google-shared-gemini3-unsigned-tool-call.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go` Gemini 3 unsigned-tool-call cases. |
| 47 | `test/google-shared-image-tool-result-routing.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go` image tool-result routing cases. |
| 48 | `test/google-shared-retry.test.ts` | N/A/JS-SDK | TS SDK retry wrapper behavior; Go uses HTTP/context retry helpers and deterministic retry tests. |
| 49 | `test/google-shared-signed-empty-blocks.test.ts` | N/A/JS-SDK | Google SDK signed empty-block serialization detail; Go signed/thinking/tool behavior covered where applicable. |
| 50 | `test/google-thinking-disable.test.ts` | DETERMINISTIC-PORTED | `google_thinking_disable_upstream_test.go`. |
| 51 | `test/google-thinking-signature.test.ts` | DETERMINISTIC-PORTED | `google_thinking_signature_test.go`. |
| 52 | `test/google-vertex-api-key-resolution.test.ts` | DETERMINISTIC-PORTED | `google_vertex_api_key_resolution_test.go`. |
| 53 | `test/image-model-data.test.ts` | N/A/adapted-generator-policy | Upstream tests private TS generator helper malformed-input paths (`parseOpenRouterImageModels`) that are not a Go public/runtime API. Go consumes exact generated image artifacts and verifies them separately via `scripts/generate-image-models.py` plus exact 42/42 comparator and `tests/images_test.go`; do not label as a test-for-test port. |
| 54 | `test/image-tool-result.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live image tool-result matrix; deterministic serialization/routing remains covered by OpenAI/Responses/Google image tool-result tests. |
| 55 | `test/images-models.test.ts` | DETERMINISTIC-PORTED | `tests/images_test.go`, `tests/images_openrouter_upstream_test.go`. |
| 56 | `test/images.test.ts` | N/A/live-provider | Requires live image provider credentials/network. Deterministic OpenRouter image wrapper tests exist. |
| 57 | `test/interleaved-thinking.test.ts` | N/A/live-provider | Requires live provider credentials/network; deterministic thinking replay/serialization tests cover local behavior. |
| 58 | `test/kimi-coding-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Kimi OAuth/device/refresh tests; JS prompt/auth operation details are N/A. |
| 59 | `test/lax-message-content.test.ts` | DETERMINISTIC-PORTED | `tests/lax_message_content_upstream_test.go`. |
| 60 | `test/lazy-module-load.test.ts` | N/A/JS-runtime | JS lazy module loading/bundling; Go links/imports packages statically. |
| 61 | `test/max-thinking.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go`, thinking-level clamp tests, and Codex request tests. |
| 62 | `test/mistral-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestMistralRawStopReason`. |
| 63 | `test/mistral-reasoning-mode.test.ts` | DETERMINISTIC-PORTED | `mistral_reasoning_mode_test.go`. |
| 64 | `test/mistral-tool-schema.test.ts` | DETERMINISTIC-PORTED | `mistral_tool_schema_test.go`. |
| 65 | `test/model-catalog-types.test.ts` | DETERMINISTIC-PORTED | Generated catalog compile/type checks and exact model comparator. |
| 66 | `test/model-data-validation.test.ts` | N/A/adapted-generator-policy | v0.84.1 adds exact allowlist helper assertions for `qwen-token-plan-individual` plus private TS model-data shard validation helper failures. Go does not expose these TS helper APIs; final generated artifacts are verified by exact 1220/1220 comparator, 128-file manifest accounting, and Qwen Individual catalog tests. |
| 67 | `test/models-runtime.test.ts` | DETERMINISTIC-PORTED | `tests/models_runtime_test.go`. |
| 68 | `test/node-http-proxy.test.ts` | DETERMINISTIC-PORTED | `tests/retry_proxy_test.go`. |
| 69 | `test/oauth-auth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `GetAPIKeyWithContext` / `RuntimeForProviderContext` cancellation/cause tests; JS credential-store UI N/A. |
| 70 | `test/oauth-device-code.test.ts` | DETERMINISTIC-PORTED | `oauth_device_code_upstream_test.go`. |
| 71 | `test/openai-codex-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live Codex credentials/network. Do not count as passing; deterministic Codex cache-affinity headers are covered locally. |
| 72 | `test/openai-codex-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Codex OAuth refresh/context/header tests. |
| 73 | `test/openai-codex-stream.test.ts` | DETERMINISTIC-PORTED | Codex SSE/WebSocket tests under `inference/provider/openaicodex`. |
| 74 | `test/openai-completions-cache-control-format.test.ts` | DETERMINISTIC-PORTED | `openai_cache_control_upstream_test.go`. |
| 75 | `test/openai-completions-empty-tools.test.ts` | DETERMINISTIC-PORTED | `openai_completions_empty_tools_upstream_test.go`. |
| 76 | `test/openai-completions-prompt-cache.test.ts` | DETERMINISTIC-PORTED | `openai_completions_prompt_cache_test.go`. |
| 77 | `test/openai-completions-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestOpenAICompletionsRawStopReason`. |
| 78 | `test/openai-completions-reasoning-details.test.ts` | DETERMINISTIC-PORTED | `openai_reasoning_details_upstream_test.go`. |
| 79 | `test/openai-completions-response-model.test.ts` | DETERMINISTIC-PORTED | `openai_completions_response_model_test.go`. |
| 80 | `test/openai-completions-retry.test.ts` | N/A/JS-SDK | Upstream asserts JS SDK `maxRetries`; Go has `RetryConfig`/transport retry tests instead. |
| 81 | `test/openai-completions-thinking-as-text.test.ts` | DETERMINISTIC-PORTED | `openai_completions_thinking_as_text_test.go`. |
| 82 | `test/openai-completions-thinking-token-budget.test.ts` | DETERMINISTIC-PORTED | `TestOpenAIThinkingTokenBudgetUpstreamMatrix`. |
| 83 | `test/openai-completions-tool-choice.test.ts` | DETERMINISTIC-PORTED | OpenAI completions tool-choice/tool-call provider tests. |
| 84 | `test/openai-completions-tool-result-images.test.ts` | DETERMINISTIC-PORTED | OpenAI compatible image tool-result tests. |
| 85 | `test/openai-responses-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic affinity behavior is now covered by `TestOpenAIResponsesCompatSessionAffinityFormats`. |
| 86 | `test/openai-responses-compat.test.ts` | DETERMINISTIC-PORTED | `responses_compat_upstream_test.go`: session-affinity formats, cacheRetention none, explicit header override, required tool choice, service-tier costs, off→none matrix, xAI include. |
| 87 | `test/openai-responses-empty-tool-result.test.ts` | DETERMINISTIC-PORTED | `responses_empty_tool_result_upstream_test.go`. |
| 88 | `test/openai-responses-foreign-toolcall-id.test.ts` | DETERMINISTIC-PORTED | `responses_foreign_toolcall_id_test.go`. |
| 89 | `test/openai-responses-message-id.test.ts` | DETERMINISTIC-PORTED | `responses_message_id_test.go`. |
| 90 | `test/openai-responses-partial-json-cleanup.test.ts` | DETERMINISTIC-PORTED | `responses_partial_json_cleanup_test.go`. |
| 91 | `test/openai-responses-reasoning-replay-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic replay is covered by `azure_reasoning_replay_upstream_test.go` and Responses replay tests. |
| 92 | `test/openai-responses-terminal-event.test.ts` | DETERMINISTIC-PORTED | `responses_v0840_test.go`, `raw_status_upstream_test.go`. |
| 93 | `test/openai-responses-tool-result-images.test.ts` | DETERMINISTIC-PORTED | Responses image tool-result tests. |
| 94 | `test/openrouter-cache-control-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_catalog_upstream_test.go` and OpenRouter cache-control model metadata tests. |
| 95 | `test/openrouter-cache-write-repro.test.ts` | DETERMINISTIC-PORTED | `openrouter_cache_write_repro_upstream_test.go`. |
| 96 | `test/openrouter-images.test.ts` | DETERMINISTIC-PORTED | `tests/images_openrouter_upstream_test.go`. |
| 97 | `test/openrouter-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | OpenRouter OAuth/key exchange/context tests. |
| 98 | `test/overflow.test.ts` | DETERMINISTIC-PORTED | `tests/overflow_upstream_test.go`. |
| 99 | `test/pi-messages.test.ts` | DETERMINISTIC-PORTED | `inference/provider/pimessages/pimessages_test.go`. |
| 100 | `test/provider-error-body-passthrough.test.ts` | DETERMINISTIC-PORTED | `tests/provider_error_body_test.go`. |
| 101 | `test/provider-error-body-regression.test.ts` | DETERMINISTIC-PORTED | `tests/provider_error_body_test.go`. |
| 102 | `test/provider-retry.test.ts` | DETERMINISTIC-PORTED | `tests/provider_retry_test.go`. |
| 103 | `test/providers.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Registry/env/provider/runtime/faux deferred tests; JS instance helper surfaces mapped to Go registries. |
| 104 | `test/qwen-token-plan-models.test.ts` | DETERMINISTIC-PORTED | `tests/qwen_token_plan_upstream_test.go` verifies `qwen-token-plan-individual` exact seven-model allowlist, international endpoint/API metadata, env-key reuse, image-model omission, retired preview omission, and shared Token Plan model exposure; `qwen_token_plan_individual_upstream_test.go` verifies Qwen `enable_thinking` / `reasoning_effort` request fields. |
| 105 | `test/radius-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/radius_test.go` plus context refresh tests. |
| 106 | `test/reasoning-options.test.ts` | N/A/adapted-generator-policy | Upstream tests a TS generator helper (`getEffortThinkingLevelMap`) for model-data reasoning-control policy. Go verifies the resulting generated thinking maps and provider request behavior through catalog tests and reasoning/payload tests; do not label script-helper policy assertions as a test-for-test port. |
| 107 | `test/responseid.test.ts` | DETERMINISTIC-PORTED | `tests/responseid_simulated_test.go`. |
| 108 | `test/retry.test.ts` | DETERMINISTIC-PORTED | `tests/retry_assistant_test.go`, `tests/v0805_inplace_deltas_test.go`. |
| 109 | `test/sampling-options.test.ts` | DETERMINISTIC-PORTED | `openai_v0840_test.go`, `responses_v0840_test.go` sampling matrices. |
| 110 | `test/stream.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live stream matrix requiring credentials. Deterministic OpenAI-compatible request/stream behavior is covered by local provider tests. |
| 111 | `test/supports-xhigh.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go`. |
| 112 | `test/telemetry-options.test.ts` | DETERMINISTIC-PORTED | `TestTelemetryContextHooks`, faux deferred telemetry tests, OpenRouter image telemetry tests. |
| 113 | `test/text.test.ts` | DETERMINISTIC-PORTED | `TestContentTextExtractsTextBlocks`. |
| 114 | `test/together-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_catalog_upstream_test.go`. |
| 115 | `test/tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated token accounting remains ported in `tests/tokens_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live token-stat case requires credentials and is N/A/live-only. |
| 116 | `test/tool-call-id-normalization.test.ts` | DETERMINISTIC-PORTED | `openai_tool_call_id_normalization_test.go`. |
| 117 | `test/tool-call-without-result.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to a live provider matrix requiring credentials; deterministic missing-tool-result filtering/replay behavior remains covered locally. |
| 118 | `test/total-tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated total-token accounting remains ported in `tests/total_tokens_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live total-token case requires credentials and is N/A/live-only. |
| 119 | `test/transform-messages-copilot-openai-to-anthropic.test.ts` | DETERMINISTIC-PORTED | Anthropic/OpenAI replay transform tests. |
| 120 | `test/unicode-surrogate.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated Unicode/surrogate behavior remains ported in `tests/unicode_surrogate_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live Unicode case requires credentials and is N/A/live-only. |
| 121 | `test/uuid.test.ts` | DETERMINISTIC-PORTED | `TestUUIDv7UsesRFC9562LayoutAndPreservesMonotonicOrder`. |
| 122 | `test/validation.test.ts` | DETERMINISTIC-PORTED | `tests/upstream_validation_test.go`. |
| 123 | `test/xai-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/xai_test.go` plus context refresh tests. |
| 124 | `test/xai-responses.test.ts` | DETERMINISTIC-PORTED | `xai_responses_upstream_test.go` verifies exact low/medium/high-only levels, Responses API catalog routing, bearer auth, developer prompt, `/responses`, `session_id` + `prompt_cache_key`, `store=false`, medium reasoning, encrypted reasoning include, and no long retention. |
| 125 | `test/xhigh.test.ts` | N/A/live-provider | Live OpenAI xhigh test requiring credentials; deterministic xhigh support metadata is covered. |
| 126 | `test/xiaomi-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_catalog_upstream_test.go`. |
| 127 | `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | DETERMINISTIC-PORTED | Xiaomi empty-signature request-shape/catalog tests. |
| 128 | `test/zen.test.ts` | N/A/live-provider | Live Zen/OpenCode request requiring credentials/network. Deterministic OpenCode/Responses compat is covered locally. |
