# Upstream test-for-test parity checklist — pi-ai ec6311b / v0.80.2

Canonical source: `github.com/earendil-works/pi` at commit `ec6311b` (`packages/ai`).

Finding: the npm tarball omits upstream tests; this checklist is based on the GitHub source tree. I find **86** `packages/ai/**/*.test.ts` files at this commit, not 87 under `packages/ai`.

Status key: **PORTED** = upstream test file has a named Go port with the same cases/expected values and is passing; **DONE** = local Go suite covers the same behavior class; **PARTIAL** = local coverage exists but exact upstream assertions are not yet ported one-for-one; **MISSING** = no known local equivalent. Drive this checklist toward PORTED for every upstream file.

## Summary

- PORTED: 16 files
- DONE: 52 files
- PARTIAL: 18 files
- MISSING: 0 files

## Test files

| Upstream test file | Status | Local Go coverage path | Notes |
|---|---:|---|---|
| `test/abort.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-adaptive-thinking-models.test.ts` | PORTED | anthropic_adaptive_thinking_models_test.go | Ported upstream adaptive-thinking metadata assertions: expected current model set contains Fable/Opus/OpenCode/Cloudflare/Vercel entries and all flagged IDs match adaptive-model regex. Passing. |
| `test/anthropic-cache-write-1h-cost.test.ts` | PORTED | inference/provider/anthropic/anthropic_cache_write_1h_cost_test.go | Ported both upstream cache-write cost cases with exact expected usage/cost values: 1h split `400000` at `7.75`, and no-breakdown fallback at `6.25`. Passing. |
| `test/anthropic-eager-tool-input-compat.test.ts` | PORTED | inference/provider/anthropic/anthropic_request_compat_test.go | Ported all three upstream eager tool-input compatibility cases: default per-tool `eager_input_streaming`, legacy beta when disabled, and no beta/tools when no tools. Passing. |
| `test/anthropic-eager-tool-input-e2e.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-empty-thinking-signature-compat.test.ts` | PORTED | inference/provider/anthropic/anthropic_request_compat_test.go | Ported both upstream empty thinking-signature replay cases: default converts to text, `allowEmptySignature` preserves `thinking` with empty signature. Passing. |
| `test/anthropic-force-adaptive-thinking.test.ts` | PORTED | inference/provider/anthropic/anthropic_request_compat_test.go | Ported all five upstream force-adaptive-thinking cases: custom legacy default, compat override adaptive, Fable 5 xhigh native effort, built-in opt-out, and reasoning-off disabled payload. Passing. |
| `test/anthropic-long-cache-retention-e2e.test.ts` | DONE | inference/provider/anthropic/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-oauth.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go<br>oauth/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-opus-4-8-smoke.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-sse-parsing.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>transports/sse/*_test.go; provider stream tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-temperature-compat.test.ts` | PORTED | inference/provider/anthropic/anthropic_temperature_compat_test.go | Ported all six upstream payload cases with identical expected `temperature` values: Opus 4.7/4.8 omit explicit `0`, Opus 4.7 omits default `1`, Opus 4.6 and Sonnet 4.6 keep `0`, and custom `supportsTemperature: false` omits `0`. Passing. |
| `test/anthropic-thinking-disable.test.ts` | PORTED | inference/provider/anthropic/anthropic_thinking_disable_test.go | Ported all six upstream payload cases with identical expected `thinking`/`output_config` values: Sonnet 4.5, Opus 4.6, and Opus 4.8 disable thinking when off; Fable 5 omits disabled thinking; Opus 4.8 uses adaptive summarized thinking with high/xhigh output effort when reasoning is enabled. Also fixed Fable 5 off-map handling. Passing. |
| `test/anthropic-tool-name-normalization.test.ts` | PORTED | inference/provider/anthropic/anthropic_tool_name_normalization_test.go | Ported upstream four OAuth tool-name normalization cases with identical expected returned tool names: `todowrite`, `read`, `find`, and `my_custom_tool`; also asserts outbound Claude Code canonical casing where applicable. Passing. |
| `test/azure-openai-base-url.test.ts` | PORTED | inference/provider/openairesponses/azure_openai_base_url_test.go | Ported all 11 upstream Azure base URL / prompt cache / storage cases with identical expected URLs, invalid-URL error substring, 64-char prompt cache clamp, and `store=false`. Passing. |
| `test/bedrock-convert-messages.test.ts` | PORTED | inference/provider/bedrock/bedrock_convert_messages_upstream_test.go | Ported all nine upstream message-conversion cases with identical expected content behavior: unknown user/assistant blocks skipped, empty user content becomes `<empty>`, blank user text filtered, surrogate-emptied user text becomes `<empty>`, surrogate-emptied/unknown-only assistant messages are skipped, and blank tool result content becomes `<empty>`. Passing. |
| `test/bedrock-custom-headers.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-endpoint-resolution.test.ts` | PORTED | inference/provider/bedrock/bedrock_endpoint_resolution_upstream_test.go | Ported all seven upstream endpoint-resolution cases with identical expected base URL/region/endpoint decisions: EU inference-profile base URL, AWS_REGION standard endpoint unpinning, EU endpoint region derivation, explicit/scoped/ambient profile handling, custom endpoint passthrough, and commercial/GovCloud ARN region extraction. Passing. |
| `test/bedrock-models.test.ts` | DONE | inference/provider/bedrock/*_test.go<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-thinking-payload.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/cache-retention.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/compat-env.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/context-overflow.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/cross-provider-handoff.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/empty.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/env-api-keys.test.ts` | PORTED | env_api_keys_test.go | Ported all three upstream env-key cases: generic GitHub tokens ignored for Copilot, `COPILOT_GITHUB_TOKEN` selected, and `ZAI_CODING_CN_API_KEY` selected. Passing. |
| `test/faux-provider.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/fireworks-models.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/github-copilot-anthropic.test.ts` | PORTED | inference/provider/anthropic/github_copilot_anthropic_test.go | Ported upstream Copilot Anthropic metadata/header/payload assertions: adaptive effort overrides, Bearer auth + static/dynamic Copilot headers, Anthropic Messages payload, and no interleaved beta for adaptive models. Passing. |
| `test/github-copilot-oauth.test.ts` | DONE | oauth/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-shared-convert-tools.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-shared-gemini3-unsigned-tool-call.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-shared-image-tool-result-routing.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go<br>images_test.go; images/openrouter<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-thinking-disable.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-thinking-signature.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/google-vertex-api-key-resolution.test.ts` | DONE | inference/provider/google/*_test.go; inference/provider/geminicli/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/image-tool-result.test.ts` | DONE | images_test.go; images/openrouter<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/images-models.test.ts` | DONE | images_test.go; images/openrouter<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/images.test.ts` | DONE | images_test.go; images/openrouter | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/interleaved-thinking.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/lazy-module-load.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/mistral-reasoning-mode.test.ts` | PORTED | inference/provider/mistral/mistral_reasoning_mode_test.go | Ported all seven upstream Mistral reasoning/cache-key cases with identical expected `reasoning_effort`, `prompt_mode`, and `prompt_cache_key` values. Passing. |
| `test/mistral-tool-schema.test.ts` | PARTIAL | inference/provider/mistral/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/models-runtime.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/node-http-proxy.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/oauth-auth.test.ts` | PARTIAL | oauth/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/oauth-device-code.test.ts` | PARTIAL | oauth/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/openai-codex-cache-affinity-e2e.test.ts` | DONE | inference/provider/openaicodex/*_test.go<br>inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-codex-oauth.test.ts` | DONE | inference/provider/openaicodex/*_test.go<br>inference/provider/openai/*_test.go<br>oauth/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-codex-stream.test.ts` | DONE | inference/provider/openaicodex/*_test.go<br>inference/provider/openai/*_test.go<br>transports/sse/*_test.go; provider stream tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-cache-control-format.test.ts` | DONE | inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-empty-tools.test.ts` | DONE | inference/provider/openai/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-prompt-cache.test.ts` | DONE | inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-reasoning-details.test.ts` | DONE | inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-response-model.test.ts` | PORTED | inference/provider/openai/openai_completions_response_model_test.go | Ported all three upstream response-model cases: routed chunk model sets `responseModel` without changing `model`, echoed requested id leaves it empty, and empty/missing chunk model is ignored. Passing. |
| `test/openai-completions-retry.test.ts` | DONE | inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-thinking-as-text.test.ts` | DONE | inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-tool-choice.test.ts` | DONE | inference/provider/openai/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-completions-tool-result-images.test.ts` | DONE | inference/provider/openai/*_test.go<br>images_test.go; images/openrouter<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-cache-affinity-e2e.test.ts` | DONE | inference/provider/openairesponses/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-copilot-provider.test.ts` | DONE | inference/provider/openairesponses/*_test.go<br>oauth/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-foreign-toolcall-id.test.ts` | DONE | inference/provider/openairesponses/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-message-id.test.ts` | DONE | inference/provider/openairesponses/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-partial-json-cleanup.test.ts` | DONE | inference/provider/openairesponses/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-reasoning-replay-e2e.test.ts` | DONE | inference/provider/openairesponses/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-terminal-event.test.ts` | DONE | inference/provider/openairesponses/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openai-responses-tool-result-images.test.ts` | DONE | inference/provider/openairesponses/*_test.go<br>images_test.go; images/openrouter<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openrouter-cache-write-repro.test.ts` | DONE | images_test.go; images/openrouter | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/openrouter-images.test.ts` | DONE | images_test.go; images/openrouter | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/overflow.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/providers.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/responseid.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/stream.test.ts` | DONE | transports/sse/*_test.go; provider stream tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/supports-xhigh.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/together-models.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/tokens.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/tool-call-id-normalization.test.ts` | DONE | context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/tool-call-without-result.test.ts` | DONE | context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/total-tokens.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/transform-messages-copilot-openai-to-anthropic.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>inference/provider/openai/*_test.go<br>oauth/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/unicode-surrogate.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/validation.test.ts` | PORTED | `upstream_validation_test.go`; `context.go` | Ported all three upstream cases: Function-constructor fallback equivalent, AJV-compatible primitive coercions, and invalid coercion rejection. Passing with `go test . -run TestUpstreamValidation`. |
| `test/xhigh.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/xiaomi-models.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/zen.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
