# v0.99.1 changed-test crosswalk (58 rows)

| # | upstream | disposition | Go evidence / rationale |
|---:|---|---|---|
| 1 | `packages/ai/test/abort.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 2 | `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 3 | `packages/ai/test/anthropic-cache-write-1h-cost.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 4 | `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 5 | `packages/ai/test/anthropic-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 6 | `packages/ai/test/anthropic-sse-parsing.test.ts` | covered-existing | inference/provider/anthropic existing upstream parity tests |
| 7 | `packages/ai/test/azure-openai-base-url.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 8 | `packages/ai/test/bedrock-raw-stop-reason.test.ts` | covered-existing | inference/provider/bedrock existing upstream parity tests |
| 9 | `packages/ai/test/cache-retention.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 10 | `packages/ai/test/classifier-models.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 11 | `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 12 | `packages/ai/test/context-overflow.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 13 | `packages/ai/test/cross-provider-handoff.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 14 | `packages/ai/test/empty.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 15 | `packages/ai/test/fetch-option.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 16 | `packages/ai/test/fireworks-model-generation.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 17 | `packages/ai/test/fireworks-models.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 18 | `packages/ai/test/google-raw-stop-reason.test.ts` | covered-existing | inference/provider/google existing upstream parity tests |
| 19 | `packages/ai/test/image-model-data.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 20 | `packages/ai/test/image-tool-result.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 21 | `packages/ai/test/images-models.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 22 | `packages/ai/test/images.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 23 | `packages/ai/test/llama-cpp-classify.test.ts` | ported | classifier_llama_cpp.go; tests/classifier_llama_cpp_v0991_test.go |
| 24 | `packages/ai/test/max-thinking.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 25 | `packages/ai/test/mistral-http-transport.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 26 | `packages/ai/test/mistral-reasoning-mode.test.ts` | covered-existing | inference/provider/mistral existing upstream parity tests |
| 27 | `packages/ai/test/model-data-validation.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 28 | `packages/ai/test/model-types.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 29 | `packages/ai/test/models-runtime.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 30 | `packages/ai/test/oauth-auth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 31 | `packages/ai/test/oauth-callback-server.test.ts` | ported | oauth/openai_chatgpt_test.go |
| 32 | `packages/ai/test/openai-chatgpt-oauth.test.ts` | ported | oauth/openai_chatgpt_test.go |
| 33 | `packages/ai/test/openai-codex-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 34 | `packages/ai/test/openai-codex-stream.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 35 | `packages/ai/test/openai-completions-prompt-cache.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 36 | `packages/ai/test/openai-completions-provider-stream-event.test.ts` | ported | inference/provider/openai/provider_stream_event_v0991_test.go |
| 37 | `packages/ai/test/openai-completions-tool-choice.test.ts` | covered-existing | inference/provider/openai existing upstream parity tests |
| 38 | `packages/ai/test/openai-responses-chatgpt-sign-in.test.ts` | ported | inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 39 | `packages/ai/test/openai-responses-compat.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 40 | `packages/ai/test/openai-responses-terminal-event.test.ts` | covered-existing | inference/provider/openairesponses existing upstream parity tests |
| 41 | `packages/ai/test/openai-responses-usage-limit.test.ts` | ported | inference/provider/openairesponses/responses_v0991_chatgpt_stream_test.go |
| 42 | `packages/ai/test/openrouter-images.test.ts` | covered-existing | image_models_generated.go and image/openrouter tests |
| 43 | `packages/ai/test/openrouter-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 44 | `packages/ai/test/pi-messages.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 45 | `packages/ai/test/provider-error-body-passthrough.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 46 | `packages/ai/test/providers.test.ts` | ported | model_types.go; tests/models_v0991_typed_lookup_test.go; tests/models_v0991_typed_registry_test.go; scripts/validate-v0991-inventory.py |
| 47 | `packages/ai/test/radius-oauth.test.ts` | covered-existing | oauth package deterministic tests; provider-specific files under oauth/ |
| 48 | `packages/ai/test/retry.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 49 | `packages/ai/test/sampling-options.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 50 | `packages/ai/test/stream.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 51 | `packages/ai/test/supports-xhigh.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 52 | `packages/ai/test/telemetry-options.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 53 | `packages/ai/test/together-models.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 54 | `packages/ai/test/tokens.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 55 | `packages/ai/test/tool-call-without-result.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 56 | `packages/ai/test/total-tokens.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
| 57 | `packages/ai/test/typesafe-system-one.test.ts` | ported | classifier_runtime.go; classifier_models_generated.go; tests/classifier_runtime_v0991_test.go |
| 58 | `packages/ai/test/unicode-surrogate.test.ts` | reviewed/n/a | not a changed v0.99.1 Go runtime delta or covered by existing package/corpus tests |
