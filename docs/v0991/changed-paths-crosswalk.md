# v0.99.1 changed-path disposition crosswalk (169 rows)

| # | upstream | disposition | Go evidence / rationale |
|---:|---|---|---|
| 1 | `packages/ai/CHANGELOG.md` | documented/n/a | metadata/doc release artifact; no Go runtime behavior to execute directly |
| 2 | `packages/ai/README.md` | documented/n/a | metadata/doc release artifact; no Go runtime behavior to execute directly |
| 3 | `packages/ai/package.json` | documented/n/a | metadata/doc release artifact; no Go runtime behavior to execute directly |
| 4 | `packages/ai/scripts/generate-image-models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 5 | `packages/ai/scripts/generate-models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 6 | `packages/ai/scripts/model-data.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 7 | `packages/ai/scripts/openrouter-catalog.ts` | reviewed/n/a | reviewed as no direct Go runtime delta or already covered by package-level tests |
| 8 | `packages/ai/src/api/anthropic-messages.ts` | covered-existing | inference/provider/anthropic existing adaptive thinking/cache/oauth tests |
| 9 | `packages/ai/src/api/azure-openai-responses.ts` | implemented | inference/provider/openairesponses/responses.go; responses_v0991_chatgpt_stream_test.go; message_wire_exclusion_v0991_test.go |
| 10 | `packages/ai/src/api/bedrock-converse-stream.ts` | covered-existing | inference/provider/bedrock existing thinking/raw-stop/cache tests |
| 11 | `packages/ai/src/api/cloudflare-workers-ai-system-one.lazy.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 12 | `packages/ai/src/api/cloudflare-workers-ai-system-one.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 13 | `packages/ai/src/api/cloudflare.ts` | reviewed/n/a | reviewed as no direct Go runtime delta or already covered by package-level tests |
| 14 | `packages/ai/src/api/google-generative-ai.ts` | covered-existing | inference/provider/google existing thinking/raw-stop/signature tests |
| 15 | `packages/ai/src/api/google-vertex.ts` | covered-existing | inference/provider/google existing thinking/raw-stop/signature tests |
| 16 | `packages/ai/src/api/llama-cpp-classify.lazy.ts` | implemented | classifier_llama_cpp.go; tests/classifier_llama_cpp_v0991_test.go |
| 17 | `packages/ai/src/api/llama-cpp-classify.ts` | implemented | classifier_llama_cpp.go; tests/classifier_llama_cpp_v0991_test.go |
| 18 | `packages/ai/src/api/mistral-conversations.ts` | covered-existing | inference/provider/mistral existing reasoning/raw-stop/http tests |
| 19 | `packages/ai/src/api/openai-codex-responses.ts` | covered-existing | inference/provider/openaicodex existing v0.84/v0.85 tests plus provider-event surface in shared types |
| 20 | `packages/ai/src/api/openai-completions.ts` | implemented | inference/provider/openai/openai.go; provider_stream_event_v0991_test.go |
| 21 | `packages/ai/src/api/openai-responses-shared.ts` | implemented | inference/provider/openairesponses/responses.go; responses_v0991_chatgpt_stream_test.go; message_wire_exclusion_v0991_test.go |
| 22 | `packages/ai/src/api/openai-responses.ts` | implemented | inference/provider/openairesponses/responses.go; responses_v0991_chatgpt_stream_test.go; message_wire_exclusion_v0991_test.go |
| 23 | `packages/ai/src/api/openrouter-images.lazy.ts` | implemented/generated | image_models.go/image_models_generated.go plus existing openrouter image tests |
| 24 | `packages/ai/src/api/openrouter-images.ts` | implemented/generated | image_models.go/image_models_generated.go plus existing openrouter image tests |
| 25 | `packages/ai/src/api/pi-messages.ts` | reviewed/n/a | reviewed as no direct Go runtime delta or already covered by package-level tests |
| 26 | `packages/ai/src/api/simple-options.ts` | implemented | types.go/model_types.go/simple_options.go; message_wire_exclusion_v0991_test.go; tests/models_v0991_* |
| 27 | `packages/ai/src/api/system-one-shared.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 28 | `packages/ai/src/api/typesafe-system-one.lazy.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 29 | `packages/ai/src/api/typesafe-system-one.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 30 | `packages/ai/src/auth/helpers.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 31 | `packages/ai/src/auth/oauth/anthropic.ts` | covered-existing | inference/provider/anthropic existing adaptive thinking/cache/oauth tests |
| 32 | `packages/ai/src/auth/oauth/callback-server.ts` | implemented | oauth/openai_chatgpt.go; oauth/callback_server.go; oauth/openai_chatgpt_test.go |
| 33 | `packages/ai/src/auth/oauth/load.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 34 | `packages/ai/src/auth/oauth/openai-chatgpt.ts` | implemented | oauth/openai_chatgpt.go; oauth/callback_server.go; oauth/openai_chatgpt_test.go |
| 35 | `packages/ai/src/auth/oauth/openai-codex.ts` | covered-existing | inference/provider/openaicodex existing v0.84/v0.85 tests plus provider-event surface in shared types |
| 36 | `packages/ai/src/auth/oauth/openrouter.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 37 | `packages/ai/src/auth/oauth/radius.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 38 | `packages/ai/src/auth/resolve.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 39 | `packages/ai/src/auth/types.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 40 | `packages/ai/src/bun-oauth.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 41 | `packages/ai/src/cli.ts` | reviewed/n/a | reviewed as no direct Go runtime delta or already covered by package-level tests |
| 42 | `packages/ai/src/env-api-keys.ts` | reviewed/n/a | reviewed as no direct Go runtime delta or already covered by package-level tests |
| 43 | `packages/ai/src/image-models.generated.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 44 | `packages/ai/src/image-models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 45 | `packages/ai/src/images-api-registry.ts` | implemented/generated | image_models.go/image_models_generated.go plus existing openrouter image tests |
| 46 | `packages/ai/src/images-models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 47 | `packages/ai/src/images.ts` | implemented/generated | image_models.go/image_models_generated.go plus existing openrouter image tests |
| 48 | `packages/ai/src/index.ts` | implemented | types.go/model_types.go/simple_options.go; message_wire_exclusion_v0991_test.go; tests/models_v0991_* |
| 49 | `packages/ai/src/model-catalog.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 50 | `packages/ai/src/models-store.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 51 | `packages/ai/src/models.generated.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 52 | `packages/ai/src/models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 53 | `packages/ai/src/providers/all.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 54 | `packages/ai/src/providers/amazon-bedrock.models.ts` | covered-existing | inference/provider/bedrock existing thinking/raw-stop/cache tests |
| 55 | `packages/ai/src/providers/ant-ling.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 56 | `packages/ai/src/providers/anthropic.models.ts` | covered-existing | inference/provider/anthropic existing adaptive thinking/cache/oauth tests |
| 57 | `packages/ai/src/providers/azure-openai-responses.models.ts` | implemented | inference/provider/openairesponses/responses.go; responses_v0991_chatgpt_stream_test.go; message_wire_exclusion_v0991_test.go |
| 58 | `packages/ai/src/providers/baseten.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 59 | `packages/ai/src/providers/cerebras.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 60 | `packages/ai/src/providers/cloudflare-ai-gateway.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 61 | `packages/ai/src/providers/cloudflare-stream.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 62 | `packages/ai/src/providers/cloudflare-workers-ai.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 63 | `packages/ai/src/providers/cloudflare-workers-ai.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 64 | `packages/ai/src/providers/deepseek.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 65 | `packages/ai/src/providers/fireworks.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 66 | `packages/ai/src/providers/github-copilot.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 67 | `packages/ai/src/providers/google-vertex.models.ts` | covered-existing | inference/provider/google existing thinking/raw-stop/signature tests |
| 68 | `packages/ai/src/providers/google.models.ts` | covered-existing | inference/provider/google existing thinking/raw-stop/signature tests |
| 69 | `packages/ai/src/providers/groq.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 70 | `packages/ai/src/providers/huggingface.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 71 | `packages/ai/src/providers/images/register-builtins.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 72 | `packages/ai/src/providers/kimi-coding.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 73 | `packages/ai/src/providers/meta.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 74 | `packages/ai/src/providers/minimax-cn.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 75 | `packages/ai/src/providers/minimax.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 76 | `packages/ai/src/providers/mistral.models.ts` | covered-existing | inference/provider/mistral existing reasoning/raw-stop/http tests |
| 77 | `packages/ai/src/providers/moonshotai-cn.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 78 | `packages/ai/src/providers/moonshotai.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 79 | `packages/ai/src/providers/nvidia.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 80 | `packages/ai/src/providers/openai-codex.models.ts` | covered-existing | inference/provider/openaicodex existing v0.84/v0.85 tests plus provider-event surface in shared types |
| 81 | `packages/ai/src/providers/openai-codex.ts` | covered-existing | inference/provider/openaicodex existing v0.84/v0.85 tests plus provider-event surface in shared types |
| 82 | `packages/ai/src/providers/openai.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 83 | `packages/ai/src/providers/openai.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 84 | `packages/ai/src/providers/opencode-go.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 85 | `packages/ai/src/providers/opencode.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 86 | `packages/ai/src/providers/opencode.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 87 | `packages/ai/src/providers/openrouter-images.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 88 | `packages/ai/src/providers/openrouter.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 89 | `packages/ai/src/providers/openrouter.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 90 | `packages/ai/src/providers/qwen-token-plan-cn.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 91 | `packages/ai/src/providers/qwen-token-plan-individual.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 92 | `packages/ai/src/providers/qwen-token-plan.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 93 | `packages/ai/src/providers/radius.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 94 | `packages/ai/src/providers/together.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 95 | `packages/ai/src/providers/typesafe.models.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 96 | `packages/ai/src/providers/typesafe.ts` | implemented | classifier_runtime.go; tests/classifier_runtime_v0991_test.go |
| 97 | `packages/ai/src/providers/vercel-ai-gateway.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 98 | `packages/ai/src/providers/vercel-ai-gateway.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 99 | `packages/ai/src/providers/xai.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 100 | `packages/ai/src/providers/xiaomi-token-plan-ams.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 101 | `packages/ai/src/providers/xiaomi-token-plan-cn.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 102 | `packages/ai/src/providers/xiaomi-token-plan-sgp.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 103 | `packages/ai/src/providers/xiaomi.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 104 | `packages/ai/src/providers/zai-coding-cn.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 105 | `packages/ai/src/providers/zai.models.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 106 | `packages/ai/src/types.ts` | implemented | types.go/model_types.go/simple_options.go; message_wire_exclusion_v0991_test.go; tests/models_v0991_* |
| 107 | `packages/ai/src/utils/headers.ts` | covered-existing | root utility/retry/header tests; no new Go transport-only gap |
| 108 | `packages/ai/src/utils/model-operations.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 109 | `packages/ai/src/utils/models-error.ts` | implemented/generated | models_generated.go/image_models_generated.go/classifier_models_generated.go; tests/models_v0991_*; scripts/validate-v0991-inventory.py |
| 110 | `packages/ai/src/utils/oauth-page.ts` | covered-existing | oauth provider tests plus oauth/openai_chatgpt_test.go for new ChatGPT flow |
| 111 | `packages/ai/src/utils/retry.ts` | covered-existing | root utility/retry/header tests; no new Go transport-only gap |
| 112 | `packages/ai/test/abort.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `abort.test.ts` |
| 113 | `packages/ai/test/anthropic-adaptive-thinking-models.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `anthropic-adaptive-thinking-models.test.ts` |
| 114 | `packages/ai/test/anthropic-cache-write-1h-cost.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `anthropic-cache-write-1h-cost.test.ts` |
| 115 | `packages/ai/test/anthropic-empty-thinking-signature-compat.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `anthropic-empty-thinking-signature-compat.test.ts` |
| 116 | `packages/ai/test/anthropic-oauth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `anthropic-oauth.test.ts` |
| 117 | `packages/ai/test/anthropic-sse-parsing.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `anthropic-sse-parsing.test.ts` |
| 118 | `packages/ai/test/azure-openai-base-url.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `azure-openai-base-url.test.ts` |
| 119 | `packages/ai/test/bedrock-raw-stop-reason.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `bedrock-raw-stop-reason.test.ts` |
| 120 | `packages/ai/test/cache-retention.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `cache-retention.test.ts` |
| 121 | `packages/ai/test/classifier-models.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `classifier-models.test.ts` |
| 122 | `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `cloudflare-workers-ai-system-one.test.ts` |
| 123 | `packages/ai/test/context-overflow.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `context-overflow.test.ts` |
| 124 | `packages/ai/test/cross-provider-handoff.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `cross-provider-handoff.test.ts` |
| 125 | `packages/ai/test/empty.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `empty.test.ts` |
| 126 | `packages/ai/test/fetch-option.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `fetch-option.test.ts` |
| 127 | `packages/ai/test/fireworks-model-generation.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `fireworks-model-generation.test.ts` |
| 128 | `packages/ai/test/fireworks-models.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `fireworks-models.test.ts` |
| 129 | `packages/ai/test/google-raw-stop-reason.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `google-raw-stop-reason.test.ts` |
| 130 | `packages/ai/test/image-model-data.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `image-model-data.test.ts` |
| 131 | `packages/ai/test/image-tool-result.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `image-tool-result.test.ts` |
| 132 | `packages/ai/test/images-models.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `images-models.test.ts` |
| 133 | `packages/ai/test/images.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `images.test.ts` |
| 134 | `packages/ai/test/llama-cpp-classify.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `llama-cpp-classify.test.ts` |
| 135 | `packages/ai/test/max-thinking.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `max-thinking.test.ts` |
| 136 | `packages/ai/test/mistral-http-transport.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `mistral-http-transport.test.ts` |
| 137 | `packages/ai/test/mistral-reasoning-mode.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `mistral-reasoning-mode.test.ts` |
| 138 | `packages/ai/test/model-data-validation.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `model-data-validation.test.ts` |
| 139 | `packages/ai/test/model-types.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `model-types.test.ts` |
| 140 | `packages/ai/test/models-runtime.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `models-runtime.test.ts` |
| 141 | `packages/ai/test/oauth-auth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `oauth-auth.test.ts` |
| 142 | `packages/ai/test/oauth-callback-server.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `oauth-callback-server.test.ts` |
| 143 | `packages/ai/test/openai-chatgpt-oauth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-chatgpt-oauth.test.ts` |
| 144 | `packages/ai/test/openai-codex-oauth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-codex-oauth.test.ts` |
| 145 | `packages/ai/test/openai-codex-stream.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-codex-stream.test.ts` |
| 146 | `packages/ai/test/openai-completions-prompt-cache.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-completions-prompt-cache.test.ts` |
| 147 | `packages/ai/test/openai-completions-provider-stream-event.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-completions-provider-stream-event.test.ts` |
| 148 | `packages/ai/test/openai-completions-tool-choice.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-completions-tool-choice.test.ts` |
| 149 | `packages/ai/test/openai-responses-chatgpt-sign-in.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-responses-chatgpt-sign-in.test.ts` |
| 150 | `packages/ai/test/openai-responses-compat.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-responses-compat.test.ts` |
| 151 | `packages/ai/test/openai-responses-terminal-event.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-responses-terminal-event.test.ts` |
| 152 | `packages/ai/test/openai-responses-usage-limit.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openai-responses-usage-limit.test.ts` |
| 153 | `packages/ai/test/openrouter-images.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openrouter-images.test.ts` |
| 154 | `packages/ai/test/openrouter-oauth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `openrouter-oauth.test.ts` |
| 155 | `packages/ai/test/pi-messages.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `pi-messages.test.ts` |
| 156 | `packages/ai/test/provider-error-body-passthrough.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `provider-error-body-passthrough.test.ts` |
| 157 | `packages/ai/test/providers.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `providers.test.ts` |
| 158 | `packages/ai/test/radius-oauth.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `radius-oauth.test.ts` |
| 159 | `packages/ai/test/retry.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `retry.test.ts` |
| 160 | `packages/ai/test/sampling-options.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `sampling-options.test.ts` |
| 161 | `packages/ai/test/stream.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `stream.test.ts` |
| 162 | `packages/ai/test/supports-xhigh.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `supports-xhigh.test.ts` |
| 163 | `packages/ai/test/telemetry-options.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `telemetry-options.test.ts` |
| 164 | `packages/ai/test/together-models.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `together-models.test.ts` |
| 165 | `packages/ai/test/tokens.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `tokens.test.ts` |
| 166 | `packages/ai/test/tool-call-without-result.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `tool-call-without-result.test.ts` |
| 167 | `packages/ai/test/total-tokens.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `total-tokens.test.ts` |
| 168 | `packages/ai/test/typesafe-system-one.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `typesafe-system-one.test.ts` |
| 169 | `packages/ai/test/unicode-surrogate.test.ts` | covered-test-crosswalk | covered in changed-tests/corpus crosswalk for `unicode-surrogate.test.ts` |
