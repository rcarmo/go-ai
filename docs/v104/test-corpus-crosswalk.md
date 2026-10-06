# Official 1.0.4 suite corpus

Every official suite path is listed once. Changed rows link to the delta audit. Unchanged rows inherit their historical native suite family; no per-inner-case or live-credential certification is implied.

Historical mappings: [AI 1.0.1](../v101/test-corpus-crosswalk.md), [durable 1.0.0](../durable/test-crosswalk.md). [Final native results](local-validation.md).

## ai: 165 suites

| Official suite | Delta scope |
| --- | --- |
| `packages/ai/test/abort.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-auth-token.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-cache-write-1h-cost.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-eager-tool-input-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-federation-sdk.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-federation.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-force-adaptive-thinking.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-long-cache-retention-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-mid-conversation-effort.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-opus-4-8-smoke.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-sse-parsing.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-strict-tool-schema.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-temperature-compat.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-thinking-binding-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-thinking-disable.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/anthropic-tool-name-normalization.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/assistant-message-frame.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/azure-openai-base-url.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/azure-openai-completions.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/azure-openai-responses-reasoning-replay.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/azure-openai-tool-choice.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/baseten-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-cache-write-1h-cost.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-convert-messages.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-credentials.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-custom-headers.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-endpoint-resolution.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-error-metadata.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-raw-stop-reason.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-redacted-reasoning.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-response-headers.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/bedrock-thinking-payload.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/cache-retention.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/classifier-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/cloudflare-ai-binding.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/cloudflare-stream.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/compat-env.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/constrained-sampling.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/context-estimate.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/context-overflow.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/cross-provider-handoff.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/empty.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/env-api-keys.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/error-body.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/event-stream.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/faux-provider.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/fetch-option.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/fireworks-model-generation.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/fireworks-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/generate-models-strict.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/github-copilot-anthropic.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/github-copilot-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-raw-stop-reason.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-shared-convert-tools.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-shared-gemini3-unsigned-tool-call.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-shared-image-tool-result-routing.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-shared-retry.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-shared-signed-empty-blocks.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-thinking-disable.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-thinking-level-map.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-thinking-signature.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/google-vertex-api-key-resolution.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/image-model-data.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/image-tool-result.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/images-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/images.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/interleaved-thinking.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/kimi-coding-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/lax-message-content.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/lazy-module-load.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/llama-cpp-classify.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/max-thinking.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/message-types.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/meta-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/mistral-http-transport.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/mistral-raw-stop-reason.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/mistral-reasoning-mode.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/mistral-tool-schema.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/model-catalog-types.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/model-data-validation.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/model-types.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/models-entry.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/models-runtime.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/node-http-proxy.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/oauth-auth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/oauth-callback-server.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/oauth-device-code.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-chatgpt-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-codex-cache-affinity-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-codex-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-codex-stream.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-cache-control-format.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-empty-tools.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-prompt-cache.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-provider-stream-event.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-raw-stop-reason.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-reasoning-details.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-response-model.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-retry.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-thinking-as-text.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-thinking-token-budget.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-tool-result-images.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-completions-vllm-priority.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-cache-affinity-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-chatgpt-sign-in.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-compat.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-empty-tool-result.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-foreign-toolcall-id.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-message-id.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-namespace.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/openai-responses-partial-json-cleanup.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-reasoning-replay-e2e.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-terminal-event.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openai-responses-tool-result-images.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/openai-responses-usage-limit.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/opencode-provider-headers.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openrouter-cache-control-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openrouter-cache-write-repro.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openrouter-images.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openrouter-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/openrouter-reasoning-options.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/overflow.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/pi-messages.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/pre-generation-error.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/provider-error-body-passthrough.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/provider-error-body-regression.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/provider-retry.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/providers.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/qwen-token-plan-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/radius-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/radius-provider.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/reasoning-options.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/responseid.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/retry.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/sampling-options.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/stream.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/supports-xhigh.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/system-message-replay.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/telemetry-options.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/text.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/together-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/tokens.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/tool-call-id-normalization.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/tool-call-without-result.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/total-tokens.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/transcript-tool-changes.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/transform-messages-copilot-openai-to-anthropic.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/typesafe-system-one.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/unicode-surrogate.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/ai/test/uuid.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/validation.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/xai-oauth.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/xai-responses.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/xhigh.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/xiaomi-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/zai-coding-plan-models.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/ai/test/zen.test.ts` | Unchanged: inherited native suite family; no new runtime delta |

## durable: 47 suites

| Official suite | Delta scope |
| --- | --- |
| `packages/durable/test/chord-guide.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/env-line-scan.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/env-node-conformance.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/env-node-spill.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/env-node.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/env-truncate.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-compaction.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-context.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-conversations.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-events.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-generation-recovery.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-generation.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-inbox.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-inspect.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-lifecycle.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-live-deltas.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-output-skip.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-output.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-ownership.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-prompt.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-registry.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-structured.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-submissions.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-task-graph.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-tasks-recovery.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-tasks.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-tools-recovery.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/harness-tools.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/harness-view.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/jsonl-storage.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/memory-storage.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/provider-session-cache-e2e.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/session-checkpoints-migrations.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-definitions.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-documents.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-forks.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-states.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-tables.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/session-watches.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/spec-usage.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/sqlite-facade.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/sqlite-migrations.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/sqlite-storage.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/storage-runtime-boundary.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
| `packages/durable/test/tools-read-differential.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/tools.test.ts` | Changed: [production proofs/native adaptations](changed-paths-crosswalk.md) |
| `packages/durable/test/types.test.ts` | Unchanged: inherited native suite family; no new runtime delta |
