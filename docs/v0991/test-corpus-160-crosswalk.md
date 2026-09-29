# v0.99.1 upstream test corpus crosswalk (160 rows)

| # | upstream | disposition | Go evidence / rationale |
|---:|---|---|---|
| 1 | `abort.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 2 | `anthropic-adaptive-thinking-models.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 3 | `anthropic-auth-token.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 4 | `anthropic-cache-write-1h-cost.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 5 | `anthropic-eager-tool-input-compat.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 6 | `anthropic-eager-tool-input-e2e.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 7 | `anthropic-empty-thinking-signature-compat.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 8 | `anthropic-force-adaptive-thinking.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 9 | `anthropic-long-cache-retention-e2e.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 10 | `anthropic-mid-conversation-effort.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 11 | `anthropic-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 12 | `anthropic-opus-4-8-smoke.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 13 | `anthropic-sse-parsing.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 14 | `anthropic-temperature-compat.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 15 | `anthropic-thinking-binding-e2e.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 16 | `anthropic-thinking-disable.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 17 | `anthropic-tool-name-normalization.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 18 | `assistant-message-frame.test.ts` | ported | inference/provider/openairesponses/message_wire_exclusion_v0991_test.go; types.go JSON tags |
| 19 | `azure-openai-base-url.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 20 | `azure-openai-responses-reasoning-replay.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 21 | `azure-openai-tool-choice.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 22 | `baseten-models.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 23 | `bedrock-cache-write-1h-cost.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 24 | `bedrock-convert-messages.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 25 | `bedrock-credentials.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 26 | `bedrock-custom-headers.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 27 | `bedrock-endpoint-resolution.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 28 | `bedrock-error-metadata.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 29 | `bedrock-models.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 30 | `bedrock-raw-stop-reason.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 31 | `bedrock-redacted-reasoning.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 32 | `bedrock-response-headers.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 33 | `bedrock-thinking-payload.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 34 | `cache-retention.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 35 | `classifier-models.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 36 | `cloudflare-ai-binding.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 37 | `cloudflare-stream.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 38 | `cloudflare-workers-ai-system-one.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 39 | `compat-env.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 40 | `constrained-sampling.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 41 | `context-estimate.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 42 | `context-overflow.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 43 | `cross-provider-handoff.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 44 | `empty.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 45 | `env-api-keys.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 46 | `error-body.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 47 | `event-stream.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 48 | `faux-provider.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 49 | `fetch-option.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 50 | `fireworks-model-generation.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 51 | `fireworks-models.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 52 | `generate-models-strict.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 53 | `github-copilot-anthropic.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 54 | `github-copilot-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 55 | `google-raw-stop-reason.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 56 | `google-shared-convert-tools.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 57 | `google-shared-gemini3-unsigned-tool-call.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 58 | `google-shared-image-tool-result-routing.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 59 | `google-shared-retry.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 60 | `google-shared-signed-empty-blocks.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 61 | `google-thinking-disable.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 62 | `google-thinking-level-map.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 63 | `google-thinking-signature.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 64 | `google-vertex-api-key-resolution.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 65 | `image-model-data.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 66 | `image-tool-result.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 67 | `images-models.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 68 | `images.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 69 | `interleaved-thinking.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 70 | `kimi-coding-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 71 | `lax-message-content.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 72 | `lazy-module-load.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 73 | `llama-cpp-classify.test.ts` | ported | classifier_llama_cpp.go; tests/classifier_llama_cpp_v0991_test.go |
| 74 | `max-thinking.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 75 | `message-types.test.ts` | ported | inference/provider/openairesponses/message_wire_exclusion_v0991_test.go; types.go JSON tags |
| 76 | `meta-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 77 | `mistral-http-transport.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 78 | `mistral-raw-stop-reason.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 79 | `mistral-reasoning-mode.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 80 | `mistral-tool-schema.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 81 | `model-catalog-types.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 82 | `model-data-validation.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 83 | `model-types.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 84 | `models-runtime.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 85 | `node-http-proxy.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 86 | `oauth-auth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 87 | `oauth-callback-server.test.ts` | ported | oauth/openai_chatgpt_test.go |
| 88 | `oauth-device-code.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 89 | `openai-chatgpt-oauth.test.ts` | ported | oauth/openai_chatgpt_test.go |
| 90 | `openai-codex-cache-affinity-e2e.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 91 | `openai-codex-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 92 | `openai-codex-stream.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 93 | `openai-completions-cache-control-format.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 94 | `openai-completions-empty-tools.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 95 | `openai-completions-prompt-cache.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 96 | `openai-completions-provider-stream-event.test.ts` | ported | inference/provider/openai/provider_stream_event_v0991_test.go |
| 97 | `openai-completions-raw-stop-reason.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 98 | `openai-completions-reasoning-details.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 99 | `openai-completions-response-model.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 100 | `openai-completions-retry.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 101 | `openai-completions-thinking-as-text.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 102 | `openai-completions-thinking-token-budget.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 103 | `openai-completions-tool-choice.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 104 | `openai-completions-tool-result-images.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 105 | `openai-completions-vllm-priority.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 106 | `openai-responses-cache-affinity-e2e.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 107 | `openai-responses-chatgpt-sign-in.test.ts` | ported | inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 108 | `openai-responses-compat.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 109 | `openai-responses-empty-tool-result.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 110 | `openai-responses-foreign-toolcall-id.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 111 | `openai-responses-message-id.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 112 | `openai-responses-namespace.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 113 | `openai-responses-partial-json-cleanup.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 114 | `openai-responses-reasoning-replay-e2e.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 115 | `openai-responses-terminal-event.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 116 | `openai-responses-tool-result-images.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 117 | `openai-responses-usage-limit.test.ts` | ported | inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 118 | `opencode-provider-headers.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 119 | `openrouter-cache-control-models.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 120 | `openrouter-cache-write-repro.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 121 | `openrouter-images.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 122 | `openrouter-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 123 | `openrouter-reasoning-options.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 124 | `overflow.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 125 | `pi-messages.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 126 | `pre-generation-error.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 127 | `provider-error-body-passthrough.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 128 | `provider-error-body-regression.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 129 | `provider-retry.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 130 | `providers.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 131 | `qwen-token-plan-models.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 132 | `radius-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 133 | `radius-provider.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 134 | `reasoning-options.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 135 | `responseid.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 136 | `retry.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 137 | `sampling-options.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 138 | `stream.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 139 | `supports-xhigh.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 140 | `system-message-replay.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 141 | `telemetry-options.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 142 | `text.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 143 | `together-models.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 144 | `tokens.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 145 | `tool-call-id-normalization.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 146 | `tool-call-without-result.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 147 | `total-tokens.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 148 | `transcript-tool-changes.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 149 | `transform-messages-copilot-openai-to-anthropic.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 150 | `typesafe-system-one.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 151 | `unicode-surrogate.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 152 | `uuid.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 153 | `validation.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 154 | `xai-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 155 | `xai-responses.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 156 | `xhigh.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 157 | `xiaomi-models.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 158 | `xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 159 | `zai-coding-plan-models.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 160 | `zen.test.ts` | unchanged-corpus | retained upstream corpus baseline; no v0.99.1 changed-test delta |
