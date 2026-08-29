# v0.84.4 release ledger

Audit target: official `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.84.4`, SHA `b79e4cc834970cca69daebffab7df1da7d1e52c4`, published 2026-08-28T22:05:13.974Z.

Previous accepted upstream baseline: `v0.84.3`, SHA `4e58f324fae8ebfa98a3d45181fb248072a2afac`. Previous accepted Go baseline: `e25cd2e0b454767c35ac53831d2c1a5bb4641299`.

Exact artifacts: source checkout `/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4`; npm package `/workspace/tmp/pi-ai-0844-npm/package`; npm SHA-256 `dfd3c929cee5a7387199a0a24dfc1be2096f1ea8f59ffb8285198a0ed01ebf93`.

## Changed-path matrix (15 `packages/ai` paths)

Exact command: `git diff --name-status v0.84.3..v0.84.4 -- packages/ai`.

| Status | Upstream path | Disposition | Go evidence / rationale |
| --- | --- | --- | --- |
| `M` | `packages/ai/CHANGELOG.md` | N/A/docs/package metadata | Recorded in RELEASE; no Go runtime surface beyond audited deltas. |
| `M` | `packages/ai/package.json` | N/A/package metadata | Recorded in RELEASE with npm tarball/version/hash. |
| `M` | `packages/ai/scripts/generate-models.ts` | Implemented/adapted | Go generator and regeneration gate updated for v0.84.4 catalog, OpenRouter reasoning metadata, Cloudflare Gateway mirrored Workers AI models, and generated text/image output. |
| `A` | `packages/ai/scripts/openrouter-reasoning-options.ts` | Implemented/adapted | OpenRouter supported_efforts/default reasoning metadata is represented as generated thinkingLevelMap and verified by catalog/request tests. |
| `M` | `packages/ai/src/api/mistral-conversations.ts` | Implemented | Mistral streaming now merges fragmented indexed tool-call chunks even when later fragments omit id and have empty function name; raw SSE test added. |
| `M` | `packages/ai/src/api/openai-completions.ts` | Implemented | Explicit toolChoice:"none" serializes without tools; streamed reasoning.text/reasoning.summary/encrypted reasoning_details merge into one thinkingSignature and replay once; raw SSE/payload tests added. |
| `M` | `packages/ai/src/image-models.generated.ts` | Implemented mechanically | Image catalog regenerated to 50 OpenRouter image models and exact regeneration/fault gates cover +5 additions. |
| `M` | `packages/ai/src/providers/cloudflare-ai-gateway.ts` | Implemented mechanically/adapted | Generated catalog mirrors tool-capable Workers AI models into Cloudflare Gateway using workers-ai/ prefix and /compat endpoint with dedupe/session-affinity evidence. |
| `M` | `packages/ai/src/types.ts` | Implemented/adapted | OpenRouter reasoning metadata effects are represented in Go model thinkingLevelMap/compat fields; no separate public type is needed beyond existing Go structures. |
| `M` | `packages/ai/test/fireworks-models.test.ts` | Implemented | Catalog tests verify changed Fireworks model set and removed router while retaining env-key compatibility. |
| `M` | `packages/ai/test/mistral-http-transport.test.ts` | Implemented | Production SSE parser test covers fragmented indexed tool-call chunks. |
| `M` | `packages/ai/test/openai-completions-reasoning-details.test.ts` | Implemented | Production SSE/payload tests cover adjacent reasoning detail merge/order/signature replay and no duplicate reasoning_details. |
| `M` | `packages/ai/test/openai-completions-tool-choice.test.ts` | Implemented | Payload test covers explicit tool_choice none with no tools. |
| `A` | `packages/ai/test/openrouter-reasoning-options.test.ts` | Implemented/adapted | Generated thinkingLevelMap and OpenAI request payload semantics cover supported_efforts mandatory/optional/off behavior. |
| `M` | `packages/ai/test/zai-coding-plan-models.test.ts` | Implemented | Catalog tests verify GLM-5.3 metadata/compat and existing ZAI coding plan provider/env tests remain. |

## Corpus and catalog evidence

- Whole corpus manifest: `docs/v0844-137-test-manifest.md` (137 rows, exact unique filename set, 6 changed-row markers).
- Text catalog: 1290 models across 39 providers and 9 APIs; exact provider/id pair parity with upstream.
- Image catalog: 50 OpenRouter image models, +5 vs v0.84.3 (`meta/muse-image` and Recraft v4 variants).
- Official package hash pinned: `dfd3c929cee5a7387199a0a24dfc1be2096f1ea8f59ffb8285198a0ed01ebf93`.

## Validation evidence

Candidate gate commands:

```text
python3 scripts/validate-test-manifest.py docs/v0844-137-test-manifest.md /workspace/tmp/v0844-test-files.txt
# manifest rows: 137; unique paths: 137; expected paths: 137; changed-row markers: 6; manifest validation passed

PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/providers
# upstream pairs: 1290; generated pairs: 1290; exact match

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/models.generated.ts PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/image-models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
# model regeneration metadata comparator passed; image model regeneration comparator passed

TMPDIR=/workspace/tmp go test ./...
make check GO_TMPDIR=/workspace/tmp
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go vet ./...
make staticcheck GO_TMPDIR=/workspace/tmp
make check-logging
make test-repro GO_TMPDIR=/workspace/tmp
```
