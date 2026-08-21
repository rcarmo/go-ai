# v0.84.2 whole-corpus test disposition manifest

Audit target: official upstream `github.com/earendil-works/pi` / `@earendil-works/pi-ai` tag `v0.84.2`, SHA `914cf1472e715297caa30db4b9535d534a9eb718`.

Source enumerated: `/workspace/tmp/pi-v0842/packages/ai/test/*.test.ts`.

Command:

```text
find /workspace/tmp/pi-v0842/packages/ai/test -name '*.test.ts' | sort
```

## Summary

- Upstream test files: **131**

- DETERMINISTIC-PORTED / DETERMINISTIC-PORTED-ADAPTED / covered by deterministic local tests: **103**

- N/A credential/live/JS-runtime/generator-policy/Workers-binding adapted only: **28**

- Unclassified upstream test files: **0**

- Achieved (`DETERMINISTIC + N/A`): **131 / 131**

Changed upstream test files in v0.84.2: **21** (18 modified + 3 added). New files: `cloudflare-gateway-binding.test.ts`, `mistral-http-transport.test.ts`, `openai-responses-namespace.test.ts`.

## Manifest

| # | Upstream test file | Disposition | Local Go evidence / N/A reason |
| ---: | --- | --- | --- |
| 1 | `test/abort.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live abort matrix. Go uses `context.Context` cancellation and deterministic cancellation tests (`TestRetryAssistantCallAbortedBackoffReturnsAbortedAndUnsuccessful`, OAuth context tests), but this file is not a portable wire fixture. |
| 2 | `test/anthropic-adaptive-thinking-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 3 | `test/anthropic-auth-token.test.ts` | DETERMINISTIC-PORTED/covered | inference/provider/anthropic/*auth*/request tests; inference/provider/openaicodex/v0842_endturn_useragent_test.go. v0.84.2 adds shared browser-safe `pi (...)` user-agent behavior for Anthropic-compatible Kimi/Codex-like paths; Go provider headers remain deterministic and Codex user-agent shape is covered. v0.84.2 changed. |
| 4 | `test/anthropic-cache-write-1h-cost.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 5 | `test/anthropic-eager-tool-input-compat.test.ts` | DETERMINISTIC-PORTED | inference/provider/anthropic/anthropic_request_compat_test.go; inference/provider/anthropic/anthropic.go. v0.84.2 strict schema conversion is wired into Anthropic tool input schemas while retaining eager-input compat behavior. v0.84.2 changed. |
| 6 | `test/anthropic-eager-tool-input-e2e.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 7 | `test/anthropic-empty-thinking-signature-compat.test.ts` | DETERMINISTIC-PORTED | Empty-signature replay cases in Anthropic request-compat tests. |
| 8 | `test/anthropic-force-adaptive-thinking.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 9 | `test/anthropic-long-cache-retention-e2e.test.ts` | N/A/live-only | Requires live Anthropic credentials/networked 1h cache service. Do not count as passing; deterministic cache-control TTL request shape is covered by cache-retention tests. |
| 10 | `test/anthropic-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 11 | `test/anthropic-opus-4-8-smoke.test.ts` | N/A/live-only | Live Anthropic smoke test requiring credentials. Catalog/thinking metadata is covered deterministically. |
| 12 | `test/anthropic-sse-parsing.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 13 | `test/anthropic-temperature-compat.test.ts` | DETERMINISTIC-PORTED | `anthropic_temperature_compat_test.go` temperature omission/preservation matrix. |
| 14 | `test/anthropic-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 15 | `test/anthropic-tool-name-normalization.test.ts` | DETERMINISTIC-PORTED | `anthropic_tool_name_normalization_test.go`. |
| 16 | `test/azure-openai-base-url.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 17 | `test/azure-openai-responses-reasoning-replay.test.ts` | DETERMINISTIC-PORTED | `azure_reasoning_replay_upstream_test.go`. |
| 18 | `test/baseten-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 19 | `test/bedrock-convert-messages.test.ts` | DETERMINISTIC-PORTED | inference/provider/bedrock/bedrock_convert_messages_upstream_test.go; inference/provider/bedrock/v0842_sanitization_test.go. v0.84.2 empty-key Bedrock document sanitization and converted strict tool schemas are covered; existing conversion cases remain covered. v0.84.2 changed. |
| 20 | `test/bedrock-credentials.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 21 | `test/bedrock-custom-headers.test.ts` | DETERMINISTIC-PORTED | `TestApplyCustomHeaders*` in Bedrock tests. |
| 22 | `test/bedrock-endpoint-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 23 | `test/bedrock-error-metadata.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `TestProcessConverseStreamAddsFailureDiagnosticForStreamErr` and diagnostic metadata matrix. |
| 24 | `test/bedrock-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 25 | `test/bedrock-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestProcessConverseStreamPreservesRawStopReason`. |
| 26 | `test/bedrock-thinking-payload.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 27 | `test/cache-retention.test.ts` | DETERMINISTIC-PORTED | Anthropic cache-retention tests plus Responses/OpenAI cache tests. |
| 28 | `test/cloudflare-gateway-binding.test.ts` | N/A/JS-Workers-binding | —. New TS Workers AI binding fetch shim (`createGatewayBindingFetch`) is a JavaScript/Cloudflare Worker transport adapter around `fetch` and `env.AI.gateway()`. The Go library has no Workers binding/fetch-injection surface; existing Go Cloudflare gateway HTTPS URL/auth/placeholder behavior remains covered by Cloudflare stream/provider tests. v0.84.2 changed. |
| 29 | `test/cloudflare-stream.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 30 | `test/compat-env.test.ts` | N/A/JS-runtime | JS legacy registry/env compatibility surface (`registerApiProvider`/module runtime); Go uses typed provider registration. |
| 31 | `test/constrained-sampling.test.ts` | DETERMINISTIC-PORTED | schema_strict_test.go; inference/provider/openairesponses/constrained_sampling_upstream_test.go; inference/provider/openai/v0842_strict_schema_test.go; inference/provider/mistral/v0842_strict_schema_test.go. v0.84.2 strict JSON Schema conversion implemented: object strictification, optional non-nullable null widening, unsupported schema fallback/reject behavior, and provider parameter conversion. v0.84.2 changed. |
| 32 | `test/context-estimate.test.ts` | DETERMINISTIC-PORTED | `estimate_upstream_test.go`, `TestUpstreamV0806ContextEstimateIgnoresUsageBeforeNewerPrefixMessage`. |
| 33 | `test/context-overflow.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | context_overflow_simulated_test.go. v0.84.2 catalog/live-matrix adjustments retain existing deterministic overflow coverage; credential/live provider additions remain N/A-live. v0.84.2 changed. |
| 34 | `test/cross-provider-handoff.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual cases to a live cross-provider handoff matrix requiring credentials/network. Deterministic replay/handoff transforms are covered locally. |
| 35 | `test/deferred-tools.test.ts` | DETERMINISTIC-PORTED | deferred_tools_upstream_test.go; inference/provider/openairesponses/deferred_tools_upstream_test.go; inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. v0.84.2 Responses `additional_tools` mode and namespace-safe deferred-tool replay are implemented; prior `tool_search` semantics retained. v0.84.2 changed. |
| 36 | `test/empty.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live empty-message matrix. Deterministic empty tool-result/request behavior is covered locally. |
| 37 | `test/env-api-keys.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 38 | `test/error-body.test.ts` | DETERMINISTIC-PORTED | `error_body_test.go`. |
| 39 | `test/faux-provider.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 40 | `test/fetch-option.test.ts` | N/A/JS-runtime | Custom `fetch` injection into JS SDK adapters has no Go public API analogue; Go uses `http.Client`/retry transport hooks. |
| 41 | `test/fireworks-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 42 | `test/generate-models-strict.test.ts` | N/A/adapted-generator-policy | Upstream exercises the private TS generator `--strict` rollback/failure path for Qwen Token Plan Individual allowlist mismatch. Go generator is a separate artifact consumer; final release artifacts are verified by exact source/package comparator (1220/1220) and no image diff, while helper rollback policy is documented as N/A/adapted rather than claimed as passing. |
| 43 | `test/github-copilot-anthropic.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 44 | `test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | oauth/github_copilot.go; oauth/oauth_test.go. v0.84.2 caps GitHub Copilot policy-enable fanout to batches of four; live GitHub policy endpoint remains credential/network-bound, but Go production loop now mirrors the cap. v0.84.2 changed. |
| 45 | `test/google-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/google/google_raw_stop_reason_test.go. v0.84.2 only tightens `toolUse` upgrade to stop-terminal Google tool calls; raw stop reason coverage remains deterministic. v0.84.2 changed. |
| 46 | `test/google-shared-convert-tools.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go`. |
| 47 | `test/google-shared-gemini3-unsigned-tool-call.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 48 | `test/google-shared-image-tool-result-routing.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go` image tool-result routing cases. |
| 49 | `test/google-shared-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 50 | `test/google-shared-signed-empty-blocks.test.ts` | N/A/JS-SDK | Google SDK signed empty-block serialization detail; Go signed/thinking/tool behavior covered where applicable. |
| 51 | `test/google-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 52 | `test/google-thinking-signature.test.ts` | DETERMINISTIC-PORTED | `google_thinking_signature_test.go`. |
| 53 | `test/google-vertex-api-key-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 54 | `test/image-model-data.test.ts` | N/A/adapted-generator-policy | Upstream tests private TS generator helper malformed-input paths (`parseOpenRouterImageModels`) that are not a Go public/runtime API. Go consumes exact generated image artifacts and verifies them separately via `scripts/generate-image-models.py` plus exact 42/42 comparator and `images_test.go`; do not label as a test-for-test port. |
| 55 | `test/image-tool-result.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 56 | `test/images-models.test.ts` | DETERMINISTIC-PORTED | `images_test.go`, `images_openrouter_upstream_test.go`. |
| 57 | `test/images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 58 | `test/interleaved-thinking.test.ts` | N/A/live-provider | Requires live provider credentials/network; deterministic thinking replay/serialization tests cover local behavior. |
| 59 | `test/kimi-coding-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 60 | `test/lax-message-content.test.ts` | DETERMINISTIC-PORTED | `lax_message_content_upstream_test.go`. |
| 61 | `test/lazy-module-load.test.ts` | N/A/JS-runtime | —. JS lazy module load import-count change has no Go analogue; providers are statically linked/registered. v0.84.2 changed. |
| 62 | `test/max-thinking.test.ts` | DETERMINISTIC-PORTED | `supports_xhigh_upstream_test.go`, thinking-level clamp tests, and Codex request tests. |
| 63 | `test/mistral-http-transport.test.ts` | DETERMINISTIC-PORTED/ADAPTED | inference/provider/mistral/*_test.go; inference/provider/mistral/v0842_strict_schema_test.go. Go Mistral already uses direct HTTP/SSE rather than the TS SDK. Existing request/retry/reasoning/raw-stop tests plus v0.84.2 strict schema conversion cover the Go-facing HTTP transport behavior; JS `fetch` injection and camelCase remapping are N/A to Go. v0.84.2 changed. |
| 64 | `test/mistral-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/mistral/mistral_raw_stop_reason_test.go. Existing Mistral raw stop reason fixture remains passing after v0.84.2 direct HTTP transport/catalog refresh. v0.84.2 changed. |
| 65 | `test/mistral-reasoning-mode.test.ts` | DETERMINISTIC-PORTED | `mistral_reasoning_mode_test.go`. |
| 66 | `test/mistral-tool-schema.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 67 | `test/model-catalog-types.test.ts` | DETERMINISTIC-PORTED | Generated catalog compile/type checks and exact model comparator. |
| 68 | `test/model-data-validation.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 69 | `test/models-runtime.test.ts` | DETERMINISTIC-PORTED | `models_runtime_test.go`. |
| 70 | `test/node-http-proxy.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 71 | `test/oauth-auth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `GetAPIKeyWithContext` / `RuntimeForProviderContext` cancellation/cause tests; JS credential-store UI N/A. |
| 72 | `test/oauth-device-code.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 73 | `test/openai-codex-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live Codex credentials/network. Do not count as passing; deterministic Codex cache-affinity headers are covered locally. |
| 74 | `test/openai-codex-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 75 | `test/openai-codex-stream.test.ts` | DETERMINISTIC-PORTED | inference/provider/openaicodex/*_test.go; inference/provider/openaicodex/v0842_endturn_useragent_test.go. v0.84.2 Codex `end_turn` capture and shared `pi (...)` user-agent shape are ported; existing SSE/WS/cache/zstd tests retained. v0.84.2 changed. |
| 76 | `test/openai-completions-cache-control-format.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 77 | `test/openai-completions-empty-tools.test.ts` | DETERMINISTIC-PORTED | `openai_completions_empty_tools_upstream_test.go`. |
| 78 | `test/openai-completions-prompt-cache.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 79 | `test/openai-completions-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestOpenAICompletionsRawStopReason`. |
| 80 | `test/openai-completions-reasoning-details.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 81 | `test/openai-completions-response-model.test.ts` | DETERMINISTIC-PORTED | `openai_completions_response_model_test.go`. |
| 82 | `test/openai-completions-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 83 | `test/openai-completions-thinking-as-text.test.ts` | DETERMINISTIC-PORTED | `openai_completions_thinking_as_text_test.go`. |
| 84 | `test/openai-completions-thinking-token-budget.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 85 | `test/openai-completions-tool-choice.test.ts` | DETERMINISTIC-PORTED | inference/provider/openai/*tool*test.go; inference/provider/openai/v0842_strict_schema_test.go. v0.84.2 OpenAI-compatible tool parameter conversion uses strict provider schemas for strict JSON-schema tools. v0.84.2 changed. |
| 86 | `test/openai-completions-tool-result-images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 87 | `test/openai-responses-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic affinity behavior is now covered by `TestOpenAIResponsesCompatSessionAffinityFormats`. |
| 88 | `test/openai-responses-compat.test.ts` | DETERMINISTIC-PORTED | inference/provider/openairesponses/responses_compat_upstream_test.go; inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. v0.84.2 Cloudflare strict-mode explicit false/true behavior and additional tool mode are covered by request-shape tests. v0.84.2 changed. |
| 89 | `test/openai-responses-empty-tool-result.test.ts` | DETERMINISTIC-PORTED | `responses_empty_tool_result_upstream_test.go`. |
| 90 | `test/openai-responses-foreign-toolcall-id.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 91 | `test/openai-responses-message-id.test.ts` | DETERMINISTIC-PORTED | `responses_message_id_test.go`. |
| 92 | `test/openai-responses-namespace.test.ts` | DETERMINISTIC-PORTED | inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. New v0.84.2 namespace round-trip coverage: stream `namespace` from `output_item.done`, persist in `ContentBlock`/`ToolCall`, and replay only same-model namespace. v0.84.2 changed. |
| 93 | `test/openai-responses-partial-json-cleanup.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 94 | `test/openai-responses-reasoning-replay-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic replay is covered by `azure_reasoning_replay_upstream_test.go` and Responses replay tests. |
| 95 | `test/openai-responses-terminal-event.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 96 | `test/openai-responses-tool-result-images.test.ts` | DETERMINISTIC-PORTED | Responses image tool-result tests. |
| 97 | `test/openrouter-cache-control-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 98 | `test/openrouter-cache-write-repro.test.ts` | DETERMINISTIC-PORTED | `openrouter_cache_write_repro_upstream_test.go`. |
| 99 | `test/openrouter-images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 100 | `test/openrouter-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | OpenRouter OAuth/key exchange/context tests. |
| 101 | `test/overflow.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 102 | `test/pi-messages.test.ts` | DETERMINISTIC-PORTED | `inference/provider/pimessages/pimessages_test.go`. |
| 103 | `test/provider-error-body-passthrough.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 104 | `test/provider-error-body-regression.test.ts` | DETERMINISTIC-PORTED | `provider_error_body_test.go`. |
| 105 | `test/provider-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 106 | `test/providers.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Registry/env/provider/runtime/faux deferred tests; JS instance helper surfaces mapped to Go registries. |
| 107 | `test/qwen-token-plan-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 108 | `test/radius-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/radius_test.go` plus context refresh tests. |
| 109 | `test/reasoning-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 110 | `test/responseid.test.ts` | DETERMINISTIC-PORTED | `responseid_simulated_test.go`. |
| 111 | `test/retry.test.ts` | DETERMINISTIC-PORTED | retry_assistant.go; retry_assistant_test.go. v0.84.2 retryable provider wording `exceeded request buffer limit while retrying upstream` is classified retryable. v0.84.2 changed. |
| 112 | `test/sampling-options.test.ts` | DETERMINISTIC-PORTED | `openai_v0840_test.go`, `responses_v0840_test.go` sampling matrices. |
| 113 | `test/stream.test.ts` | N/A/live-provider | —. v0.84.2 live stream matrix update requires provider credentials/network; deterministic streaming fixtures remain covered locally. v0.84.2 changed. |
| 114 | `test/supports-xhigh.test.ts` | DETERMINISTIC-PORTED | supports_xhigh_upstream_test.go; models_test.go. v0.84.2 catalog thinking-level changes adopted, including DeepSeek V4 Flash low/high/max/off and refreshed generated metadata. v0.84.2 changed. |
| 115 | `test/telemetry-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 116 | `test/text.test.ts` | DETERMINISTIC-PORTED | `TestContentTextExtractsTextBlocks`. |
| 117 | `test/together-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 118 | `test/tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated token accounting remains ported in `tokens_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live token-stat case requires credentials and is N/A/live-only. |
| 119 | `test/tool-call-id-normalization.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 120 | `test/tool-call-without-result.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to a live provider matrix requiring credentials; deterministic missing-tool-result filtering/replay behavior remains covered locally. |
| 121 | `test/total-tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | total_tokens_simulated_test.go; models_test.go. v0.84.2 catalog/live matrix update retains deterministic token-accounting fixtures; credential/live additions remain N/A-live. v0.84.2 changed. |
| 122 | `test/transform-messages-copilot-openai-to-anthropic.test.ts` | DETERMINISTIC-PORTED | Anthropic/OpenAI replay transform tests. |
| 123 | `test/unicode-surrogate.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 124 | `test/uuid.test.ts` | DETERMINISTIC-PORTED | `TestUUIDv7UsesRFC9562LayoutAndPreservesMonotonicOrder`. |
| 125 | `test/validation.test.ts` | DETERMINISTIC-PORTED | upstream_validation_v0842_test.go; context.go. v0.84.2 optional non-nullable `null` is treated as omission before validation while nullable/reference nulls are preserved. v0.84.2 changed. |
| 126 | `test/xai-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/xai_test.go` plus context refresh tests. |
| 127 | `test/xai-responses.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 128 | `test/xhigh.test.ts` | N/A/live-provider | Live OpenAI xhigh test requiring credentials; deterministic xhigh support metadata is covered. |
| 129 | `test/xiaomi-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
| 130 | `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | DETERMINISTIC-PORTED | Xiaomi empty-signature request-shape/catalog tests. |
| 131 | `test/zen.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.2 range. |
