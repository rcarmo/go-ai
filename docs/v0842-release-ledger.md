# v0.84.2 release ledger

Audit target: official `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.84.2`, SHA `914cf1472e715297caa30db4b9535d534a9eb718`, published 2026-08-14T10:14:32Z.

Previous accepted baseline: `v0.84.1`, SHA `53fa77ccd8a279eb87e92294ef3687b03ff80112`.

Exact checkouts/artifacts:

- Source tag checkout: `/workspace/tmp/pi-v0842`
- Previous tag: `v0.84.1` in `/workspace/tmp/pi-v0842`
- Official npm package artifact: `/workspace/tmp/v0842/npm0842/package`
- Previous official npm package artifact: `/workspace/tmp/v0842/npm0841/package`
- Official npm tarball SHA-256: `0262785a76b0eb2eec596cd8a7ab2ee23eef89d2ef1bb1211c4f0a1944dacf41`

## Changed-path matrix (42 `packages/ai` paths)

Exact command: `git diff --name-status v0.84.1..v0.84.2 -- packages/ai`.

| Upstream path | Disposition | Go evidence / rationale |
| --- | --- | --- |
| `packages/ai/CHANGELOG.md` | N/A/docs metadata | Release notes only; behavior deltas captured in `RELEASE.md` and this ledger. |
| `packages/ai/package.json` | N/A/package metadata | Version/package metadata only; no Go runtime surface. |
| `packages/ai/scripts/generate-models.ts` | Adopted mechanically | Go generator updated for `supportsAdditionalTools`; exact v0.84.2 source + npm provider data regeneration proves final metadata. |
| `packages/ai/src/api/anthropic-messages.ts` | Implemented/adapted | Anthropic tools use strict-converted schemas when `SupportsStrictTools` allows them; existing Anthropic request architecture retained. |
| `packages/ai/src/api/bedrock-converse-stream.ts` | Implemented | Strict-converted tool schemas and recursive empty-key input sanitization; `bedrock/v0842_sanitization_test.go`. |
| `packages/ai/src/api/cloudflare-gateway-binding.ts` | N/A/JS-Workers-binding | Workers `env.AI.gateway().run()` fetch shim; Go has no Workers binding/fetch-injection surface. HTTPS gateway behavior remains covered. |
| `packages/ai/src/api/constrained-sampling.ts` | Implemented | `schema_strict.go`, `schema_strict_test.go`, provider strict-schema tests, and existing constrained sampling tests. |
| `packages/ai/src/api/google-generative-ai.ts` | Implemented | Google request path passes strict-support into tool conversion and preserves stop/error distinction. |
| `packages/ai/src/api/google-shared.ts` | Implemented | Strict schema parameters for Google tools and Gemini 3 strict sampling; covered by Google/provider tests. |
| `packages/ai/src/api/google-vertex.ts` | Implemented | Vertex shares Google strict conversion and stop/error behavior. |
| `packages/ai/src/api/mistral-conversations.ts` | Implemented/adapted | Direct HTTP/SSE Go transport now proves x-affinity, prompt cache key, byte-split UTF-8, abort/timeout, bounded 403 body, replay/tool fields, and cache-read usage in `mistral/v0842_wire_contract_test.go`. |
| `packages/ai/src/api/openai-codex-responses.ts` | Implemented | Codex `end_turn`, `pi (...)` user-agent, and strict JSON-schema tools. Tests: `openaicodex/v0842_endturn_useragent_test.go`, `openaicodex/v0842_strict_schema_test.go`. |
| `packages/ai/src/api/openai-completions.ts` | Implemented | Strict JSON-schema tool parameter conversion; DeepSeek mixed-case URL detection and `max_tokens` behavior tested in `goai_test.go`. |
| `packages/ai/src/api/openai-responses-shared.ts` | Implemented | Responses strict schema conversion, namespace capture/replay, and deferred `additional_tools` support. |
| `packages/ai/src/api/openai-responses.ts` | Implemented | `SupportsAdditionalTools` compat selection and Cloudflare strict tool behavior; `openairesponses/v0842_namespace_additional_tools_test.go`. |
| `packages/ai/src/auth/oauth/github-copilot.ts` | Implemented/adapted | Policy enablement batches concurrency at 4 with injectable httptest proof in `oauth/github_copilot_policy_test.go`. |
| `packages/ai/src/image-models.generated.ts` | Adopted mechanically | `images/models_generated.go` regenerated to 45 image models; clean and fault image gates documented. |
| `packages/ai/src/types.ts` | Implemented | Added `Namespace`, `EndTurn`, and `SupportsAdditionalTools` Go fields/generator support. |
| `packages/ai/src/utils/pi-user-agent.ts` | Implemented/adapted | Go Codex uses `codexPiUserAgent`; browser-safe TS import mechanics are N/A. |
| `packages/ai/src/utils/retry.ts` | Implemented | Request-buffer exhaustion wording is retryable; `retry_assistant_test.go`. |
| `packages/ai/src/utils/validation.ts` | Implemented | Optional non-nullable null omission before validation; `upstream_validation_v0842_test.go`. |
| `packages/ai/test/anthropic-auth-token.test.ts` | DETERMINISTIC-PORTED/covered | Anthropic auth/header tests plus Codex user-agent shape cover Go-facing header behavior. |
| `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | DETERMINISTIC-PORTED | Anthropic eager-input compat remains covered; strict schema conversion wired into Anthropic tools. |
| `packages/ai/test/bedrock-convert-messages.test.ts` | DETERMINISTIC-PORTED | Existing conversion tests plus `bedrock/v0842_sanitization_test.go` for empty-key sanitization. |
| `packages/ai/test/cloudflare-gateway-binding.test.ts` | N/A/JS-Workers-binding | Workers binding fetch shim has no Go equivalent; explicit N/A retained. |
| `packages/ai/test/constrained-sampling.test.ts` | DETERMINISTIC-PORTED | Strict schema conversion/fallback/reject and provider parameter conversion tests. |
| `packages/ai/test/context-overflow.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Existing deterministic overflow fixtures retained; live provider matrix additions remain N/A-live. |
| `packages/ai/test/deferred-tools.test.ts` | DETERMINISTIC-PORTED | Responses `additional_tools` and existing `tool_search` deferred behavior covered. |
| `packages/ai/test/github-copilot-oauth.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Concurrency cap executable proof; live GitHub policy endpoint remains N/A-live. |
| `packages/ai/test/google-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | Malformed function-call finish remains error even with tool-call block. |
| `packages/ai/test/lazy-module-load.test.ts` | N/A/JS-runtime | Go static provider linking has no JS lazy-module analogue. |
| `packages/ai/test/mistral-http-transport.test.ts` | DETERMINISTIC-PORTED/ADAPTED | Exact wire contract proven by httptest/raw stream tests in `mistral/v0842_wire_contract_test.go`. |
| `packages/ai/test/mistral-raw-stop-reason.test.ts` | DETERMINISTIC-PORTED | Existing raw stop reason test retained and full Mistral suite passes. |
| `packages/ai/test/openai-codex-stream.test.ts` | DETERMINISTIC-PORTED | Codex `end_turn`, user-agent, SSE/WS/cache/zstd tests. |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | DETERMINISTIC-PORTED | Strict tool schema conversion and existing tool-choice/tool-call tests. |
| `packages/ai/test/openai-responses-compat.test.ts` | DETERMINISTIC-PORTED | Responses compat plus `additional_tools`/strict Cloudflare behavior covered. |
| `packages/ai/test/openai-responses-namespace.test.ts` | DETERMINISTIC-PORTED | Namespace capture/persist/replay covered. |
| `packages/ai/test/retry.test.ts` | DETERMINISTIC-PORTED | Request-buffer retry wording covered. |
| `packages/ai/test/stream.test.ts` | N/A/live-provider | Live stream matrix requires credentials/network; deterministic streaming fixtures cover local behavior. |
| `packages/ai/test/supports-xhigh.test.ts` | DETERMINISTIC-PORTED | v0.84.2 thinking-level catalog changes adopted. |
| `packages/ai/test/total-tokens.test.ts` | DETERMINISTIC-PORTED / N/A-live additions | Deterministic token accounting retained; live additions N/A. |
| `packages/ai/test/validation.test.ts` | DETERMINISTIC-PORTED | Optional-null validation semantics covered. |

## Material delta disposition

| Upstream area | Disposition | Evidence |
| --- | --- | --- |
| Strict JSON-schema constrained sampling | Implemented | `schema_strict.go`, `schema_strict_test.go`, `openai/v0842_strict_schema_test.go`, `mistral/v0842_strict_schema_test.go`, `mistral/v0842_wire_contract_test.go`, `openairesponses/constrained_sampling_upstream_test.go` |
| Optional non-nullable null validation | Implemented | `context.go`, `upstream_validation_v0842_test.go` |
| Responses namespace and additional tools | Implemented | `types.go`, `compat.go`, `scripts/generate-models.go`, `openairesponses/responses.go`, `openairesponses/v0842_namespace_additional_tools_test.go` |
| Codex end-turn/user-agent | Implemented | `openaicodex/codex.go`, `openaicodex/v0842_endturn_useragent_test.go` |
| Bedrock sanitization | Implemented | `bedrock/bedrock.go`, `bedrock/v0842_sanitization_test.go` |
| Google finish reason behavior | Implemented | `google/google.go`, `google/raw_stop_reason_upstream_test.go` |
| Copilot OAuth policy concurrency | Implemented/adapted | `oauth/github_copilot.go`, `oauth/github_copilot_policy_test.go` |
| Cloudflare gateway binding | N/A/JS-Workers-binding | No Go Workers `env.AI` binding surface; HTTPS gateway behavior remains covered. |
| Text catalog | Implemented mechanically | `models_generated.go`: 1267 models / 39 providers; pair comparator exact 1267/1267; full metadata regeneration gate passed. |
| Image catalog | Implemented mechanically | `images/models_generated.go`: 45 image models; image regeneration gate added to `scripts/check-model-regeneration.sh` and passed. |
| Whole-corpus test crosswalk | Updated | `docs/v0842-131-test-manifest.md`: 131 well-formed rows, 131 unique upstream test paths, exact set equality with `/workspace/tmp/v0842_test_files.txt`, 21 changed-row markers, 103 deterministic/covered, 28 N/A/adapted, 0 unclassified. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0842/packages/ai/src/providers
upstream pairs: 1267
generated pairs: 1267
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/models.generated.ts PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/image-models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed

GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-v0842-gate-cache TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed

# Deliberate fault proof in isolated worktree copy:
# text fault: models_generated.go MaxTokens 384000 -> 384001
# image fault: images/models_generated.go Output []string{"image"} -> []string{"image", "text"}
# both failed with normalized regeneration diffs; retained logs under /workspace/tmp/go-ai-v0842-correction-fault-logs/
```

Artifact summary:

```text
0.84.2 text models/providers: 1267 / 39
0.84.2 generated provider/id pairs: 1267 / 1267 exact
0.84.2 image models/providers: 45 / 1
changed packages/ai paths: 42
changed test files: 21
whole test corpus: 131
```

## Validation gate

Final validation evidence before commit/push:

```text
scripts/validate-test-manifest.py docs/v0842-131-test-manifest.md /workspace/tmp/v0842_test_files.txt
# manifest rows: 131; unique paths: 131; expected paths: 131; changed-row markers: 21; manifest validation passed

TMPDIR=/workspace/tmp go test ./...
PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0842/packages/ai/src/providers
PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/models.generated.ts PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0842/packages/ai/src/image-models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0842/npm0842/package/dist/providers/data TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-v0842-gate-cache TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
```

Full gate passed:

```text
make check GO_TMPDIR=/workspace/tmp
# PASS: go test ./... -count=3, go vet ./..., staticcheck@v0.7.0, check-logging, check-model-regeneration

TMPDIR=/workspace/tmp go test -shuffle=on ./...
# PASS

TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
# PASS

TMPDIR=/workspace/tmp go vet ./...
# PASS

make staticcheck GO_TMPDIR=/workspace/tmp
# PASS

make check-logging
# PASS

make test-repro GO_TMPDIR=/workspace/tmp
# PASS: test-repro-fast + race; model regeneration found 1267 models across 39 providers and verified text + image catalogs
```
