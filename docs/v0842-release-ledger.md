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
| `packages/ai/CHANGELOG.md`, `packages/ai/package.json` | N/A/docs/package metadata | Recorded in `RELEASE.md`; no Go runtime behavior beyond audited source/catalog/test deltas. |
| `packages/ai/scripts/generate-models.ts` | Adopted mechanically | Go generator updated for new compat field `supportsAdditionalTools`; exact v0.84.2 source + npm provider data regeneration proves final metadata. |
| `packages/ai/src/api/anthropic-messages.ts` | Implemented/adapted | Anthropic tools now use strict-converted schemas when `SupportsStrictTools` allows them, while retaining Go's existing Anthropic request architecture. |
| `packages/ai/src/api/bedrock-converse-stream.ts` | Implemented | Bedrock tool schemas use strict-converted parameters when supported; assistant tool-use replay recursively removes empty object keys. Tests: `bedrock/v0842_sanitization_test.go`. |
| `packages/ai/src/api/cloudflare-gateway-binding.ts` | N/A/JS-Workers-binding | This is a TypeScript Workers `env.AI.gateway().run()` fetch shim. Go has no Workers binding/fetch-injection surface; Cloudflare HTTPS gateway URL/auth/placeholder behavior remains covered by existing Go tests. |
| `packages/ai/src/api/constrained-sampling.ts` | Implemented | `schema_strict.go` implements strict JSON Schema conversion/fallback/reject behavior; provider conversions call it. Tests: `schema_strict_test.go`, provider strict-schema tests, existing constrained sampling tests. |
| `packages/ai/src/api/google-generative-ai.ts`, `packages/ai/src/api/google-shared.ts`, `packages/ai/src/api/google-vertex.ts` | Implemented | Google tool conversion uses strict schemas for Gemini 3+ strict sampling; finish reasons only upgrade tool calls to `toolUse` when mapped stop reason is `stop`. Regression added in `google/raw_stop_reason_upstream_test.go`. |
| `packages/ai/src/api/mistral-conversations.ts` | Adapted/implemented | Go already used direct HTTP/SSE, so TS SDK-removal/fetch-shim mechanics are N/A. Go-facing strict tool schema serialization is implemented and tested in `mistral/v0842_strict_schema_test.go`; existing Mistral request/retry/raw-stop tests remain passing. |
| `packages/ai/src/api/openai-codex-responses.ts` | Implemented/adapted | Codex terminal `end_turn` is preserved in `Message.EndTurn`; headers use `pi (...)` user-agent shape; Responses shared strict/deferred behavior is mirrored where Go Codex shares request conversion. Tests: `openaicodex/v0842_endturn_useragent_test.go`. |
| `packages/ai/src/api/openai-completions.ts` | Implemented | OpenAI-compatible tool parameter conversion uses strict schemas where requested/supported. Test: `openai/v0842_strict_schema_test.go`. |
| `packages/ai/src/api/openai-responses-shared.ts`, `packages/ai/src/api/openai-responses.ts` | Implemented | Responses strict schema conversion, `supportsAdditionalTools`, namespace capture/replay, and `end_turn` capture are ported. Test: `openairesponses/v0842_namespace_additional_tools_test.go`. |
| `packages/ai/src/auth/oauth/github-copilot.ts` | Implemented/adapted | Go Copilot policy enablement now processes models in concurrency-4 batches. Live policy endpoint remains credential/network-bound. |
| `packages/ai/src/image-models.generated.ts` | Adopted mechanically | `images/models_generated.go` regenerated from exact v0.84.2 source: 45 OpenRouter image models; portable regeneration gate now verifies image catalog equality. |
| `packages/ai/src/types.ts` | Implemented | Added `ToolCall.Namespace`, `ContentBlock.Namespace`, `Message.EndTurn`, and `OpenAIResponsesCompat.SupportsAdditionalTools` Go fields/generator support. |
| `packages/ai/src/utils/pi-user-agent.ts` | Implemented/adapted | Go Codex now emits a shared `pi (...)` user-agent shape from `codexPiUserAgent`; browser-safe TS loading is N/A to Go. |
| `packages/ai/src/utils/retry.ts` | Implemented | Added retryable assistant-error pattern for `exceeded request buffer limit while retrying upstream`. Test: `retry_assistant_test.go`. |
| `packages/ai/src/utils/validation.ts` | Implemented | `ValidateToolArguments` now treats optional non-nullable nulls as omissions before validation; nullable/reference nulls are preserved. Tests: `upstream_validation_v0842_test.go`. |
| Changed tests: `anthropic-auth-token`, `anthropic-eager-tool-input-compat`, `bedrock-convert-messages`, `constrained-sampling`, `context-overflow`, `deferred-tools`, `github-copilot-oauth`, `google-raw-stop-reason`, `lazy-module-load`, `mistral-raw-stop-reason`, `openai-codex-stream`, `openai-completions-tool-choice`, `openai-responses-compat`, `retry`, `stream`, `supports-xhigh`, `total-tokens`, `validation` | Classified in whole-corpus manifest | See `docs/v0842-131-test-manifest.md`; no changed upstream test remains unclassified. |
| New tests: `cloudflare-gateway-binding.test.ts`, `mistral-http-transport.test.ts`, `openai-responses-namespace.test.ts` | Classified/ported as applicable | Cloudflare Workers binding is N/A/JS-Workers-binding; Mistral HTTP transport is adapted/covered; OpenAI Responses namespace is ported. |

## Material delta disposition

| Upstream area | Disposition | Evidence |
| --- | --- | --- |
| Strict JSON-schema constrained sampling | Implemented | `schema_strict.go`, `schema_strict_test.go`, `openai/v0842_strict_schema_test.go`, `mistral/v0842_strict_schema_test.go`, `openairesponses/constrained_sampling_upstream_test.go` |
| Optional non-nullable null validation | Implemented | `context.go`, `upstream_validation_v0842_test.go` |
| Responses namespace and additional tools | Implemented | `types.go`, `compat.go`, `scripts/generate-models.go`, `openairesponses/responses.go`, `openairesponses/v0842_namespace_additional_tools_test.go` |
| Codex end-turn/user-agent | Implemented | `openaicodex/codex.go`, `openaicodex/v0842_endturn_useragent_test.go` |
| Bedrock sanitization | Implemented | `bedrock/bedrock.go`, `bedrock/v0842_sanitization_test.go` |
| Google finish reason behavior | Implemented | `google/google.go`, `google/raw_stop_reason_upstream_test.go` |
| Copilot OAuth policy concurrency | Implemented/adapted | `oauth/github_copilot.go` |
| Cloudflare gateway binding | N/A/JS-Workers-binding | No Go Workers `env.AI` binding surface; HTTPS gateway behavior remains covered. |
| Text catalog | Implemented mechanically | `models_generated.go`: 1267 models / 39 providers; pair comparator exact 1267/1267; full metadata regeneration gate passed. |
| Image catalog | Implemented mechanically | `images/models_generated.go`: 45 image models; image regeneration gate added to `scripts/check-model-regeneration.sh` and passed. |
| Whole-corpus test crosswalk | Updated | `docs/v0842-131-test-manifest.md`: 131 rows, 103 deterministic/covered, 28 N/A/adapted, 0 unclassified. |

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
