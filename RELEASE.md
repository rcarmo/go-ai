# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.3`
- Upstream tag/SHA: `4e58f324fae8ebfa98a3d45181fb248072a2afac`
- Published: 2026-08-24T11:09:57Z
- Previous accepted baseline: `v0.84.2` / `914cf1472e715297caa30db4b9535d534a9eb718`
- Accepted local baseline before this audit: `cdd8df962e2ba8070f1df7db6fe502b1305891a5`
- Exact upstream checkout used: `/workspace/tmp/pi-v0843`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/v0843/npm0843/package`
- Official npm tarball SHA-256: `9c40af2f43950f8e94e7bbcd0c1b3548f000972da00c4fb9c0d0529d4d7d5431`
- Detailed path matrix: `docs/v0843-release-ledger.md`
- Whole-corpus test crosswalk: `docs/v0843-136-test-manifest.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from official `v0.84.2` to official `v0.84.3`, no unpublished `main` changes.

Audited scope:

- 48 changed `packages/ai` paths: 19 source, 25 tests, 4 package/docs.
- Changed tests: 25 total, 20 modified plus 5 new (`azure-openai-tool-choice.test.ts`, `bedrock-redacted-reasoning.test.ts`, `bedrock-response-headers.test.ts`, `google-thinking-level-map.test.ts`, `zai-coding-plan-models.test.ts`).
- Whole test corpus: 136 `packages/ai/test/*.test.ts` files, fully classified with 0 unclassified rows.
- Text model catalog: 1312 models across 39 providers, exact provider/id pair parity with upstream.
- Image model catalog: 45 OpenRouter image models, unchanged from v0.84.2.

## v0.84.3 Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| Text catalog refresh | Implemented mechanically. `models_generated.go` regenerated from exact v0.84.3 source and official npm provider shards: 1312 models / 39 providers. |
| Image catalog | Verified unchanged at 45 image models; image regeneration comparator remains in `scripts/check-model-regeneration.sh`. |
| Generator/type compat | Implemented `ThinkingTokenBudgetField`, `$var:"thinking.budget"` support, and Anthropic fallback metadata generation. |
| Provider-neutral toolChoice | Implemented/adapted where Go exposes the field: Responses/Azure already serializes it; Codex simple/request path now honors it; Azure test added. |
| Default Pi User-Agent | Added shared `PiUserAgent` default and applied to HTTP adapters while preserving model/caller override precedence. |
| Anthropic server-side fallback | Implemented fallback request field, beta flag, and fallback pricing based on returned `message.model`; focused raw `streamAnthropic` HTTP/SSE test now proves `fallbacks`, `server-side-fallback-2026-07-01`, default/explicit User-Agent precedence, returned fallback model, and fallback pricing end-to-end. |
| Bedrock redacted reasoning / response hooks | Implemented preservation/finalization/replay of `redactedContent` as redacted thinking with base64 `thinkingSignature`; response callback adapted to modeled AWS SDK request-id metadata and covered for status 200/request-id and absent-request-id behavior. Raw Smithy gateway response headers remain unavailable through Go SDK modeled `ConverseStreamOutput`. |
| Google thinking level mapping | Implemented mapping resolution/error path and mapped budget lookup; tests added. |
| OpenAI-compatible thinking budgets | Implemented configurable `thinkingTokenBudgetField` variants and `thinking.budget` chat-template variable support. |
| xAI migration | Generated xAI models now use Responses API; Grok 4.6 metadata/reasoning coverage added, plus raw production `/responses` request tests for low/medium/high/xhigh mapping, encrypted reasoning include, endpoint, auth, and explicit User-Agent override while retaining the Grok 4.5 regression. |
| GitHub Copilot OAuth policy workflow | Implemented/adapted v0.84.3 catalog/policy workflow: known + tool-capable + unconfigured filtering, policy fallback for Individual accounts, no refresh-time catalog retry, 429 `Retry-After` policy retry, continuation after transport failure, 5s login policy retry budget, and returning credentials when policy enabling stops so caller persistence can proceed. |
| ZAI Coding Plan | Generated global/CN ZAI coding plan catalogs and env-key tests added. |
| JS-only/runtime-specific surfaces | Narrowly N/A/adapted where no Go equivalent exists (JS sleep helper internals, Cloudflare Workers binding, private TS generator policy, JS credential-store implementation details beyond Go's returned-credential boundary). |
| Upstream test corpus | Updated. `docs/v0843-136-test-manifest.md` has 136 well-formed rows, exact set equality with `/workspace/tmp/v0843/test_files.txt`, 25 changed-row markers, and 0 unclassified rows. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data \
  python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0843/packages/ai/src/providers
upstream pairs: 1312
generated pairs: 1312
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/models.generated.ts \
PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/image-models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data \
TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed

# Deliberate fault proof in isolated worktree copy:
# text fault: models_generated.go MaxTokens 384000 -> 384001
# image fault: images/models_generated.go Output []string{"image"} -> []string{"image", "text"}
# both failed with normalized regeneration diffs; retained logs under /workspace/tmp/go-ai-v0843-fault-logs/
```

## Validation evidence

Final gate passed before commit/push:

```text
scripts/validate-test-manifest.py docs/v0843-136-test-manifest.md /workspace/tmp/v0843/test_files.txt
# manifest rows: 136; unique paths: 136; expected paths: 136; changed-row markers: 25; manifest validation passed

PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0843/packages/ai/src/providers
# upstream pairs: 1312; generated pairs: 1312; exact match

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/models.generated.ts PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/image-models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
# model regeneration metadata comparator passed; image model regeneration comparator passed

# deliberate text/image faults failed as expected; logs under /workspace/tmp/go-ai-v0843-fault-logs/

TMPDIR=/workspace/tmp go test ./oauth ./inference/provider/anthropic ./inference/provider/openairesponses ./inference/provider/bedrock
# focused v0.84.3 correction packages PASS: Copilot OAuth, Anthropic fallback stream, xAI Grok 4.6 raw Responses, Bedrock modeled response hook

TMPDIR=/workspace/tmp go test ./...
make check GO_TMPDIR=/workspace/tmp
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go vet ./...
make staticcheck GO_TMPDIR=/workspace/tmp
make check-logging
make test-repro GO_TMPDIR=/workspace/tmp
# all PASS

```
