# v1.0.1 whole upstream corpus crosswalk

171 exact paths:164 executable .test.ts files and7 support/fixtures. Six changed markers handled through changed-tests-crosswalk.md. Unchanged rows retain accepted historical disposition, without claiming new live-credential validation; explicit live cases remain credential-only remainders. No pending classifications.

| # | Upstream path | Changed | Disposition | Evidence/rationale |
| ---: | --- | --- | --- | --- |
| 1 | `packages/ai/test/abort.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 2 | `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 3 | `packages/ai/test/anthropic-auth-token.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 4 | `packages/ai/test/anthropic-cache-write-1h-cost.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 5 | `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 6 | `packages/ai/test/anthropic-eager-tool-input-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 7 | `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 8 | `packages/ai/test/anthropic-federation-sdk.test.ts` | no | unchanged reviewed | accepted v1.0.0/v0.99.2 runtime corpus baseline; native go test ./... regression; no v1.0.1 delta |
| 9 | `packages/ai/test/anthropic-federation.test.ts` | no | unchanged reviewed | accepted v1.0.0/v0.99.2 runtime corpus baseline; native go test ./... regression; no v1.0.1 delta |
| 10 | `packages/ai/test/anthropic-force-adaptive-thinking.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 11 | `packages/ai/test/anthropic-long-cache-retention-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 12 | `packages/ai/test/anthropic-mid-conversation-effort.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 13 | `packages/ai/test/anthropic-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 14 | `packages/ai/test/anthropic-opus-4-8-smoke.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 15 | `packages/ai/test/anthropic-sse-parsing.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 16 | `packages/ai/test/anthropic-strict-tool-schema.test.ts` | no | unchanged reviewed | accepted v1.0.0/v0.99.2 runtime corpus baseline; native go test ./... regression; no v1.0.1 delta |
| 17 | `packages/ai/test/anthropic-temperature-compat.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 18 | `packages/ai/test/anthropic-thinking-binding-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 19 | `packages/ai/test/anthropic-thinking-disable.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 20 | `packages/ai/test/anthropic-tool-name-normalization.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/anthropic existing upstream parity tests |
| 21 | `packages/ai/test/assistant-message-frame.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; inference/provider/openairesponses/message_wire_exclusion_v0991_test.go; types.go JSON tags |
| 22 | `packages/ai/test/azure-openai-base-url.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 23 | `packages/ai/test/azure-openai-responses-reasoning-replay.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 24 | `packages/ai/test/azure-openai-tool-choice.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 25 | `packages/ai/test/azure-utils.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 26 | `packages/ai/test/baseten-models.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 27 | `packages/ai/test/bedrock-cache-write-1h-cost.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 28 | `packages/ai/test/bedrock-convert-messages.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 29 | `packages/ai/test/bedrock-credentials.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 30 | `packages/ai/test/bedrock-custom-headers.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 31 | `packages/ai/test/bedrock-endpoint-resolution.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 32 | `packages/ai/test/bedrock-error-metadata.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 33 | `packages/ai/test/bedrock-models.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 34 | `packages/ai/test/bedrock-raw-stop-reason.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 35 | `packages/ai/test/bedrock-redacted-reasoning.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 36 | `packages/ai/test/bedrock-response-headers.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/bedrock existing upstream parity tests |
| 37 | `packages/ai/test/bedrock-thinking-payload.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 38 | `packages/ai/test/bedrock-utils.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 39 | `packages/ai/test/cache-retention.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 40 | `packages/ai/test/classifier-models.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 41 | `packages/ai/test/cloudflare-ai-binding.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 42 | `packages/ai/test/cloudflare-stream.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 43 | `packages/ai/test/cloudflare-utils.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 44 | `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 45 | `packages/ai/test/codex-websocket-cached-probe.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 46 | `packages/ai/test/compat-env.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 47 | `packages/ai/test/constrained-sampling.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 48 | `packages/ai/test/context-estimate.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 49 | `packages/ai/test/context-overflow.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 50 | `packages/ai/test/cross-provider-handoff.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 51 | `packages/ai/test/data/red-circle.png` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 52 | `packages/ai/test/empty.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 53 | `packages/ai/test/env-api-keys.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 54 | `packages/ai/test/error-body.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 55 | `packages/ai/test/event-stream.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 56 | `packages/ai/test/faux-provider.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 57 | `packages/ai/test/fetch-option.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 58 | `packages/ai/test/fireworks-model-generation.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 59 | `packages/ai/test/fireworks-models.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 60 | `packages/ai/test/generate-models-strict.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 61 | `packages/ai/test/github-copilot-anthropic.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 62 | `packages/ai/test/github-copilot-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 63 | `packages/ai/test/google-raw-stop-reason.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 64 | `packages/ai/test/google-shared-convert-tools.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 65 | `packages/ai/test/google-shared-gemini3-unsigned-tool-call.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 66 | `packages/ai/test/google-shared-image-tool-result-routing.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 67 | `packages/ai/test/google-shared-retry.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 68 | `packages/ai/test/google-shared-signed-empty-blocks.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 69 | `packages/ai/test/google-thinking-disable.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 70 | `packages/ai/test/google-thinking-level-map.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 71 | `packages/ai/test/google-thinking-signature.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 72 | `packages/ai/test/google-vertex-api-key-resolution.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/google existing upstream parity tests |
| 73 | `packages/ai/test/image-model-data.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; image_models_generated.go and image/openrouter tests |
| 74 | `packages/ai/test/image-tool-result.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; image_models_generated.go and image/openrouter tests |
| 75 | `packages/ai/test/images-models.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; image_models_generated.go and image/openrouter tests |
| 76 | `packages/ai/test/images.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; image_models_generated.go and image/openrouter tests |
| 77 | `packages/ai/test/interleaved-thinking.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 78 | `packages/ai/test/kimi-coding-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 79 | `packages/ai/test/lax-message-content.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 80 | `packages/ai/test/lazy-module-load.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 81 | `packages/ai/test/llama-cpp-classify.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; classifier_llama_cpp.go; tests/classifier_llama_cpp_v0991_test.go |
| 82 | `packages/ai/test/max-thinking.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 83 | `packages/ai/test/message-types.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; inference/provider/openairesponses/message_wire_exclusion_v0991_test.go; types.go JSON tags |
| 84 | `packages/ai/test/meta-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 85 | `packages/ai/test/mistral-http-transport.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/mistral existing upstream parity tests |
| 86 | `packages/ai/test/mistral-raw-stop-reason.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/mistral existing upstream parity tests |
| 87 | `packages/ai/test/mistral-reasoning-mode.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/mistral existing upstream parity tests |
| 88 | `packages/ai/test/mistral-tool-schema.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/mistral existing upstream parity tests |
| 89 | `packages/ai/test/model-catalog-types.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 90 | `packages/ai/test/model-data-validation.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 91 | `packages/ai/test/model-types.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 92 | `packages/ai/test/models-entry.test.ts` | no | unchanged reviewed | accepted v1.0.0/v0.99.2 runtime corpus baseline; native go test ./... regression; no v1.0.1 delta |
| 93 | `packages/ai/test/models-runtime.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 94 | `packages/ai/test/node-http-proxy.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 95 | `packages/ai/test/oauth-auth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 96 | `packages/ai/test/oauth-callback-server.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; oauth/openai_chatgpt_test.go |
| 97 | `packages/ai/test/oauth-device-code.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 98 | `packages/ai/test/oauth.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 99 | `packages/ai/test/openai-chatgpt-oauth.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; oauth/openai_chatgpt_test.go |
| 100 | `packages/ai/test/openai-codex-cache-affinity-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 101 | `packages/ai/test/openai-codex-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 102 | `packages/ai/test/openai-codex-stream.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 103 | `packages/ai/test/openai-completions-cache-control-format.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 104 | `packages/ai/test/openai-completions-empty-tools.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 105 | `packages/ai/test/openai-completions-prompt-cache.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 106 | `packages/ai/test/openai-completions-provider-stream-event.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; inference/provider/openai/provider_stream_event_v0991_test.go |
| 107 | `packages/ai/test/openai-completions-raw-stop-reason.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 108 | `packages/ai/test/openai-completions-reasoning-details.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 109 | `packages/ai/test/openai-completions-response-model.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 110 | `packages/ai/test/openai-completions-retry.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 111 | `packages/ai/test/openai-completions-thinking-as-text.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 112 | `packages/ai/test/openai-completions-thinking-token-budget.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 113 | `packages/ai/test/openai-completions-tool-choice.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 114 | `packages/ai/test/openai-completions-tool-result-images.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 115 | `packages/ai/test/openai-completions-vllm-priority.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openai existing upstream parity tests |
| 116 | `packages/ai/test/openai-responses-cache-affinity-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 117 | `packages/ai/test/openai-responses-chatgpt-sign-in.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 118 | `packages/ai/test/openai-responses-compat.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 119 | `packages/ai/test/openai-responses-empty-tool-result.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 120 | `packages/ai/test/openai-responses-foreign-toolcall-id.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 121 | `packages/ai/test/openai-responses-message-id.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 122 | `packages/ai/test/openai-responses-namespace.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 123 | `packages/ai/test/openai-responses-partial-json-cleanup.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 124 | `packages/ai/test/openai-responses-reasoning-replay-e2e.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 125 | `packages/ai/test/openai-responses-terminal-event.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 126 | `packages/ai/test/openai-responses-tool-result-images.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; inference/provider/openairesponses existing upstream parity tests |
| 127 | `packages/ai/test/openai-responses-usage-limit.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 128 | `packages/ai/test/opencode-provider-headers.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 129 | `packages/ai/test/openrouter-cache-control-models.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 130 | `packages/ai/test/openrouter-cache-write-repro.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 131 | `packages/ai/test/openrouter-images.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; image_models_generated.go and image/openrouter tests |
| 132 | `packages/ai/test/openrouter-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 133 | `packages/ai/test/openrouter-reasoning-options.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 134 | `packages/ai/test/overflow.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 135 | `packages/ai/test/pi-messages.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 136 | `packages/ai/test/pre-generation-error.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 137 | `packages/ai/test/provider-error-body-passthrough.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 138 | `packages/ai/test/provider-error-body-regression.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 139 | `packages/ai/test/provider-retry.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 140 | `packages/ai/test/providers.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 141 | `packages/ai/test/qwen-token-plan-models.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 142 | `packages/ai/test/radius-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 143 | `packages/ai/test/radius-provider.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 144 | `packages/ai/test/reasoning-options.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 145 | `packages/ai/test/responseid.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 146 | `packages/ai/test/retry.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 147 | `packages/ai/test/sampling-options.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 148 | `packages/ai/test/scratch.ts` | no | support/fixture | retained source/support inventory; native HTTP/fixture equivalent, not an executable test count |
| 149 | `packages/ai/test/stream.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 150 | `packages/ai/test/supports-xhigh.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 151 | `packages/ai/test/system-message-replay.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 152 | `packages/ai/test/telemetry-options.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 153 | `packages/ai/test/text.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 154 | `packages/ai/test/together-models.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 155 | `packages/ai/test/tokens.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 156 | `packages/ai/test/tool-call-id-normalization.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 157 | `packages/ai/test/tool-call-without-result.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 158 | `packages/ai/test/total-tokens.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 159 | `packages/ai/test/transcript-tool-changes.test.ts` | yes | implemented/adapted | docs/v101/changed-tests-crosswalk.md specific production/catalog fixtures |
| 160 | `packages/ai/test/transform-messages-copilot-openai-to-anthropic.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 161 | `packages/ai/test/typesafe-system-one.test.ts` | no | unchanged: ported | Accepted earlier crosswalk retained; classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 162 | `packages/ai/test/unicode-surrogate.test.ts` | no | unchanged: reviewed/n/a | Accepted earlier crosswalk retained; not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 163 | `packages/ai/test/uuid.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 164 | `packages/ai/test/validation.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 165 | `packages/ai/test/xai-oauth.test.ts` | no | unchanged: covered-existing | Accepted earlier crosswalk retained; oauth package deterministic tests; provider-specific files under oauth/ |
| 166 | `packages/ai/test/xai-responses.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 167 | `packages/ai/test/xhigh.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 168 | `packages/ai/test/xiaomi-models.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 169 | `packages/ai/test/xiaomi-token-plan-ams-anthropic-empty-signature-smoke.test.ts` | no | credential-only remainder | portable provider/request metadata covered by native deterministic suites; live upstream credential outcome not run |
| 170 | `packages/ai/test/zai-coding-plan-models.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
| 171 | `packages/ai/test/zen.test.ts` | no | unchanged: unchanged-corpus | Accepted earlier crosswalk retained; retained upstream corpus baseline; no v0.99.1 changed-test delta |
