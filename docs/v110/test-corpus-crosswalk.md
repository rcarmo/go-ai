# Official 1.1.0 suite corpus

Every official suite is listed once. Changed suites map to named native tests and adaptations. Unchanged suites inherit the [1.0.4 corpus](../v104/test-corpus-crosswalk.md), [AI1.0.1 crosswalk](../v101/test-corpus-crosswalk.md) and [durable native crosswalk](../durable/test-crosswalk.md). This is suite-level coverage, not blanket per-inner-case, live-provider or Cloudflare-platform certification. [Validation](local-validation.md).

## ai: 167 suites

| Official suite | Native scope |
| --- | --- |
| `packages/ai/test/abort.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | Haiku5.5 catalog/adaptive effort; `tests/catalog_110_test.go; inference/provider/anthropic/*adaptive*test.go` |
| `packages/ai/test/anthropic-auth-token.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-cache-write-1h-cost.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-eager-tool-input-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-federation-sdk.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-federation.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-force-adaptive-thinking.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-long-cache-retention-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-mid-conversation-effort.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-oauth.test.ts` | callback/manual/state/redirect and token contracts; `oauth/anthropic_110_test.go; oauth/anthropic_manual_110_test.go; oauth/anthropic_test.go` |
| `packages/ai/test/anthropic-opus-4-8-smoke.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-sse-parsing.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-strict-tool-schema.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-temperature-compat.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-thinking-binding-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-thinking-disable.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/anthropic-tool-name-normalization.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/assistant-message-frame.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/azure-openai-base-url.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/azure-openai-completions.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/azure-openai-responses-reasoning-replay.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/azure-openai-tool-choice.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/baseten-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-cache-write-1h-cost.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-convert-messages.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-credentials.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-custom-headers.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-endpoint-resolution.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-error-metadata.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-raw-stop-reason.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-redacted-reasoning.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-response-headers.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/bedrock-thinking-payload.test.ts` | Claude/GPT precedence and default effort; `inference/provider/bedrock/bedrock_110_test.go; bedrock_thinking_payload_upstream_test.go` |
| `packages/ai/test/cache-retention.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/classifier-models.test.ts` | 26classifier identity/input/pricing records; `tests/catalog_110_test.go; tests/models_v100_typed_registry_test.go` |
| `packages/ai/test/cloudflare-ai-binding.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/cloudflare-stream.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/compat-env.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/constrained-sampling.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/context-estimate.test.ts` | UTF-16 estimates and usage anchors; `tests/estimate_upstream_test.go; tests/estimate_upstream_test.go` |
| `packages/ai/test/context-overflow.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/cross-provider-handoff.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/empty.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/env-api-keys.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/error-body.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/event-stream.test.ts` | producer timing and detached progress; TS queue/end result uses native channel/Complete adaptation; `assistant_stream_110_test.go; event_snapshot_test.go` |
| `packages/ai/test/faux-provider.test.ts` | part-prefix/cache isolation and direct timing; `inference/provider/faux/prompt_cache_110_test.go; faux_test.go; duration_110_test.go` |
| `packages/ai/test/fetch-option.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/fireworks-model-generation.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/fireworks-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/generate-models-strict.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/github-copilot-anthropic.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/github-copilot-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-raw-stop-reason.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-shared-convert-tools.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-shared-gemini3-unsigned-tool-call.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-shared-image-tool-result-routing.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-shared-retry.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-shared-signed-empty-blocks.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-thinking-disable.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-thinking-level-map.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-thinking-signature.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/google-vertex-api-key-resolution.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/image-model-data.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/image-tool-result.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/images-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/images.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/interleaved-thinking.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/kimi-coding-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/lax-message-content.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/lazy-module-load.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/llama-cpp-classify.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/max-thinking.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/message-types.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/meta-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/mistral-http-transport.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/mistral-raw-stop-reason.test.ts` | server error classification; `inference/provider/mistral/raw_stop_reason_upstream_test.go` |
| `packages/ai/test/mistral-reasoning-mode.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/mistral-tool-schema.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/model-catalog-types.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/model-cost-tiers.test.ts` | tiered cost and detached prices; `tests/catalog_110_test.go; tests/classifier_decisions_110_test.go` |
| `packages/ai/test/model-data-validation.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/model-types.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/models-entry.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/models-runtime.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/node-http-proxy.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/oauth-auth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/oauth-callback-server.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/oauth-device-code.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-chatgpt-oauth.test.ts` | default/override/explicit empty hint and callback/manual; `oauth/agent_name_110_test.go; oauth/openai_chatgpt_test.go` |
| `packages/ai/test/openai-codex-cache-affinity-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-codex-oauth.test.ts` | native browser/manual/refresh and retained device flow; live OAuth not exercised; `oauth/codex_browser_110_test.go; oauth/oauth_test.go` |
| `packages/ai/test/openai-codex-stream.test.ts` | header precedence SSE/WS and recovery; `inference/provider/openaicodex/headers_110_test.go; codex_ws_test.go` |
| `packages/ai/test/openai-completions-cache-control-format.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-empty-tools.test.ts` | updated estimated budget and empty-tools compatibility; `inference/provider/openai/openai_completions_empty_tools_upstream_test.go` |
| `packages/ai/test/openai-completions-prompt-cache.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-provider-stream-event.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-raw-stop-reason.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-reasoning-details.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-response-model.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-retry.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-thinking-as-text.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-thinking-token-budget.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-tool-result-images.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-completions-vllm-priority.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-decisions.test.ts` | production wire/errors/refusals/usage/retries/images; `tests/classifier_decisions_110_test.go` |
| `packages/ai/test/openai-responses-cache-affinity-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-chatgpt-sign-in.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-compat.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-empty-tool-result.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-foreign-toolcall-id.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-message-id.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-namespace.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-partial-json-cleanup.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-reasoning-replay-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-terminal-event.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-tool-result-images.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openai-responses-usage-limit.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/opencode-provider-headers.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openrouter-cache-control-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openrouter-cache-write-repro.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openrouter-images.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openrouter-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/openrouter-reasoning-options.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/overflow.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/pi-messages.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/pre-generation-error.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/provider-error-body-passthrough.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/provider-error-body-regression.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/provider-retry.test.ts` | busy/quota precedence and 504 policy; `tests/retry_assistant_test.go; tests/retry_assistant_test.go; tests/classifier_decisions_110_test.go` |
| `packages/ai/test/providers.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/qwen-token-plan-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/radius-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/radius-provider.test.ts` | baseline replacement/empty account/cache/validators; `oauth/radius_catalog_110_test.go; radius_runtime_test.go; radius_revalidation_test.go` |
| `packages/ai/test/reasoning-options.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/responseid.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/retry.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/sampling-options.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/stream.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/supports-xhigh.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/system-message-replay.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/telemetry-options.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/text.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/together-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/tokens.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/tool-call-id-normalization.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/tool-call-without-result.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/total-tokens.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/transcript-tool-changes.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/transform-messages-copilot-openai-to-anthropic.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/typesafe-system-one.test.ts` | shared HTTP remains compatible; `tests/classifier_contract_issue1_test.go; tests/classifier_runtime_v0991_test.go` |
| `packages/ai/test/unicode-surrogate.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/uuid.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/validation.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/xai-oauth.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/xai-responses.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/xhigh.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/xiaomi-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/zai-coding-plan-models.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/ai/test/zen.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |

## durable: 49 suites

| Official suite | Native scope |
| --- | --- |
| `packages/durable/test/chord-guide.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/env-line-scan.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/env-node-conformance.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/env-node-spill.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/env-node.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/env-truncate.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-compaction.test.ts` | head changes, prompt baselines, background compaction; `durable/compaction*test.go; context_cache_110_test.go` |
| `packages/durable/test/harness-context.test.ts` | as-of/visibility/edit/head/tool ordering/cache expiry; `durable/context_cache_110_test.go; history_test.go; prompt_test.go` |
| `packages/durable/test/harness-conversations.test.ts` | ordered scans/forks/conversation ownership; `durable/scan_110_test.go; conversation_created_test.go; history_test.go` |
| `packages/durable/test/harness-events.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-generation-recovery.test.ts` | recovery/join/persisted receipt timing; `durable/task_recovery_test.go; task_structured_recovery_test.go; receipt_duration_110_test.go` |
| `packages/durable/test/harness-generation.test.ts` | production generation, duration projection, defaults; `durable/harness_generation_test.go; generation_progress_test.go; models_110_test.go` |
| `packages/durable/test/harness-inbox.test.ts` | ordered submissions and queue policies; `durable/inbox*test.go; submission_test.go; scan_110_test.go` |
| `packages/durable/test/harness-inspect.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-lifecycle.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-live-deltas.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-output-skip.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-output.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-ownership.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-prompt.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-registry.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-structured.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-submissions.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-task-graph.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-tasks-recovery.test.ts` | first-start preservation across reopen/recovery; `durable/task_recovery_test.go; task_times_110_test.go` |
| `packages/durable/test/harness-tasks.test.ts` | lifecycle/ordered pages/model capability/deadlines; `durable/task_times_110_test.go; task_scheduler_test.go; models_110_test.go; model_request_110_test.go` |
| `packages/durable/test/harness-tools-recovery.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/harness-tools.test.ts` | per-attempt durations/recovery/unexecuted calls/model access; `durable/tool_duration_110_test.go; tool_progress_test.go; harness_tools_test.go` |
| `packages/durable/test/harness-view.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/jsonl-storage.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/memory-storage.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/provider-session-cache-e2e.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-checkpoints-migrations.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-definitions.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-documents.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-forks.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-states.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-tables.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/session-watches.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/spec-usage.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/sqlite-cloudflare.test.ts` | native SQLite atomic persistence; Cloudflare JS binding adapter N/A; `durable/storage_sqlite_test.go` |
| `packages/durable/test/sqlite-facade.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/sqlite-migrations.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/sqlite-storage.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/storage-runtime-boundary.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/system-order-cache-e2e.test.ts` | leading baseline/system edits preserve provider context; no live Anthropic prompt-cache hit-rate measurement; `durable/context_cache_110_test.go; prompt_test.go; prompt_tools_test.go` |
| `packages/durable/test/tools-read-differential.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/tools.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
| `packages/durable/test/types.test.ts` | Unchanged; inherited native suite family and existing platform/live limitations |
