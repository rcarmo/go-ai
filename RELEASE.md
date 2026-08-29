# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.4`
- Upstream tag/SHA: `b79e4cc834970cca69daebffab7df1da7d1e52c4`
- Published: 2026-08-28T22:05:13.974Z
- Previous accepted baseline: `v0.84.3` / `4e58f324fae8ebfa98a3d45181fb248072a2afac`
- Accepted local baseline before this audit: `e25cd2e0b454767c35ac53831d2c1a5bb4641299`
- Exact upstream checkout used: `/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/pi-ai-0844-npm/package`
- Official npm tarball SHA-256: `dfd3c929cee5a7387199a0a24dfc1be2096f1ea8f59ffb8285198a0ed01ebf93`
- Detailed path matrix: `docs/v0844-release-ledger.md`
- Whole-corpus test crosswalk: `docs/v0844-137-test-manifest.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from official `v0.84.3` to official `v0.84.4`, no unpublished `main` changes.

Audited scope:

- 15 changed `packages/ai` paths: 5 source files, 2 scripts (1 modified + 1 added), 6 tests (5 modified + 1 added), 2 package/docs.
- Added test: `openrouter-reasoning-options.test.ts`.
- Whole test corpus: 137 `packages/ai/test/*.test.ts` files, fully classified with 0 unclassified rows.
- Text model catalog: 1290 models across 39 providers and 9 APIs, exact provider/id pair parity with upstream.
- Image model catalog: 50 OpenRouter image models, +5 vs v0.84.3 (`meta/muse-image` and Recraft v4 variants).

## v0.84.4 Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| Text catalog refresh | Implemented mechanically. `models_generated.go` regenerated from exact v0.84.4 source and official npm provider shards: 1290 models / 39 providers / 9 APIs. |
| Image catalog refresh | Implemented mechanically. `images/models_generated.go` regenerated from exact v0.84.4 source: 50 OpenRouter image models, including `meta/muse-image` and Recraft v4 variants. |
| Generator OpenRouter reasoning metadata | Implemented/adapted into generated Go `ThinkingLevelMap` and `OpenAICompletionsCompat` fields. OpenRouter `supported_efforts` maps to mandatory/optional/off behavior, including `off:null` for mandatory reasoning and `off:"none"` for optional disable semantics where applicable. |
| Cloudflare AI Gateway Workers AI mirror | Implemented mechanically in generated catalog: tool-capable Workers AI models are mirrored under Cloudflare AI Gateway with `workers-ai/` prefix, `/compat` endpoint, session-affinity compat, and deduped IDs. |
| OpenAI-compatible explicit `toolChoice:"none"` | Implemented in Chat Completions payload generation. Explicit `ToolChoiceNone` serializes as `tool_choice:"none"` even when no tools are present, while omitting `tools`. |
| OpenAI-compatible streamed reasoning details | Implemented v0.84.4 semantics: streamed `reasoning.text`, `reasoning.summary`, and encrypted details are replay metadata, adjacent text/summary details merge while preserving common metadata/order, and the final array is serialized once on a thinking block `thinkingSignature` with no duplicate tool-call `thoughtSignature`. Legacy stored encrypted tool-call signatures still replay. |
| Mistral fragmented tool-call chunks | Implemented indexed tool-call accumulation so later chunks that omit ID and provide an empty function name merge into the original call. |
| ZAI GLM-5.3 metadata | Implemented mechanically via regenerated catalog and tested for v0.84.4 metadata/compat. |
| Fireworks catalog changes | Implemented mechanically via regenerated catalog and tested for Kimi K2.7/K3 additions plus retired K2.6 router removal while preserving Fireworks env-key compatibility. |
| DeepSeek V4 vision metadata | Implemented mechanically via regenerated catalog and tested for `deepseek-v4-flash-vision-exp` multimodal metadata. |
| Tests and docs | Added/updated deterministic tests, exact 15-path ledger, exact 137-file corpus crosswalk, and this release record. |
| JS-only/runtime-specific surfaces | Narrowly N/A/adapted where no Go equivalent exists (private TS generator helper structure, JS type-only surfaces); observable generated catalog and provider wire behavior are covered in Go. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data \
  python3 scripts/compare-upstream-models.py /workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/providers
upstream pairs: 1290
generated pairs: 1290
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/models.generated.ts \
PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/image-models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data \
TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed
```

## Validation evidence

Candidate gate commands:

```text
python3 scripts/validate-test-manifest.py docs/v0844-137-test-manifest.md /workspace/tmp/v0844-test-files.txt
# manifest rows: 137; unique paths: 137; expected paths: 137; changed-row markers: 6; manifest validation passed

TMPDIR=/workspace/tmp go test ./...
make check GO_TMPDIR=/workspace/tmp
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go vet ./...
make staticcheck GO_TMPDIR=/workspace/tmp
make check-logging
make test-repro GO_TMPDIR=/workspace/tmp
```
