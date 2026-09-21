# v0.87.0 upstream test corpus manifest

- Source: `docs/v0870/test-corpus-150.txt`
- Rows: 150
- Changed tests: 82 (80 remain in the final 150-test corpus; two changed paths were removed upstream: `test/deferred-tools.test.ts` and `test/codex-websocket-cached-probe.ts`).

| # | Upstream test | Classification | Go disposition |
| ---: | --- | --- | --- |
| 1 | `test/abort.test.ts` | carried corpus | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 2 | `test/anthropic-adaptive-thinking-models.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 3 | `test/anthropic-auth-token.test.ts` | v0.87.0 changed | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 4 | `test/anthropic-cache-write-1h-cost.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 5 | `test/anthropic-eager-tool-input-compat.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 6 | `test/anthropic-eager-tool-input-e2e.test.ts` | carried corpus | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 7 | `test/anthropic-empty-thinking-signature-compat.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 8 | `test/anthropic-force-adaptive-thinking.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 9 | `test/anthropic-long-cache-retention-e2e.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 10 | `test/anthropic-mid-conversation-effort.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 11 | `test/anthropic-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 12 | `test/anthropic-opus-4-8-smoke.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 13 | `test/anthropic-sse-parsing.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 14 | `test/anthropic-temperature-compat.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 15 | `test/anthropic-thinking-binding-e2e.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 16 | `test/anthropic-thinking-disable.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 17 | `test/anthropic-tool-name-normalization.test.ts` | carried corpus | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 18 | `test/assistant-message-frame.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 19 | `test/azure-openai-base-url.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 20 | `test/azure-openai-responses-reasoning-replay.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 21 | `test/azure-openai-tool-choice.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 22 | `test/baseten-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 23 | `test/bedrock-cache-write-1h-cost.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 24 | `test/bedrock-convert-messages.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 25 | `test/bedrock-credentials.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 26 | `test/bedrock-custom-headers.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 27 | `test/bedrock-endpoint-resolution.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 28 | `test/bedrock-error-metadata.test.ts` | v0.87.0 changed | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 29 | `test/bedrock-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 30 | `test/bedrock-raw-stop-reason.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 31 | `test/bedrock-redacted-reasoning.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 32 | `test/bedrock-response-headers.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 33 | `test/bedrock-thinking-payload.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 34 | `test/cache-retention.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 35 | `test/cloudflare-ai-binding.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 36 | `test/cloudflare-stream.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 37 | `test/compat-env.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 38 | `test/constrained-sampling.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 39 | `test/context-estimate.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 40 | `test/context-overflow.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 41 | `test/cross-provider-handoff.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 42 | `test/empty.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 43 | `test/env-api-keys.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 44 | `test/error-body.test.ts` | carried corpus | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 45 | `test/event-stream.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 46 | `test/faux-provider.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 47 | `test/fetch-option.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 48 | `test/fireworks-model-generation.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 49 | `test/fireworks-models.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 50 | `test/generate-models-strict.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 51 | `test/github-copilot-anthropic.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 52 | `test/github-copilot-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 53 | `test/google-raw-stop-reason.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 54 | `test/google-shared-convert-tools.test.ts` | carried corpus | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 55 | `test/google-shared-gemini3-unsigned-tool-call.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 56 | `test/google-shared-image-tool-result-routing.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 57 | `test/google-shared-retry.test.ts` | carried corpus | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 58 | `test/google-shared-signed-empty-blocks.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 59 | `test/google-thinking-disable.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 60 | `test/google-thinking-level-map.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 61 | `test/google-thinking-signature.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 62 | `test/google-vertex-api-key-resolution.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 63 | `test/image-model-data.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 64 | `test/image-tool-result.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 65 | `test/images-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 66 | `test/images.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 67 | `test/interleaved-thinking.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 68 | `test/kimi-coding-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 69 | `test/lax-message-content.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 70 | `test/lazy-module-load.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 71 | `test/max-thinking.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 72 | `test/message-types.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 73 | `test/meta-oauth.test.ts` | v0.87.0 changed | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 74 | `test/mistral-http-transport.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 75 | `test/mistral-raw-stop-reason.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 76 | `test/mistral-reasoning-mode.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 77 | `test/mistral-tool-schema.test.ts` | carried corpus | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 78 | `test/model-catalog-types.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 79 | `test/model-data-validation.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 80 | `test/models-runtime.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 81 | `test/node-http-proxy.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 82 | `test/oauth-auth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 83 | `test/oauth-device-code.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 84 | `test/openai-codex-cache-affinity-e2e.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 85 | `test/openai-codex-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 86 | `test/openai-codex-stream.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 87 | `test/openai-completions-cache-control-format.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 88 | `test/openai-completions-empty-tools.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 89 | `test/openai-completions-prompt-cache.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 90 | `test/openai-completions-raw-stop-reason.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 91 | `test/openai-completions-reasoning-details.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 92 | `test/openai-completions-response-model.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 93 | `test/openai-completions-retry.test.ts` | v0.87.0 changed | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 94 | `test/openai-completions-thinking-as-text.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 95 | `test/openai-completions-thinking-token-budget.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 96 | `test/openai-completions-tool-choice.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 97 | `test/openai-completions-tool-result-images.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 98 | `test/openai-completions-vllm-priority.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 99 | `test/openai-responses-cache-affinity-e2e.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 100 | `test/openai-responses-compat.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 101 | `test/openai-responses-empty-tool-result.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 102 | `test/openai-responses-foreign-toolcall-id.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 103 | `test/openai-responses-message-id.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 104 | `test/openai-responses-namespace.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 105 | `test/openai-responses-partial-json-cleanup.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 106 | `test/openai-responses-reasoning-replay-e2e.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 107 | `test/openai-responses-terminal-event.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 108 | `test/openai-responses-tool-result-images.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 109 | `test/opencode-provider-headers.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 110 | `test/openrouter-cache-control-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 111 | `test/openrouter-cache-write-repro.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 112 | `test/openrouter-images.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 113 | `test/openrouter-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 114 | `test/openrouter-reasoning-options.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 115 | `test/overflow.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 116 | `test/pi-messages.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 117 | `test/pre-generation-error.test.ts` | v0.87.0 changed | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 118 | `test/provider-error-body-passthrough.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 119 | `test/provider-error-body-regression.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 120 | `test/provider-retry.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 121 | `test/providers.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 122 | `test/qwen-token-plan-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 123 | `test/radius-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 124 | `test/radius-provider.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 125 | `test/reasoning-options.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 126 | `test/responseid.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 127 | `test/retry.test.ts` | v0.87.0 changed | Retry/error/abort behavior covered by Go retry, stream, and provider error tests. |
| 128 | `test/sampling-options.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 129 | `test/stream.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 130 | `test/supports-xhigh.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 131 | `test/system-message-replay.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 132 | `test/telemetry-options.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 133 | `test/text.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 134 | `test/together-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 135 | `test/tokens.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 136 | `test/tool-call-id-normalization.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 137 | `test/tool-call-without-result.test.ts` | carried corpus | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 138 | `test/total-tokens.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 139 | `test/transcript-tool-changes.test.ts` | v0.87.0 changed | Transcript/tool behavior covered by Go transcript, schema/tool, deferred-tool, and provider payload tests. |
| 140 | `test/transform-messages-copilot-openai-to-anthropic.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 141 | `test/unicode-surrogate.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 142 | `test/uuid.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 143 | `test/validation.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 144 | `test/xai-oauth.test.ts` | carried corpus | OAuth/provider auth behavior covered by focused Go OAuth/auth tests or existing provider auth tests. |
| 145 | `test/xai-responses.test.ts` | v0.87.0 changed | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 146 | `test/xhigh.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 147 | `test/xiaomi-models.test.ts` | carried corpus | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 148 | `test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
| 149 | `test/zai-coding-plan-models.test.ts` | v0.87.0 changed | Catalog/provider metadata covered by generated catalog tests, validators, and regeneration comparator. |
| 150 | `test/zen.test.ts` | carried corpus | Runtime behavior covered by existing Go provider/parity tests and full package test gate. |
