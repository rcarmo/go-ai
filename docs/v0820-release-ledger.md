# v0.82.0 release-only parity ledger

Scope: official `@earendil-works/pi-ai` release `v0.82.0` at `083e61621276bff9f6faefab87ce07fcd98734e2`, compared only against accepted `v0.81.1` at `20be4b18d4c57487f8993d2762bace129f0cf7c6`. Do not chase beyond tag.

Exact source checkouts used:

- old: `/workspace/tmp/pi-v0811` → `20be4b18d4c57487f8993d2762bace129f0cf7c6` (`v0.81.1`)
- new: `/workspace/tmp/pi-v0820` → `083e61621276bff9f6faefab87ce07fcd98734e2` (`v0.82.0`)

## Adopted Go-facing changes

- Text model catalog regenerated from exact v0.82.0 grouped JSON-backed provider data. `scripts/generate-models.go` and `scripts/compare-upstream-models.py` now flatten both v0.81 flat JSON shards and v0.82 API-grouped JSON shards. Comparator evidence: `upstream pairs: 1116`, `generated pairs: 1116`, exact match.
- Image model catalog regenerated from exact v0.82.0 `src/image-models.generated.ts`; comparator evidence: `upstream image pairs: 40`, `generated image pairs: 40`, exact match.
- Constrained sampling ported: `Tool.ConstrainedSampling`, JSON-schema strict conversion, grammar custom tool conversion, schema validation, and deterministic OpenAI Responses tests.
- Provider retries ported: `RetryProviderRequest` mirrors SDK-style retryability headers/statuses, provider retry-delay cap, abortable retry sleeps, and DNS/no-status transport retry with deterministic tests.
- OpenRouter OAuth ported: OAuth key exchange provider and local-server tests for success/error handling.
- Kimi Code subscription OAuth ported: device authorization, polling, refresh, trusted verification URI validation, and local-server tests.
- Catalog/provider metadata regressions updated for v0.82.0: 1116 text models, 40 image models, Together reasoning maps, OpenRouter Kimi/GLM pricing, xAI/OpenCode/OpenRouter/Vercel model additions/removals.

## Changed-path disposition matrix

| Status | Path | Disposition |
| --- | --- | --- |
| `M` | `CHANGELOG.md` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `M` | `README.md` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `M` | `package.json` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `M` | `scripts/generate-models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `scripts/model-data.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `A` | `scripts/models-dev-reasoning-options.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/api/anthropic-messages.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/azure-openai-responses.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/bedrock-converse-stream.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `A` | `src/api/constrained-sampling.ts` | Constrained sampling; Go Tool metadata, OpenAI Responses conversion, and deterministic tests added. |
| `M` | `src/api/google-generative-ai.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/google-shared.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/google-vertex.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/mistral-conversations.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/openai-codex-responses.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/openai-completions.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/openai-responses-shared.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/openai-responses.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `M` | `src/api/openrouter-images.ts` | Provider API runtime delta audited; applicable behavior covered by catalog metadata, constrained sampling, retry, or existing provider tests; JS-only wrapper details N/A. |
| `A` | `src/auth/oauth/kimi-coding.ts` | Kimi Code subscription OAuth; Go device-flow provider/refresh and local-server tests added. |
| `M` | `src/auth/oauth/load.ts` | Auth/env/type/export surface; Go-facing pieces covered by OAuth providers, env/provider constants, and Tool metadata. |
| `A` | `src/auth/oauth/openrouter.ts` | OpenRouter OAuth; Go OAuth provider/key exchange and local-server tests added. |
| `M` | `src/bun-oauth.ts` | Audited; no direct Go analogue beyond documented runtime/catalog changes. |
| `M` | `src/image-models.generated.ts` | Generated image catalog/data type metadata; Go image generator/catalog updated and exact image pair comparator added. |
| `A` | `src/model-catalog.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/models.generated.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/all.ts` | Provider catalog/runtime wiring; Go-facing changes covered by regenerated catalog, OAuth providers, and provider constants/env mapping. |
| `M` | `src/providers/amazon-bedrock.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/ant-ling.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/anthropic.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/azure-openai-responses.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/cerebras.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/cloudflare-ai-gateway.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/cloudflare-workers-ai.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/deepseek.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/fireworks.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/github-copilot.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/google-vertex.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/google.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/groq.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/huggingface.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/kimi-coding.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/kimi-coding.ts` | Provider catalog/runtime wiring; Go-facing changes covered by regenerated catalog, OAuth providers, and provider constants/env mapping. |
| `M` | `src/providers/minimax-cn.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/minimax.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/mistral.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/moonshotai-cn.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/moonshotai.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/nvidia.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/openai-codex.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/openai.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/opencode-go.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/opencode.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/openrouter-images.ts` | Provider catalog/runtime wiring; Go-facing changes covered by regenerated catalog, OAuth providers, and provider constants/env mapping. |
| `M` | `src/providers/openrouter.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/openrouter.ts` | Provider catalog/runtime wiring; Go-facing changes covered by regenerated catalog, OAuth providers, and provider constants/env mapping. |
| `M` | `src/providers/qwen-token-plan-cn.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/qwen-token-plan.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/together.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/vercel-ai-gateway.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/xai.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/xiaomi-token-plan-ams.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/xiaomi-token-plan-cn.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/xiaomi-token-plan-sgp.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/xiaomi.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/zai-coding-cn.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/providers/zai.models.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `src/types.ts` | Auth/env/type/export surface; Go-facing pieces covered by OAuth providers, env/provider constants, and Tool metadata. |
| `A` | `src/utils/provider-retry.ts` | Abortable provider retry/DNS transport retry; Go RetryProviderRequest and behavioral tests added. |
| `M` | `src/utils/retry.ts` | Audited; no direct Go analogue beyond documented runtime/catalog changes. |
| `M` | `test/anthropic-eager-tool-input-compat.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/azure-openai-base-url.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/bedrock-convert-messages.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/cache-retention.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/constrained-sampling.test.ts` | Constrained sampling; Go Tool metadata, OpenAI Responses conversion, and deterministic tests added. |
| `M` | `test/deferred-tools.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/google-shared-convert-tools.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/kimi-coding-oauth.test.ts` | Kimi Code subscription OAuth; Go device-flow provider/refresh and local-server tests added. |
| `M` | `test/mistral-tool-schema.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/model-catalog-types.test.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `test/model-data-validation.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/oauth-auth.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-codex-stream.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-completions-cache-control-format.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-completions-retry.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-completions-thinking-as-text.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-completions-tool-choice.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/openai-completions-tool-result-images.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/openrouter-cache-control-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/openrouter-oauth.test.ts` | OpenRouter OAuth; Go OAuth provider/key exchange and local-server tests added. |
| `A` | `test/provider-retry.test.ts` | Abortable provider retry/DNS transport retry; Go RetryProviderRequest and behavioral tests added. |
| `M` | `test/providers.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `A` | `test/reasoning-options.test.ts` | Generated text catalog/model-data/type metadata; Go generator/comparator updated for v0.82 grouped JSON data; regenerated exact text catalog and metadata regressions. |
| `M` | `test/retry.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/supports-xhigh.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |
| `M` | `test/together-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked live-credential/TS harness N/A. |

## Validation evidence

Passed before commit/push:

- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-v0820-json/providers python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0820/packages/ai/src/providers` → `1116`/`1116` exact match
- image pair comparator against `/workspace/tmp/pi-v0820/packages/ai/src/image-models.generated.ts` → `40`/`40` exact match
- `make check`
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...`
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`
- `go vet ./...`
- `make staticcheck`
- `make check-logging`
- `make test-repro`
