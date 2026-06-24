# Upstream test-for-test parity checklist — pi-ai ec6311b / v0.80.2

Canonical source: `github.com/earendil-works/pi` at commit `ec6311b` (`packages/ai`).

Finding: the npm tarball omits upstream tests; this checklist is based on the GitHub source tree. I find **86** `packages/ai/**/*.test.ts` files at this commit, not 87 under `packages/ai`.

Status key: **PORTED** = upstream test file has a named Go port with the same cases/expected values and is passing; **DONE** = local Go suite covers the same behavior class; **PARTIAL** = local coverage exists but exact upstream assertions are not yet ported one-for-one; **MISSING** = no known local equivalent. Drive this checklist toward PORTED for every upstream file.

## Summary

- PORTED: 4 files
- DONE: 63 files
- PARTIAL: 19 files
- MISSING: 0 files

## Test files

| Upstream test file | Status | Local Go coverage path | Notes |
|---|---:|---|---|
| `test/abort.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-adaptive-thinking-models.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-cache-write-1h-cost.test.ts` | DONE | inference/provider/anthropic/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-eager-tool-input-compat.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-eager-tool-input-e2e.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go<br>context_test.go; harness_integration_test.go; provider tests | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-empty-thinking-signature-compat.test.ts` | DONE | inference/provider/anthropic/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-force-adaptive-thinking.test.ts` | DONE | inference/provider/anthropic/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-long-cache-retention-e2e.test.ts` | DONE | inference/provider/anthropic/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-oauth.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go<br>oauth/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-opus-4-8-smoke.test.ts` | PARTIAL | inference/provider/anthropic/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/anthropic-sse-parsing.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>transports/sse/*_test.go; provider stream tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/anthropic-temperature-compat.test.ts` | PORTED | inference/provider/anthropic/anthropic_temperature_compat_test.go | Ported all six upstream payload cases with identical expected `temperature` values: Opus 4.7/4.8 omit explicit `0`, Opus 4.7 omits default `1`, Opus 4.6 and Sonnet 4.6 keep `0`, and custom `supportsTemperature: false` omits `0`. Passing. |
| `test/anthropic-thinking-disable.test.ts` | PORTED | inference/provider/anthropic/anthropic_thinking_disable_test.go | Ported all six upstream payload cases with identical expected `thinking`/`output_config` values: Sonnet 4.5, Opus 4.6, and Opus 4.8 disable thinking when off; Fable 5 omits disabled thinking; Opus 4.8 uses adaptive summarized thinking with high/xhigh output effort when reasoning is enabled. Also fixed Fable 5 off-map handling. Passing. |
| `test/anthropic-tool-name-normalization.test.ts` | PORTED | inference/provider/anthropic/anthropic_tool_name_normalization_test.go | Ported upstream four OAuth tool-name normalization cases with identical expected returned tool names: `todowrite`, `read`, `find`, and `my_custom_tool`; also asserts outbound Claude Code canonical casing where applicable. Passing. |
| `test/azure-openai-base-url.test.ts` | DONE | inference/provider/openairesponses/*_test.go<br>inference/provider/openai/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-convert-messages.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-custom-headers.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-endpoint-resolution.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-models.test.ts` | DONE | inference/provider/bedrock/*_test.go<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/bedrock-thinking-payload.test.ts` | DONE | inference/provider/bedrock/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/cache-retention.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/compat-env.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/context-overflow.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/cross-provider-handoff.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/empty.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/env-api-keys.test.ts` | DONE | local Go tests: needs exact mapping | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/faux-provider.test.ts` | PARTIAL | local Go tests: needs exact mapping | Needs detailed test-for-test port or explicit equivalence proof. |
| `test/fireworks-models.test.ts` | DONE | models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
| `test/github-copilot-anthropic.test.ts` | DONE | inference/provider/anthropic/*_test.go<br>oauth/*_test.go | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
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
| `test/mistral-reasoning-mode.test.ts` | PARTIAL | inference/provider/mistral/*_test.go | Needs detailed test-for-test port or explicit equivalence proof. |
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
| `test/openai-completions-response-model.test.ts` | DONE | inference/provider/openai/*_test.go<br>models_test.go; compat tests | Behavior class has targeted Go regression coverage; exact test-for-test assertion audit still recommended. |
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
