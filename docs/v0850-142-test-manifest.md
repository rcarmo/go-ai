# v0.85.0 whole-corpus test disposition manifest
Audit target: official upstream `github.com/earendil-works/pi` / `@earendil-works/pi-ai` tag `v0.85.0`, SHA `107d79f11072bbc8a3a757ed7fd69596bee7d68c`.
Source enumerated: `/workspace/tmp/pi-mono-audit/packages/ai/test/*.test.ts`.
Command:

```text
find /workspace/tmp/pi-mono-audit/packages/ai/test -name '*.test.ts' | sort
```
## Summary
- Upstream test files: **142**
- DETERMINISTIC-PORTED / DETERMINISTIC-PORTED-ADAPTED / covered by deterministic local tests: **123**
- N/A credential/live/JS-runtime/generator-policy/Workers-binding adapted only: **19**
- Unclassified upstream test files: **0**
- Achieved (`DETERMINISTIC + N/A`): **142 / 142**

Changed upstream test files in v0.85.0: **29** (6 added, 22 modified, 1 deleted from the previous baseline). The deleted `cloudflare-gateway-binding.test.ts` is superseded by `cloudflare-ai-binding.test.ts`.

## Manifest

| # | Upstream test file | Disposition | Local Go evidence / N/A reason |
| ---: | --- | --- | --- |
| 1 | `test/abort.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live abort matrix. Go uses `context.Context` cancellation and deterministic cancellation tests (`TestRetryAssistantCallAbortedBackoffReturnsAbortedAndUnsuccessful`, OAuth context tests), but this file is not a portable wire fixture. |
| 2 | `test/anthropic-adaptive-thinking-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 3 | `test/anthropic-auth-token.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing auth-token/Copilot/OAuth header tests remain; `v0850_beta_override_test.go` adds model-level `Anthropic-Beta` override parity so configured beta headers replace generated betas. |
| 4 | `test/anthropic-cache-write-1h-cost.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing `anthropic_cache_write_1h_cost_test.go` continues to cover 1h cache-write usage/cost and fallback zero/default handling. |
| 5 | `test/anthropic-eager-tool-input-compat.test.ts` | DETERMINISTIC-PORTED | inference/provider/anthropic/anthropic_request_compat_test.go; inference/provider/anthropic/anthropic.go. v0.84.2 strict schema conversion is wired into Anthropic tool input schemas while retaining eager-input compat behavior. |
| 6 | `test/anthropic-eager-tool-input-e2e.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 7 | `test/anthropic-empty-thinking-signature-compat.test.ts` | DETERMINISTIC-PORTED | Empty-signature replay cases in Anthropic request-compat tests. |
| 8 | `test/anthropic-force-adaptive-thinking.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 9 | `test/anthropic-long-cache-retention-e2e.test.ts` | N/A/live-only | Requires live Anthropic credentials/networked 1h cache service. Do not count as passing; deterministic cache-control TTL request shape is covered by cache-retention tests. |
| 10 | `test/anthropic-mid-conversation-effort.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed/new. `inference/provider/anthropic/v0850_mid_conversation_effort_test.go` covers same-provider historical `providerThinkingLevel` markers, active effort default/mapping, adaptive `block_binding.drop_block`, top-level `output_config`, temperature omission, beta headers, result `ProviderThinkingLevel`, and generated direct/OpenRouter catalog gates. |
| 11 | `test/anthropic-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 12 | `test/anthropic-opus-4-8-smoke.test.ts` | N/A/live-only | Live Anthropic smoke test requiring credentials. Catalog/thinking metadata is covered deterministically. |
| 13 | `test/anthropic-sse-parsing.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing Anthropic SSE parsing tests plus `v0850_mid_conversation_effort_test.go` cover signed thinking/reasoning deltas, terminal pending errors, managed `providerThinkingLevel`, and raw stop behavior. |
| 14 | `test/anthropic-temperature-compat.test.ts` | DETERMINISTIC-PORTED | `anthropic_temperature_compat_test.go` temperature omission/preservation matrix. |
| 15 | `test/anthropic-thinking-binding-e2e.test.ts` | N/A/live-only | v0.85.0 changed/new. Live Anthropic signed-thinking binding conformance requires `ANTHROPIC_API_KEY` and remote validation. Deterministic Go coverage proves request marker/binding replay and preserves signed thinking blocks through production request/stream paths; no hidden skip is counted. |
| 16 | `test/anthropic-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 17 | `test/anthropic-tool-name-normalization.test.ts` | DETERMINISTIC-PORTED | `anthropic_tool_name_normalization_test.go`. |
| 18 | `test/assistant-message-frame.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed/new. `assistant_message_frame.go` plus `tests/assistant_message_frame_v0850_test.go` cover compact stream frame encode/reduce for text, thinking/signatures/redaction, tool calls/JSON deltas, namespaces, start ordering, and terminal omission. |
| 19 | `test/azure-openai-base-url.test.ts` | DETERMINISTIC-PORTED | Existing Azure base URL tests retained; v0.84.3 User-Agent default is covered by the shared provider header path. Carried forward; not changed in v0.85.0. |
| 20 | `test/azure-openai-responses-reasoning-replay.test.ts` | DETERMINISTIC-PORTED | `azure_reasoning_replay_upstream_test.go`. |
| 21 | `test/azure-openai-tool-choice.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openairesponses/v0843_azure_tool_choice_test.go` verifies Azure Responses `tool_choice` serialization and tool preservation. Carried forward; not changed in v0.85.0. |
| 22 | `test/baseten-models.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `tests/models_v0850_catalog_test.go` verifies Baseten Kimi K2.7 Code image/text metadata against regenerated exact catalog. |
| 23 | `test/bedrock-convert-messages.test.ts` | DETERMINISTIC-PORTED | inference/provider/bedrock/bedrock_convert_messages_upstream_test.go; inference/provider/bedrock/v0842_sanitization_test.go. v0.84.2 empty-key Bedrock document sanitization and converted strict tool schemas are covered; existing conversion cases remain covered. |
| 24 | `test/bedrock-credentials.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 25 | `test/bedrock-custom-headers.test.ts` | DETERMINISTIC-PORTED | `TestApplyCustomHeaders*` in Bedrock tests. |
| 26 | `test/bedrock-endpoint-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 27 | `test/bedrock-error-metadata.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `TestProcessConverseStreamAddsFailureDiagnosticForStreamErr` and diagnostic metadata matrix. |
| 28 | `test/bedrock-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 29 | `test/bedrock-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestProcessConverseStreamPreservesRawStopReason`. |
| 30 | `test/bedrock-redacted-reasoning.test.ts` | DETERMINISTIC-PORTED | `inference/provider/bedrock/v0843_redacted_reasoning_test.go` verifies redacted reasoning preservation, finalization, base64 signature, and replay before tool use. Carried forward; not changed in v0.85.0. |
| 31 | `test/bedrock-response-headers.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `inference/provider/bedrock/bedrock.go` invokes response hooks with modeled request-id metadata; `v0843_redacted_reasoning_test.go` proves status 200/request-id hook invocation and no hook when request-id is absent. Full raw Smithy gateway header capture is limited by Go SDK testability and documented as adapted. Carried forward; not changed in v0.85.0. |
| 32 | `test/bedrock-thinking-payload.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 33 | `test/cache-retention.test.ts` | DETERMINISTIC-PORTED | Anthropic cache-retention tests plus Responses/OpenAI cache tests. |
| 34 | `test/cloudflare-ai-binding.test.ts` | DETERMINISTIC-PORTED/ADAPTED | v0.85.0 changed/new replacing deleted gateway binding test. `cloudflare_ai_binding.go` and `tests/cloudflare_ai_binding_v0850_test.go` expose the auth sentinel and early binding-fetch validation; existing Cloudflare URL/header/suppression tests cover Go HTTPS dispatch. The JavaScript Worker `env.AI.fetch` transport itself is adapted because Go has no native Workers FetchFunction runtime. |
| 35 | `test/cloudflare-stream.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 36 | `test/compat-env.test.ts` | N/A/JS-runtime | JS legacy registry/env compatibility surface (`registerApiProvider`/module runtime); Go uses typed provider registration. |
| 37 | `test/constrained-sampling.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing strict/constrained sampling tests remain green; generator/catalog changes are covered by exact regeneration and provider schema tests. |
| 38 | `test/context-estimate.test.ts` | DETERMINISTIC-PORTED | `tests/estimate_upstream_test.go`, `TestUpstreamV0806ContextEstimateIgnoresUsageBeforeNewerPrefixMessage`. |
| 39 | `test/context-overflow.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | tests/context_overflow_simulated_test.go. v0.84.2 catalog/live-matrix adjustments retain existing deterministic overflow coverage; credential/live provider additions remain N/A-live. |
| 40 | `test/cross-provider-handoff.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual cases to a live cross-provider handoff matrix requiring credentials/network. Deterministic replay/handoff transforms are covered locally. |
| 41 | `test/deferred-tools.test.ts` | DETERMINISTIC-PORTED | tests/deferred_tools_upstream_test.go; inference/provider/openairesponses/deferred_tools_upstream_test.go; inference/provider/openairesponses/v0842_namespace_additional_tools_test.go. v0.84.2 Responses `additional_tools` mode and namespace-safe deferred-tool replay are implemented; prior `tool_search` semantics retained. |
| 42 | `test/empty.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to the live empty-message matrix. Deterministic empty tool-result/request behavior is covered locally. |
| 43 | `test/env-api-keys.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 44 | `test/error-body.test.ts` | DETERMINISTIC-PORTED | `tests/error_body_test.go`. |
| 45 | `test/faux-provider.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 46 | `test/fetch-option.test.ts` | N/A/JS-runtime | Custom `fetch` injection into JS SDK adapters has no Go public API analogue; Go uses `http.Client`/retry transport hooks. |
| 47 | `test/fireworks-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` covers retained/changed Fireworks model set including Kimi K2.7/K3 additions and retired K2.6 router removal. Carried forward; not changed in v0.85.0. |
| 48 | `test/generate-models-strict.test.ts` | N/A/adapted-generator-policy | v0.85.0 changed. Private TS generator strict helper behavior is represented by exact Go generator parsing/emission plus clean comparator/fault gates, not a helper-for-helper port. |
| 49 | `test/github-copilot-anthropic.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing GitHub Copilot Anthropic tests cover Claude Code headers/tool-name normalization and no incompatible Anthropic beta emission; managed-effort beta generation is separately covered for Anthropic models. |
| 50 | `test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | v0.85.0 changed. Existing OAuth policy/runtime tests remain; JS credential-store UI aspects are N/A, while token/header/catalog behavior is deterministic in Go. |
| 51 | `test/google-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/google/google_raw_stop_reason_test.go. v0.84.2 only tightens `toolUse` upgrade to stop-terminal Google tool calls; raw stop reason coverage remains deterministic. Carried forward; not changed in v0.85.0. |
| 52 | `test/google-shared-convert-tools.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go`. |
| 53 | `test/google-shared-gemini3-unsigned-tool-call.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 54 | `test/google-shared-image-tool-result-routing.test.ts` | DETERMINISTIC-PORTED | `google_shared_upstream_test.go` image tool-result routing cases. |
| 55 | `test/google-shared-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 56 | `test/google-shared-signed-empty-blocks.test.ts` | N/A/JS-SDK | Google SDK signed empty-block serialization detail; Go signed/thinking/tool behavior covered where applicable. |
| 57 | `test/google-thinking-disable.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 58 | `test/google-thinking-level-map.test.ts` | DETERMINISTIC-PORTED | `inference/provider/google/v0843_thinking_level_map_test.go` verifies mapped Google levels, unsupported mapping errors, and mapped token budgets. Carried forward; not changed in v0.85.0. |
| 59 | `test/google-thinking-signature.test.ts` | DETERMINISTIC-PORTED | `google_thinking_signature_test.go`. |
| 60 | `test/google-vertex-api-key-resolution.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. Carried forward; not changed in v0.85.0. |
| 61 | `test/image-model-data.test.ts` | N/A/adapted-generator-policy | Upstream tests private TS generator helper malformed-input paths (`parseOpenRouterImageModels`) that are not a Go public/runtime API. Go consumes exact generated image artifacts and verifies them separately via `scripts/generate-image-models.py` plus exact 42/42 comparator and `tests/images_test.go`; do not label as a test-for-test port. |
| 62 | `test/image-tool-result.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 63 | `test/images-models.test.ts` | DETERMINISTIC-PORTED | `tests/images_test.go`, `tests/images_openrouter_upstream_test.go`. |
| 64 | `test/images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 65 | `test/interleaved-thinking.test.ts` | N/A/live-provider | Requires live provider credentials/network; deterministic thinking replay/serialization tests cover local behavior. |
| 66 | `test/kimi-coding-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 67 | `test/lax-message-content.test.ts` | DETERMINISTIC-PORTED | `tests/lax_message_content_upstream_test.go`. |
| 68 | `test/lazy-module-load.test.ts` | N/A/JS-runtime | —. JS lazy module load import-count change has no Go analogue; providers are statically linked/registered. |
| 69 | `test/max-thinking.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go`, thinking-level clamp tests, and Codex request tests. |
| 70 | `test/mistral-http-transport.test.ts` | DETERMINISTIC-PORTED | `inference/provider/mistral/v0844_tool_call_fragments_test.go` proves fragmented indexed tool-call chunks merge when later fragments omit id and have empty function name. Carried forward; not changed in v0.85.0. |
| 71 | `test/mistral-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | inference/provider/mistral/mistral_raw_stop_reason_test.go. Existing Mistral raw stop reason fixture remains passing after v0.84.2 direct HTTP transport/catalog refresh. |
| 72 | `test/mistral-reasoning-mode.test.ts` | DETERMINISTIC-PORTED | `mistral_reasoning_mode_test.go`. |
| 73 | `test/mistral-tool-schema.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 74 | `test/model-catalog-types.test.ts` | DETERMINISTIC-PORTED | Generated catalog compile/type checks and exact model comparator. Carried forward; not changed in v0.85.0. |
| 75 | `test/model-data-validation.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 76 | `test/models-runtime.test.ts` | DETERMINISTIC-PORTED | `tests/models_runtime_test.go`. |
| 77 | `test/node-http-proxy.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `retry.go` implements v0.85.0 NO_PROXY/no_proxy matching including root/subdomain and bracketed IPv6; `tests/v0850_uuid_proxy_test.go` covers deterministic proxy bypass behavior. |
| 78 | `test/oauth-auth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `GetAPIKeyWithContext` / `RuntimeForProviderContext` cancellation/cause tests; JS credential-store UI N/A. |
| 79 | `test/oauth-device-code.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 80 | `test/openai-codex-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live Codex credentials/network. Do not count as passing; deterministic Codex cache-affinity headers are covered locally. |
| 81 | `test/openai-codex-oauth.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 82 | `test/openai-codex-stream.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `inference/provider/openaicodex/v0850_terminal_sse_test.go` proves terminal SSE frames are processed without a trailing blank line; existing Codex SSE/WS tests remain green. |
| 83 | `test/openai-completions-cache-control-format.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing OpenAI cache-control format tests remain green against regenerated compat metadata. |
| 84 | `test/openai-completions-empty-tools.test.ts` | DETERMINISTIC-PORTED | `openai_completions_empty_tools_upstream_test.go`. |
| 85 | `test/openai-completions-prompt-cache.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 86 | `test/openai-completions-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | `TestOpenAICompletionsRawStopReason`. |
| 87 | `test/openai-completions-reasoning-details.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/v0844_reasoning_details_test.go` proves adjacent reasoning.text/reasoning.summary detail merging, metadata/order preservation, thinkingSignature replay exactly once, and no duplicate tool-call signature. Carried forward; not changed in v0.85.0. |
| 88 | `test/openai-completions-response-model.test.ts` | DETERMINISTIC-PORTED | `openai_completions_response_model_test.go`. |
| 89 | `test/openai-completions-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 90 | `test/openai-completions-thinking-as-text.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing OpenAI thinking-as-text tests remain green against regenerated model compat. |
| 91 | `test/openai-completions-thinking-token-budget.test.ts` | DETERMINISTIC-PORTED | `inference/provider/openai/openai_v0840_test.go` now covers v0.84.3 `thinkingTokenBudgetField` variants. Carried forward; not changed in v0.85.0. |
| 92 | `test/openai-completions-tool-choice.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing OpenAI tool-choice request-shape tests remain green. |
| 93 | `test/openai-completions-tool-result-images.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing OpenAI tool-result image routing/downgrade tests remain green. |
| 94 | `test/openai-completions-vllm-priority.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed/new. `inference/provider/openai/v0850_vllm_priority_test.go`, `compat.go`, `openai.go`, and regenerated catalog metadata prove `completionsCompat.vllmPriority` serializes as OpenAI `priority` and is omitted when unset. |
| 95 | `test/openai-responses-cache-affinity-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic affinity behavior is now covered by `TestOpenAIResponsesCompatSessionAffinityFormats`. |
| 96 | `test/openai-responses-compat.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `inference/provider/openairesponses/v0850_max_output_tokens_test.go` covers `responsesCompat.supportsMaxOutputTokens` default/false request serialization. |
| 97 | `test/openai-responses-empty-tool-result.test.ts` | DETERMINISTIC-PORTED | `responses_empty_tool_result_upstream_test.go`. |
| 98 | `test/openai-responses-foreign-toolcall-id.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 99 | `test/openai-responses-message-id.test.ts` | DETERMINISTIC-PORTED | `responses_message_id_test.go`. |
| 100 | `test/openai-responses-namespace.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing namespace/additional-tools Responses tests remain green; regenerated catalog/provider metadata retained. |
| 101 | `test/openai-responses-partial-json-cleanup.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 102 | `test/openai-responses-reasoning-replay-e2e.test.ts` | N/A/live-only | Requires live OpenAI Responses credentials/network. Do not count as passing; deterministic replay is covered by `azure_reasoning_replay_upstream_test.go` and Responses replay tests. |
| 103 | `test/openai-responses-terminal-event.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 104 | `test/openai-responses-tool-result-images.test.ts` | DETERMINISTIC-PORTED | Responses image tool-result tests. |
| 105 | `test/openrouter-cache-control-models.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Exact catalog regeneration plus OpenRouter cache-control model tests cover changed model metadata. |
| 106 | `test/openrouter-cache-write-repro.test.ts` | DETERMINISTIC-PORTED | `openrouter_cache_write_repro_upstream_test.go`. |
| 107 | `test/openrouter-images.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 108 | `test/openrouter-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | OpenRouter OAuth/key exchange/context tests. |
| 109 | `test/openrouter-reasoning-options.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` and OpenAI request tests cover generated OpenRouter `supported_efforts` thinking maps including mandatory/off semantics and payload omission/none/effort behavior through `thinkingFormat:"openrouter"`. Carried forward; not changed in v0.85.0. |
| 110 | `test/overflow.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 111 | `test/pi-messages.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `inference/provider/pimessages/pimessages_test.go` covers `providerThinkingLevel` propagation from terminal server events into the assistant message. |
| 112 | `test/pre-generation-error.test.ts` | DETERMINISTIC-PORTED/ADAPTED | v0.85.0 changed/new. Upstream asserts JavaScript `streamSimple` throws synchronously before creating an `AssistantMessageEventStream` when auth is missing. Go provider APIs expose `<-chan Event`; equivalent production-path behavior is deterministic `ErrorEvent` before dispatch with exact missing-key messages across direct providers, covered by provider auth tests and documented as channel-adapted rather than a TS construction throw. |
| 113 | `test/provider-error-body-passthrough.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 114 | `test/provider-error-body-regression.test.ts` | DETERMINISTIC-PORTED | `tests/provider_error_body_test.go`. |
| 115 | `test/provider-retry.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 116 | `test/providers.test.ts` | DETERMINISTIC-PORTED/covered | Provider registry/env/catalog coverage updated through exact generation and ZAI/xAI tests. |
| 117 | `test/qwen-token-plan-models.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `tests/qwen_token_plan_upstream_test.go` and `tests/models_v0850_catalog_test.go` cover updated Qwen Token Plan model allowlists/metadata. |
| 118 | `test/radius-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/radius_test.go` plus context refresh tests. |
| 119 | `test/reasoning-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 120 | `test/responseid.test.ts` | DETERMINISTIC-PORTED | `tests/responseid_simulated_test.go`. |
| 121 | `test/retry.test.ts` | DETERMINISTIC-PORTED/covered | Go provider retry sleeps are context-aware; existing retry tests remain passing. |
| 122 | `test/sampling-options.test.ts` | DETERMINISTIC-PORTED | `openai_v0840_test.go`, `responses_v0840_test.go` sampling matrices. |
| 123 | `test/stream.test.ts` | N/A/live-provider | —. v0.84.2 live stream matrix update requires provider credentials/network; deterministic streaming fixtures remain covered locally. Carried forward; not changed in v0.85.0. |
| 124 | `test/supports-xhigh.test.ts` | DETERMINISTIC-PORTED | `tests/supports_xhigh_upstream_test.go` updated for v0.84.3 thinking-level changes. Carried forward; not changed in v0.85.0. |
| 125 | `test/telemetry-options.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 126 | `test/text.test.ts` | DETERMINISTIC-PORTED | `TestContentTextExtractsTextBlocks`. |
| 127 | `test/together-models.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 128 | `test/tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Simulated token accounting remains ported in `tests/tokens_simulated_test.go`; v0.84.1 Qwen Token Plan Individual live token-stat case requires credentials and is N/A/live-only. |
| 129 | `test/tool-call-id-normalization.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing tool-call ID normalization tests remain green across Anthropic/OpenAI/Codex paths. |
| 130 | `test/tool-call-without-result.test.ts` | N/A/live-provider | v0.84.1 adds Qwen Token Plan Individual to a live provider matrix requiring credentials; deterministic missing-tool-result filtering/replay behavior remains covered locally. |
| 131 | `test/total-tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | tests/total_tokens_simulated_test.go; tests/models_test.go. v0.84.2 catalog/live matrix update retains deterministic token-accounting fixtures; credential/live additions remain N/A-live. |
| 132 | `test/transform-messages-copilot-openai-to-anthropic.test.ts` | DETERMINISTIC-PORTED | Anthropic/OpenAI replay transform tests. |
| 133 | `test/unicode-surrogate.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
| 134 | `test/uuid.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. `utils_text_uuid.go` accepts optional timestamp millis while preserving monotonic RFC9562 layout; `tests/v0850_uuid_proxy_test.go` covers deterministic timestamp/version/variant behavior. |
| 135 | `test/validation.test.ts` | DETERMINISTIC-PORTED | tests/upstream_validation_v0842_test.go; context.go. v0.84.2 optional non-nullable `null` is treated as omission before validation while nullable/reference nulls are preserved. |
| 136 | `test/xai-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | `oauth/xai_test.go` plus context refresh tests. |
| 137 | `test/xai-responses.test.ts` | DETERMINISTIC-PORTED | v0.85.0 changed. Existing xAI Responses tests plus regenerated catalog verify Grok routing/request shape and removal of stale Grok 4 entries. |
| 138 | `test/xhigh.test.ts` | N/A/live-provider | Live OpenAI xhigh test requiring credentials; deterministic xhigh support metadata is covered. |
| 139 | `test/xiaomi-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_catalog_upstream_test.go` updated for v0.84.3 Mimo v2.5 catalog and retired v2 flash removal. Carried forward; not changed in v0.85.0. |
| 140 | `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | DETERMINISTIC-PORTED | Xiaomi empty-signature request-shape/catalog tests. |
| 141 | `test/zai-coding-plan-models.test.ts` | DETERMINISTIC-PORTED | `tests/models_v0844_catalog_test.go` verifies GLM-5.3 catalog metadata/compat; existing ZAI coding plan env/provider tests remain. Carried forward; not changed in v0.85.0. |
| 142 | `test/zen.test.ts` | DETERMINISTIC-PORTED/covered | existing v0.84.1 Go corpus. Carried forward; not changed in the official v0.85.0 range. |
