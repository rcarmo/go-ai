# v0.84.4 whole-corpus test disposition manifest

Audit target: official upstream `github.com/earendil-works/pi` / `@earendil-works/pi-ai` tag `v0.84.4`, SHA `b79e4cc834970cca69daebffab7df1da7d1e52c4`.

Source enumerated: `/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/test/*.test.ts`.

Command:

```text
find /workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/test -name '*.test.ts' | sort
```

## Summary

- Upstream test files: **137**
- DETERMINISTIC-PORTED / DETERMINISTIC-PORTED-ADAPTED / covered by deterministic local tests: **108**
- N/A credential/live/JS-runtime/generator-policy/Workers-binding adapted only: **29**
- Unclassified upstream test files: **0**
- Achieved (`DETERMINISTIC + N/A`): **137 / 137**

Changed upstream test files in v0.84.4: **6** (5 modified + 1 added). New file: `openrouter-reasoning-options.test.ts`.

## Manifest

| # | Upstream test file | Disposition | Local Go evidence / N/A reason |
| ---: | --- | --- | --- |
| 1 | `test/abort.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live abort matrix. Go uses `context.Context` cancellation and deterministic cancellation tests (`TestRetryAssistantCallAbortedBackoffReturnsAbortedAndUnsuccessful`, OAuth context tests), but this file is not a portable wire fixture. |
| 2 | `test/anthropic-adaptive-thinking-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 3 | `test/anthropic-auth-token.test.ts` | DETERMINISTIC-PORTED | Default `pi` user-agent behavior is covered through shared header helper tests and provider request tests; `v0843_stream_fallback_test.go` also proves explicit Anthropic User-Agent override precedence in the production stream path. Carried forward from v0.84.3; not changed in v0.84.4. |
| 4 | `test/anthropic-cache-write-1h-cost.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 5 | `test/anthropic-eager-tool-input-compat.test.ts` | DETERMINISTIC-PORTED | inference/provider/anthropic/anthropic_request_compat_test.go; inference/provider/anthropic/anthropic.go. v0.84.2 strict schema conversion is wired into Anthropic tool input schemas while retaining eager-input compat behavior. |
| 6 | `test/anthropic-eager-tool-input-e2e.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 7 | `test/anthropic-empty-thinking-signature-compat.test.ts` | DETERMINISTIC-PORTED | Empty-signature replay cases in Anthropic request-compat tests. |
| 8 | `test/anthropic-force-adaptive-thinking.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 9 | `test/anthropic-long-cache-retention-e2e.test.ts` | N/A/live-only | Requires live Anthropic credentials/networked 1h cache service. Do not count as passing; deterministic cache-control TTL request shape is covered by cache-retention tests. |
| 10 | `test/anthropic-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 11 | `test/anthropic-opus-4-8-smoke.test.ts` | N/A/live-only | Live Anthropic smoke test requiring credentials. Catalog/thinking metadata is covered deterministically. |
| 12 | `test/anthropic-sse-parsing.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 13 | `test/anthropic-temperature-compat.test.ts` | DETERMINISTIC-PORTED | `anthropic_temperature_compat_test.go` temperature omission/preservation matrix. |
| 14 | `test/anthropic-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 15 | `test/anthropic-tool-name-normalization.test.ts` | DETERMINISTIC-PORTED | `anthropic_tool_name_normalization_test.go`. |
| 16 | `test/azure-openai-base-url.test.ts` | DETERMINISTIC-PORTED | Existing Azure base URL tests retained; v0.84.3 User-Agent default is covered by the shared provider header path. Carried forward from v0.84.3; not changed in v0.84.4. |
| 17 | `test/azure-openai-responses-reasoning-replay.test.ts` | DETERMINISTIC-PORTED | `azure_reasoning_replay_upstream_test.go`. |
| 18 | `test/azure-openai-tool-choice.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openairesponses/v0843_azure_tool_choice_test.go` verifies Azure Responses `tool_choice` serialization and tool preservation. Carried forward from v0.84.3; not changed in v0.84.4. |
| 19 | `test/baseten-models.test.ts` | DETERMINISTIC-PORTED | Generated v0.84.3 catalog updates Baseten metadata; exact regeneration and catalog tests cover it. Carried forward from v0.84.3; not changed in v0.84.4. |
| 20 | `test/bedrock-convert-messages.test.ts` | DETERMINISTIC-PORTED | inference/provider/bedrock/bedrock_convert_messages_upstream_test.go; inference/provider/bedrock/v0842_sanitization_test.go. v0.84.2 empty-key Bedrock document sanitization and converted strict tool schemas are covered; existing conversion cases remain covered. |
| 21 | `test/bedrock-credentials.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 22 | `test/bedrock-custom-headers.test.ts` | DETERMINISTIC-PORTED | `TestApplyCustomHeaders*` in Bedrock tests. |
| 23 | `test/bedrock-endpoint-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 24 | `test/bedrock-error-metadata.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `TestProcessConverseStreamAddsFailureDiagnosticForStreamErr` and diagnostic metadata matrix. |
| 25 | `test/bedrock-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 26 | `test/bedrock-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestProcessConverseStreamPreservesRawStopReason`. |
| 27 | `test/bedrock-redacted-reasoning.test.ts` | DETERMINISTIC-PORTED | `inference/provider/bedrock/v0843_redacted_reasoning_test.go` verifies redacted reasoning preservation, finalization, base64 signature, and replay before tool use. Carried forward from v0.84.3; not changed in v0.84.4. |
| 28 | `test/bedrock-response-headers.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `inference/provider/bedrock/bedrock.go` invokes response hooks with modeled request-id metadata; `v0843_redacted_reasoning_test.go` proves status 200/request-id hook invocation and no hook when request-id is absent. Full raw Smithy gateway header capture is limited by Go SDK testability and documented as adapted. Carried forward from v0.84.3; not changed in v0.84.4. |
| 29 | `test/bedrock-thinking-payload.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 30 | `test/cache-retention.test.ts` | DETERMINISTIC-PORTED | Anthropic cache-retention tests plus Responses/OpenAI cache tests. |
| 31 | `test/cloudflare-gateway-binding.test.ts` | N/A/JS-Workers-binding | —. New TS Workers AI binding fetch shim (`createGatewayBindingFetch`) is a JavaScript/Cloudflare Worker transport adapter around `fetch` and `env.AI.gateway()`. The Go library has no Workers binding/fetch-injection surface; existing Go Cloudflare gateway HTTPS URL/auth/placeholder behavior remains covered by Cloudflare stream/provider tests. |
| 32 | `test/cloudflare-stream.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 33 | `test/compat-env.test.ts` | N/A/JS-runtime | JS legacy registry/env compatibility surface (`registerApiProvider`/module runtime); Go uses typed provider registration. |
| 34 | `test/constrained-sampling.test.ts` | DETERMINISTIC-PORTED | tests/schema_strict_test.go; inference/provider/openairesponses/constrained_sampling_upstream_test.go; inference/provider/openai/v0842_strict_schema_test.go; inference/provider/mistral/v0842_strict_schema_test.go. v0.84.2 strict JSON Schema conversion implemented: object strictification, optional non-nullable null widening, unsupported schema fallback/reject behavior, and provider parameter conversion. |
| 35 | `test/context-estimate.test.ts` | DETERMINISTIC-PORTED | `tests/estimate_upstream_test.go`, `TestUpstreamV0806ContextEstimateIgnoresUsageBeforeNewerPrefixMessage`. |
| 36 | `test/context-overflow.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | tests/context_overflow_simulated_test.go. v0.84.2 catalog/live-matrix adjustments retain existing deterministic overflow coverage; credential/live provider additions remain N/A-live. |
| 37 | `test/cross-provider-handoff.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual cases to a live cross-provider handoff matrix requiring credentials/network. Deterministic replay/handoff transforms are covered locally. |
| 38 | `test/deferred-tools.test.ts` | DETERMINISTIC-PORTED | tests/deferred_tools_upstream_test.go; inference/provider/openairesponses/deferred_tools_upstream_test.go; inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. v0.84.2 Responses `additional_tools` mode and namespace-safe deferred-tool replay are implemented; prior `tool_search` semantics retained. |
| 39 | `test/empty.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live empty-message matrix. Deterministic empty tool-result/request behavior is covered locally. |
| 40 | `test/env-api-keys.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 41 | `test/error-body.test.ts` | DETERMINISTIC-PORTED | `tests/error_body_test.go`. |
| 42 | `test/faux-provider.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 43 | `test/fetch-option.test.ts` | N/A/JS-runtime | Custom `fetch` injection into JS SDK adapters has no Go public API analogue; Go uses `http.Client`/retry transport hooks. |
| 44 | `test/fireworks-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` covers retained/changed Fireworks model set including Kimi K2.7/K3 additions and retired K2.6 router removal. v0.84.4 changed. |
| 45 | `test/generate-models-strict.test.ts` | N/A/adapted-generator-policy | Private TS generator strict helper; Go verifies final generated artifacts exactly and has catalog tests for the new Qwen Individual model. Carried forward from v0.84.3; not changed in v0.84.4. |
| 46 | `test/github-copilot-anthropic.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 47 | `test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/github_copilot_v0843_test.go` and `oauth/github_copilot_policy_test.go` cover catalog filtering to known/tool-capable/unconfigured models, Individual policy fallback, no refresh catalog retry, 429 `Retry-After` policy retry, continuation after transport failure, bounded 5s login policy retry budget, and returned credentials for caller persistence. Carried forward from v0.84.3; not changed in v0.84.4. |
| 48 | `test/google-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/google/google_raw_stop_reason_test.go. v0.84.2 only tightens `toolUse` upgrade to stop-terminal Google tool calls; raw stop reason coverage remains deterministic. Carried forward from v0.84.3; not changed in v0.84.4. |
| 49 | `test/google-shared-convert-tools.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go`. |
| 50 | `test/google-shared-gemini3-unsigned-tool-call.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 51 | `test/google-shared-image-tool-result-routing.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go` image tool-result routing cases. |
| 52 | `test/google-shared-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 53 | `test/google-shared-signed-empty-blocks.test.ts` | N/A/JS-SDK | Google SDK signed empty-block serialization detail; Go signed/thinking/tool behavior covered where applicable. |
| 54 | `test/google-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 55 | `test/google-thinking-level-map.test.ts` | DETERMINISTIC-PORTED | `inference/provider/google/v0843_thinking_level_map_test.go` verifies mapped Google levels, unsupported mapping errors, and mapped token budgets. Carried forward from v0.84.3; not changed in v0.84.4. |
| 56 | `test/google-thinking-signature.test.ts` | DETERMINISTIC-PORTED | `google_thinking_signature_test.go`. |
| 57 | `test/google-vertex-api-key-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. Carried forward from v0.84.3; not changed in v0.84.4. |
| 58 | `test/image-model-data.test.ts` | N/A/adapted-generator-policy | Upstream tests private TS generator helper malformed-input paths (`parseOpenRouterImageModels`) that are not a Go public/runtime API. Go consumes exact generated image artifacts and verifies them separately via `scripts/generate-image-models.py` plus exact 42/42 comparator and `tests/images_test.go`; do not label as a test-for-test port. |
| 59 | `test/image-tool-result.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 60 | `test/images-models.test.ts` | DETERMINISTIC-PORTED | `tests/images_test.go`, `tests/images_openrouter_upstream_test.go`. |
| 61 | `test/images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 62 | `test/interleaved-thinking.test.ts` | N/A/live-provider | Requires live provider credentials/network; deterministic thinking replay/serialization tests cover local behavior. |
| 63 | `test/kimi-coding-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 64 | `test/lax-message-content.test.ts` | DETERMINISTIC-PORTED | `tests/lax_message_content_upstream_test.go`. |
| 65 | `test/lazy-module-load.test.ts` | N/A/JS-runtime | —. JS lazy module load import-count change has no Go analogue; providers are statically linked/registered. |
| 66 | `test/max-thinking.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go`, thinking-level clamp tests, and Codex request tests. |
| 67 | `test/mistral-http-transport.test.ts` | DETERMINISTIC-PORTED | `inference/provider/mistral/v0844_tool_call_fragments_test.go` proves fragmented indexed tool-call chunks merge when later fragments omit id and have empty function name. v0.84.4 changed. |
| 68 | `test/mistral-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/mistral/mistral_raw_stop_reason_test.go. Existing Mistral raw stop reason fixture remains passing after v0.84.2 direct HTTP transport/catalog refresh. |
| 69 | `test/mistral-reasoning-mode.test.ts` | DETERMINISTIC-PORTED | `mistral_reasoning_mode_test.go`. |
| 70 | `test/mistral-tool-schema.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 71 | `test/model-catalog-types.test.ts` | DETERMINISTIC-PORTED | Generated catalog compile/type checks and exact model comparator. Carried forward from v0.84.3; not changed in v0.84.4. |
| 72 | `test/model-data-validation.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 73 | `test/models-runtime.test.ts` | DETERMINISTIC-PORTED | `tests/models_runtime_test.go`. |
| 74 | `test/node-http-proxy.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 75 | `test/oauth-auth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `GetAPIKeyWithContext` / `RuntimeForProviderContext` cancellation/cause tests; JS credential-store UI N/A. |
| 76 | `test/oauth-device-code.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 77 | `test/openai-codex-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live Codex credentials/network. Do not count as passing; deterministic Codex cache-affinity headers are covered locally. |
| 78 | `test/openai-codex-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 79 | `test/openai-codex-stream.test.ts` | DETERMINISTIC-PORTED | Codex simple `toolChoice` propagation and existing SSE/WS coverage retained. |
| 80 | `test/openai-completions-cache-control-format.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 81 | `test/openai-completions-empty-tools.test.ts` | DETERMINISTIC-PORTED | `openai_completions_empty_tools_upstream_test.go`. |
| 82 | `test/openai-completions-prompt-cache.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 83 | `test/openai-completions-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestOpenAICompletionsRawStopReason`. |
| 84 | `test/openai-completions-reasoning-details.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/v0844_reasoning_details_test.go` proves adjacent reasoning.text/reasoning.summary detail merging, metadata/order preservation, thinkingSignature replay exactly once, and no duplicate tool-call signature. v0.84.4 changed. |
| 85 | `test/openai-completions-response-model.test.ts` | DETERMINISTIC-PORTED | `openai_completions_response_model_test.go`. |
| 86 | `test/openai-completions-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 87 | `test/openai-completions-thinking-as-text.test.ts` | DETERMINISTIC-PORTED | `openai_completions_thinking_as_text_test.go`. Carried forward from v0.84.3; not changed in v0.84.4. |
| 88 | `test/openai-completions-thinking-token-budget.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/openai_v0840_test.go` now covers v0.84.3 `thinkingTokenBudgetField` variants. Carried forward from v0.84.3; not changed in v0.84.4. |
| 89 | `test/openai-completions-tool-choice.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/v0844_reasoning_details_test.go` proves explicit `toolChoice:"none"` serializes as `tool_choice:"none"` even with no tools while omitting `tools`. v0.84.4 changed. |
| 90 | `test/openai-completions-tool-result-images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. Carried forward from v0.84.3; not changed in v0.84.4. |
| 91 | `test/openai-responses-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic affinity behavior is now covered by `TestOpenAIResponsesCompatSessionAffinityFormats`. |
| 92 | `test/openai-responses-compat.test.ts` | DETERMINISTIC-PORTED | Responses compat tests retained against regenerated v0.84.3 catalog. |
| 93 | `test/openai-responses-empty-tool-result.test.ts` | DETERMINISTIC-PORTED | `responses_empty_tool_result_upstream_test.go`. |
| 94 | `test/openai-responses-foreign-toolcall-id.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 95 | `test/openai-responses-message-id.test.ts` | DETERMINISTIC-PORTED | `responses_message_id_test.go`. |
| 96 | `test/openai-responses-namespace.test.ts` | DETERMINISTIC-PORTED | inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. New v0.84.2 namespace round-trip coverage: stream `namespace` from `output_item.done`, persist in `ContentBlock`/`ToolCall`, and replay only same-model namespace. |
| 97 | `test/openai-responses-partial-json-cleanup.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 98 | `test/openai-responses-reasoning-replay-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic replay is covered by `azure_reasoning_replay_upstream_test.go` and Responses replay tests. |
| 99 | `test/openai-responses-terminal-event.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 100 | `test/openai-responses-tool-result-images.test.ts` | DETERMINISTIC-PORTED | Responses image tool-result tests. |
| 101 | `test/openrouter-cache-control-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 102 | `test/openrouter-cache-write-repro.test.ts` | DETERMINISTIC-PORTED | `openrouter_cache_write_repro_upstream_test.go`. |
| 103 | `test/openrouter-images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 104 | `test/openrouter-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | OpenRouter OAuth/key exchange/context tests. |
| 105 | `test/openrouter-reasoning-options.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` and OpenAI request tests cover generated OpenRouter `supported_efforts` thinking maps including mandatory/off semantics and payload omission/none/effort behavior through `thinkingFormat:"openrouter"`. v0.84.4 changed (added file). |
| 106 | `test/overflow.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 107 | `test/pi-messages.test.ts` | DETERMINISTIC-PORTED | `inference/provider/pimessages/pimessages_test.go`. Carried forward from v0.84.3; not changed in v0.84.4. |
| 108 | `test/provider-error-body-passthrough.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 109 | `test/provider-error-body-regression.test.ts` | DETERMINISTIC-PORTED | `tests/provider_error_body_test.go`. |
| 110 | `test/provider-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 111 | `test/providers.test.ts` | DETERMINISTIC-PORTED/covered | Provider registry/env/catalog coverage updated through exact generation and ZAI/xAI tests. |
| 112 | `test/qwen-token-plan-models.test.ts` | DETERMINISTIC-PORTED | `tests/qwen_token_plan_upstream_test.go` includes `deepseek-v4-pro-0813` in Individual allowlist. Carried forward from v0.84.3; not changed in v0.84.4. |
| 113 | `test/radius-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/radius_test.go` plus context refresh tests. |
| 114 | `test/reasoning-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 115 | `test/responseid.test.ts` | DETERMINISTIC-PORTED | `tests/responseid_simulated_test.go`. |
| 116 | `test/retry.test.ts` | DETERMINISTIC-PORTED/covered | Go provider retry sleeps are context-aware; existing retry tests remain passing. |
| 117 | `test/sampling-options.test.ts` | DETERMINISTIC-PORTED | `openai_v0840_test.go`, `responses_v0840_test.go` sampling matrices. |
| 118 | `test/stream.test.ts` | N/A/live-provider | —. v0.84.2 live stream matrix update requires provider credentials/network; deterministic streaming fixtures remain covered locally. Carried forward from v0.84.3; not changed in v0.84.4. |
| 119 | `test/supports-xhigh.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go` updated for v0.84.3 thinking-level changes. Carried forward from v0.84.3; not changed in v0.84.4. |
| 120 | `test/telemetry-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 121 | `test/text.test.ts` | DETERMINISTIC-PORTED | `TestContentTextExtractsTextBlocks`. |
| 122 | `test/together-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 123 | `test/tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated token accounting remains ported in `tests/tokens_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live token-stat case requires credentials and is N/A/live-only. |
| 124 | `test/tool-call-id-normalization.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 125 | `test/tool-call-without-result.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to a live provider matrix requiring credentials; deterministic missing-tool-result filtering/replay behavior remains covered locally. |
| 126 | `test/total-tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | tests/total_tokens_simulated_test.go; tests/models_test.go. v0.84.2 catalog/live matrix update retains deterministic token-accounting fixtures; credential/live additions remain N/A-live. |
| 127 | `test/transform-messages-copilot-openai-to-anthropic.test.ts` | DETERMINISTIC-PORTED | Anthropic/OpenAI replay transform tests. |
| 128 | `test/unicode-surrogate.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
| 129 | `test/uuid.test.ts` | DETERMINISTIC-PORTED | `TestUUIDv7UsesRFC9562LayoutAndPreservesMonotonicOrder`. |
| 130 | `test/validation.test.ts` | DETERMINISTIC-PORTED | tests/upstream_validation_v0842_test.go; context.go. v0.84.2 optional non-nullable `null` is treated as omission before validation while nullable/reference nulls are preserved. |
| 131 | `test/xai-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/xai_test.go` plus context refresh tests. |
| 132 | `test/xai-responses.test.ts` | DETERMINISTIC-PORTED | `xai_responses_upstream_test.go` verifies Grok 4.6 Responses metadata and raw `/responses` request shape for low/medium/high/xhigh mapping, encrypted reasoning include, endpoint/auth, explicit User-Agent override, while retaining the Grok 4.5 regression. Carried forward from v0.84.3; not changed in v0.84.4. |
| 133 | `test/xhigh.test.ts` | N/A/live-provider | Live OpenAI xhigh test requiring credentials; deterministic xhigh support metadata is covered. |
| 134 | `test/xiaomi-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_catalog_upstream_test.go` updated for v0.84.3 Mimo v2.5 catalog and retired v2 flash removal. Carried forward from v0.84.3; not changed in v0.84.4. |
| 135 | `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | DETERMINISTIC-PORTED | Xiaomi empty-signature request-shape/catalog tests. |
| 136 | `test/zai-coding-plan-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` verifies GLM-5.3 catalog metadata/compat; existing ZAI coding plan env/provider tests remain. v0.84.4 changed. |
| 137 | `test/zen.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Inherited from v0.84.1 manifest; not changed in the official v0.84.4 range. |
