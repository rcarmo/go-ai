# v0.80.6 adversarial parity ledger

Source audited: `@earendil-works/pi-ai` tag `v0.80.6` / `2b3fda9921b5590f285165287bd442a25817f17b`.

Fresh evidence from this pass:

- Source checkout: `/workspace/tmp/pi-src-2b3fda`, `git rev-parse HEAD` = `2b3fda9921b5590f285165287bd442a25817f17b`.
- Published package: `/workspace/tmp/dl/pi-ai-0.80.6.tgz`, SHA-256 `1aa05502e0c3d7d4e756ec089ace195fcd9befc9566898d6c870f7be1f7a12b5`.
- Upstream test inventory: `find packages/ai -name '*.test.ts' | wc -l` = `94`.
- Published declaration inventory: `148` `dist/**/*.d.ts` files, `627` top-level declaration export lines.
- Generated text catalog: Go `models_generated.go` header says `1057 models, 35 providers`, matching upstream `models.generated.js`.
- Validation run after audit: `make test-repro` passed (`go test`, `go vet`, `go build`, pinned `staticcheck@v0.7.0`, logging gate, race suite). `make test-deterministic` passed (`go test ./... -count=3`).

## Discrepancy ledger

| Upstream area / symbol or file | Go file/test evidence | Disposition |
|---|---|---|
| `src/types.ts` content/message/event/model/usage shapes | `types.go`, `events.go`, `context.go`, `goai_test.go`, `logic_audit_test.go` | Implemented idiomatically as exported Go structs/constants. TS discriminated unions map to Go typed structs plus event constructors. |
| `src/types.ts` `ThinkingLevel = ... | "max"` | `types.go`, `v0806_parity_test.go::TestUpstreamV0806ThinkingMaxSupportAndClamp` | Implemented. `ThinkingMax` is exported; clamp behavior mirrors upstream fallback to high for request effort. |
| `src/models.ts` cost tiers / context helpers | `types.go` `ModelCostTier`, `simple_options.go` `CalculateCost`, `v0806_parity_test.go` | Implemented. Highest matching `inputTokensAbove` tier is selected and covered. |
| `src/utils/estimate.ts`, `test/context-estimate.test.ts` | `context.go` `EstimateContextTokens`, `estimate_upstream_test.go`, `v0806_parity_test.go` | Implemented. Prefix/newer-message invalidation is covered. |
| `src/utils/error-body.ts`, provider error passthrough regressions | `error_body.go`, `error_body_test.go`, `provider_error_body_test.go`, provider packages | Implemented. Body extraction is bounded and provider errors preserve upstream details. |
| `src/utils/retry.ts`, `test/retry.test.ts` | `internal/retry`, `retry_assistant.go`, `retry_assistant_test.go` | Implemented. Retry helper and assistant retry paths are covered. |
| `src/api/openai-responses*.ts` including terminal event/cache-write usage | `inference/provider/openairesponses`, `openai_responses*_test.go`, `v0806_parity_test.go` | Implemented. Responses usage and terminal-event handling are covered. |
| `src/api/openai-completions.ts` tool choice/empty tools/image tool results | `inference/provider/openai`, `openai_completions*_test.go`, `upstream_validation_test.go` | Implemented. Request transforms have executable provider tests. |
| `src/api/anthropic-messages.ts` adaptive thinking/eager tool input/cache retention | `inference/provider/anthropic`, `anthropic*_test.go`, `models_catalog_upstream_test.go` | Implemented. Thinking metadata, cache pricing, eager tool input, and compatibility flags are covered. |
| `src/api/bedrock-converse-stream.ts` thinking payload | `inference/provider/bedrock`, `bedrock*_test.go` | Implemented. Bedrock request transforms and header filtering are covered. |
| `src/api/google-generative-ai.ts`, `src/api/google-vertex.ts`, shared Google transforms | `inference/provider/google`, `transform.go`, `fuzz_test.go` | Implemented. Gemini/Vertex models share Go provider code and transform tests. |
| `src/api/mistral-conversations.ts` | `inference/provider/mistral` tests | Implemented. Conversation request/stream mapping covered. |
| `src/api/openai-codex-responses.ts` WebSocket stream/cached probe/error retry | `inference/provider/openaicodex`, `transports/websocket`, `responseid_simulated_test.go` | Implemented. Codex-specific stream and retry/error paths covered. |
| `src/api/openrouter-images.ts`, image model registry | `images/api.go`, `images/models_generated.go`, `images_openrouter_upstream_test.go`, `images_test.go` | Implemented. No v0.80.6 image-registry delta found. |
| `src/api/simple-options.ts`, `transform-messages.ts` | `simple_options.go`, `transform.go`, `overflow_upstream_test.go`, `lax_message_content_upstream_test.go` | Implemented. Message/content normalization and overflow behavior covered. |
| `src/auth/*`, OAuth helpers | `oauth/`, `env.go`, `utils.go`, `githubcopilot` provider tests | Adapted. Go exposes direct provider/env/OAuth helpers rather than JS `CredentialStore` collection objects. Wire-visible auth behavior is covered. |
| `src/models.generated.ts`, `src/providers/*.models.ts` | `models_generated.go`, `models_test.go`, `models_catalog_upstream_test.go`, `scripts/generate-models.go` | Implemented mechanically. Header and tests verify 1057 models / 35 providers and spot-check changed v0.80.6 metadata. |
| `src/image-models.generated.ts`, images model files | `images/models_generated.go`, `images_openrouter_upstream_test.go` | Implemented; unchanged by v0.80.6 in Go-facing fields. |
| `src/legacy-api-aliases.ts`, `compat.ts`, lazy modules | `compat.go`, package layout, provider init packages | Not a Go wire-runtime gap. Go has idiomatic package imports/registrations; JS barrel/lazy alias names are documented architectural differences. |
| `src/utils/typebox-helpers.ts` | `types.go` `Tool.Parameters`, `context.go` validation | Not ported 1:1 by design. Go consumes JSON Schema bytes/maps directly; executable validation parity is in `upstream_validation_test.go`. |
| Cancellation/abort path, `test/abort.test.ts` | provider context handling tests, `harness.go`, transports | Implemented via `context.Context`; race suite passed. |
| Request timeout/max retry/error/cancellation across HTTP providers | provider packages, `error_body*`, `retry*`, `context_overflow_simulated_test.go` | Implemented and exercised by provider fake-server tests. |

## Upstream test-suite disposition

The exact upstream suite has 94 `packages/ai/**/*.test.ts` files. Deterministic behavior is either directly ported, covered by stronger Go provider/fake-server tests, or marked N/A when the upstream file exercises live service smoke/OAuth/browser/package-loading behavior that has no deterministic Go equivalent.

High-risk v0.80.6 additions/changes and evidence:

- `context-estimate.test.ts` → `estimate_upstream_test.go`, `v0806_parity_test.go`.
- `max-thinking.test.ts`, `supports-xhigh.test.ts` changes → `v0806_parity_test.go`, `models_catalog_upstream_test.go`.
- `error-body.test.ts`, `provider-error-body-*.test.ts` → `error_body_test.go`, `provider_error_body_test.go`.
- `retry.test.ts` → `internal/retry` tests and `retry_assistant_test.go`.
- OpenAI Responses/Completions terminal/tool-result changes → provider tests under `inference/provider/openai*` plus root upstream tests.
- Anthropic empty thinking/adaptive thinking changes → `inference/provider/anthropic` tests and catalog tests.
- Model runtime/catalog changes → `models_generated.go`, `models_test.go`, `models_catalog_upstream_test.go`.

No new concrete Go-facing discrepancy was found in this fresh pass beyond the known intentional JS packaging/auth collection/TypeBox surface differences already documented above.
