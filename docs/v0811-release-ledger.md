# v0.81.1 release-only parity ledger

Scope: official `@earendil-works/pi-ai` release `v0.81.1` at `20be4b18d4c57487f8993d2762bace129f0cf7c6`, compared only against accepted `v0.80.10` at `8dc78834cde4e329284cf505f9e3f99763df5529`. Do not chase beyond tag.

Exact source checkouts used:

- old: `/workspace/tmp/pi-v08010` → `8dc78834cde4e329284cf505f9e3f99763df5529` (`v0.80.10`)
- new: `/workspace/tmp/pi-v0811` → `20be4b18d4c57487f8993d2762bace129f0cf7c6` (`v0.81.1`)

## Adopted Go-facing changes

- Text model catalog regenerated from exact v0.81.1 JSON-backed provider data. Upstream `models.generated.ts` now references provider JSON shards, so `scripts/generate-models.go` and `scripts/compare-upstream-models.py` support `PI_AI_MODEL_DATA_DIR` for exact tag-generated JSON data. Comparator evidence: `upstream pairs: 1103`, `generated pairs: 1103`, exact match.
- Image model catalog regenerated from exact v0.81.1 `src/image-models.generated.ts`; added `scripts/generate-image-models.py`. Comparator evidence: `upstream image pairs: 39`, `generated image pairs: 39`, exact match.
- New Qwen Token Plan providers ported with provider constants/env keys: `qwen-token-plan` / `QWEN_TOKEN_PLAN_API_KEY`, `qwen-token-plan-cn` / `QWEN_TOKEN_PLAN_CN_API_KEY`, plus deterministic inclusion/exclusion tests for text-vs-image model lists.
- Shared utilities ported: `UUIDv7()` and `ContentText(...)`, with deterministic layout/monotonic and text-extraction tests.
- Retry runtime semantics ported: `RetryAssistantCall` reports aborted retry attempts as unsuccessful and normalizes aborted backoff to an aborted assistant message, with deterministic tests.
- OpenCode Go Responses and generated Gemini/OpenRouter/Vercel/Qwen metadata are covered by regenerated catalog plus metadata regressions (`1103` total, Laguna additions, OpenRouter Kimi/GLM pricing, OpenCode Go Responses).

## Changed-path disposition matrix

| Status | Path | Disposition |
| --- | --- | --- |
| `M` | `CHANGELOG.md` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `M` | `README.md` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `M` | `package.json` | Docs/package/build metadata; recorded, no Go runtime delta. |
| `A` | `scripts/check-model-data.ts` | Generated model-data validation; Go generator/comparator updated for JSON-backed shards; regression coverage added. |
| `M` | `scripts/generate-image-models.ts` | Image catalog/generator validation; regenerated Go image catalog to exact 39/39 and updated tests. |
| `M` | `scripts/generate-models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `A` | `scripts/model-data.ts` | Generated model-data validation; Go generator/comparator updated for JSON-backed shards; regression coverage added. |
| `M` | `src/api/openai-codex-responses.ts` | Provider API runtime delta; audited for Go equivalent. Existing providers already cover request semantics except catalog-driven metadata; retry/uuid utility deltas ported where applicable. |
| `M` | `src/api/openai-completions.ts` | Provider API runtime delta; audited for Go equivalent. Existing providers already cover request semantics except catalog-driven metadata; retry/uuid utility deltas ported where applicable. |
| `M` | `src/api/pi-messages.ts` | Provider API runtime delta; audited for Go equivalent. Existing providers already cover request semantics except catalog-driven metadata; retry/uuid utility deltas ported where applicable. |
| `M` | `src/auth/helpers.ts` | Type/export/auth plumbing; Go-facing pieces covered by provider constants/env, model types, and utility exports. |
| `M` | `src/env-api-keys.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/image-models.generated.ts` | Image catalog/generator validation; regenerated Go image catalog to exact 39/39 and updated tests. |
| `M` | `src/index.ts` | Type/export/auth plumbing; Go-facing pieces covered by provider constants/env, model types, and utility exports. |
| `M` | `src/models-store.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/models.generated.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/all.ts` | Provider runtime/catalog wiring; Go-facing provider IDs/env/catalog metadata ported or marked JS-only registration plumbing. |
| `M` | `src/providers/amazon-bedrock.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/amazon-bedrock.ts` | Provider runtime/catalog wiring; Go-facing provider IDs/env/catalog metadata ported or marked JS-only registration plumbing. |
| `M` | `src/providers/ant-ling.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/anthropic.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/azure-openai-responses.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/cerebras.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/cloudflare-ai-gateway.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/cloudflare-workers-ai.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `A` | `src/providers/data-json.d.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/deepseek.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/fireworks.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/github-copilot.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/google-vertex.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/google.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/groq.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/huggingface.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/kimi-coding.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/minimax-cn.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/minimax.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/mistral.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/moonshotai-cn.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/moonshotai.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/nvidia.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/openai-codex.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/openai.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/opencode-go.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/opencode-go.ts` | Provider runtime/catalog wiring; Go-facing provider IDs/env/catalog metadata ported or marked JS-only registration plumbing. |
| `M` | `src/providers/opencode.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/openrouter.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `A` | `src/providers/qwen-token-plan-cn.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `A` | `src/providers/qwen-token-plan-cn.ts` | New Qwen Token Plan providers; provider constants/env keys and model omission/inclusion tests added. |
| `A` | `src/providers/qwen-token-plan.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `A` | `src/providers/qwen-token-plan.ts` | New Qwen Token Plan providers; provider constants/env keys and model omission/inclusion tests added. |
| `M` | `src/providers/radius-config.ts` | Provider runtime/catalog wiring; Go-facing provider IDs/env/catalog metadata ported or marked JS-only registration plumbing. |
| `M` | `src/providers/together.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/vercel-ai-gateway.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/xai.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/xiaomi-token-plan-ams.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/xiaomi-token-plan-cn.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/xiaomi-token-plan-sgp.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/xiaomi.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/zai-coding-cn.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/providers/zai.models.ts` | Text catalog/generator metadata; regenerated Go catalog from exact tag JSON data; exact 1103/1103 comparator and metadata tests. |
| `M` | `src/types.ts` | Type/export/auth plumbing; Go-facing pieces covered by provider constants/env, model types, and utility exports. |
| `M` | `src/utils/overflow.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/utils/retry.ts` | Retry runtime semantics; ported RetryAssistantCall abort reporting and tests. |
| `A` | `src/utils/text.ts` | Shared text extraction utility; ported as goai.ContentText with deterministic tests. |
| `A` | `src/utils/uuid.ts` | Shared UUIDv7 utility; ported as goai.UUIDv7 with monotonic/layout regression. |
| `M` | `test/abort.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/anthropic-adaptive-thinking-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/anthropic-empty-thinking-signature-compat.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/anthropic-force-adaptive-thinking.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/context-overflow.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/cross-provider-handoff.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/empty.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `A` | `test/image-model-data.test.ts` | Image catalog/generator validation; regenerated Go image catalog to exact 39/39 and updated tests. |
| `M` | `test/image-tool-result.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `A` | `test/model-data-validation.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/openai-completions-tool-choice.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/providers.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `A` | `test/qwen-token-plan-models.test.ts` | New Qwen Token Plan providers; provider constants/env keys and model omission/inclusion tests added. |
| `M` | `test/retry.test.ts` | Retry runtime semantics; ported RetryAssistantCall abort reporting and tests. |
| `M` | `test/stream.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/supports-xhigh.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `A` | `test/text.test.ts` | Shared text extraction utility; ported as goai.ContentText with deterministic tests. |
| `M` | `test/tokens.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/tool-call-without-result.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/total-tokens.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `M` | `test/unicode-surrogate.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live credential tests N/A. |
| `A` | `test/uuid.test.ts` | Shared UUIDv7 utility; ported as goai.UUIDv7 with monotonic/layout regression. |
| `M` | `tsconfig.build.json` | Docs/package/build metadata; recorded, no Go runtime delta. |

## Validation evidence

Passed before commit/push:

- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-v0811-json/providers python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0811/packages/ai/src/providers` → `1103`/`1103` exact match
- image pair comparator against `/workspace/tmp/pi-v0811/packages/ai/src/image-models.generated.ts` → `39`/`39` exact match
- `make check`
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...`
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`
- `go vet ./...`
- `make staticcheck`
- `make check-logging`
- `make test-repro`
