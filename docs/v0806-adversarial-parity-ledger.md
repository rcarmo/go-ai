# pi-ai v0.80.6 adversarial parity ledger

Audit source: `/workspace/tmp/pi-upstream` at `2b3fda9` (`v0.80.6`), plus the exact package tarball `/workspace/tmp/dl/pi-ai-0.80.6.tgz`.

## Mechanical catalog checks

| Upstream catalog/API | Rust/Go port file/test | Disposition/evidence |
| --- | --- | --- |
| `packages/ai/src/models.generated.ts` — 1,057 chat models across 35 providers | `models_generated.go`, `tests/models_catalog_upstream_test.go` | Regenerated from `2b3fda9`; diff against generated output is timestamp-only. |
| `packages/ai/src/image-models.generated.ts` — 35 OpenRouter image models | `images/models_generated.go`, `tests/images_test.go::TestBuiltinImageModels` | **Fixed in this audit.** Port had 34 models, missing `google/gemini-3.1-flash-lite-image`, `openai/gpt-image-1`, `openai/gpt-image-1-mini`, `openai/gpt-image-2`, and retained three stale `sourceful/*preview` IDs. Catalog now matches v0.80.6; regression checks count, new IDs, and removed IDs. |
| `packages/ai/src/providers/*.ts` / `*.models.ts` — provider registry | `types.go`, `models_generated.go`, provider packages under `inference/provider/*` | Chat model provider catalog matches generated upstream count/provider set; implemented wire protocols cover OpenAI completions/responses, Azure responses, Anthropic, Google, Bedrock, Mistral, OpenAI Codex, Gemini CLI, Faux. Model-only OpenAI-compatible providers are represented by catalog/API metadata. |
| `packages/ai/src/types.ts` — exported request/response/event/model fields | `types.go`, `events.go`, `simple_options.go`, `images/api.go` | Exported Go surface includes stream events, usage/cost/context fields, model thinking/cost tiers, image model fields, options hooks and cancellation contexts. |
| `packages/ai/src/utils/estimate.ts` / cost tiers | `context.go`, `tests/v0806_parity_test.go`, `tests/estimate_upstream_test.go`, `tests/tokens_simulated_test.go`, `tests/total_tokens_simulated_test.go` | Deterministic parity covered, including v0.80.6 highest matching cost tier and prefix-anchor context estimation. |
| `packages/ai/src/api/*` request transforms | `inference/provider/*/*.go`, provider-specific `*_upstream_test.go` files | File-by-file deterministic request transform tests cover implemented protocols: Anthropic, Bedrock, Google shared, Mistral, OpenAI completions/responses, Azure responses, Codex, image OpenRouter. |
| `packages/ai/src/utils/error-body.ts`, retry, abort/overflow/sanitize helpers | `error_body.go`, `retry.go`, `transports/sse`, `internal/jsonparse`, `transform.go`, corresponding tests | Error body passthrough, retryability, cancellation-to-`aborted`, Unicode surrogate sanitization, partial JSON cleanup and SSE parsing are covered by executable tests. |
| Upstream `packages/ai/test/*.test.ts` — 94 test files in v0.80.6 | Go test tree (`*_upstream_test.go`, simulated tests, provider tests) | Re-enumerated mechanically. Deterministic tests are ported or represented by local simulated equivalents; credential/e2e/smoke-only upstream tests remain intentionally not executed locally. New catalog regression closes the concrete image model drift found in this audit. |

## Concrete discrepancy fixed

- **Upstream file/symbol:** `packages/ai/src/image-models.generated.ts` / `IMAGE_MODELS.openrouter`
- **Port file/test:** `images/models_generated.go`; `tests/images_test.go::TestBuiltinImageModels`
- **Problem:** generated image catalog drifted from exact v0.80.6 tree.
- **Fix:** replaced the image model list with the 35-entry upstream catalog, added checks for the newly-added OpenAI/Gemini image IDs and removed stale Sourceful preview IDs.
- **Evidence:** `go test ./...`, `go vet ./...`, `go build ./...`, `staticcheck ./...`, and `scripts/check-logging.sh` pass.
