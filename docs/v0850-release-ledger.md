# v0.85.0 release ledger

Audit target: official upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.85.0`, SHA `107d79f11072bbc8a3a757ed7fd69596bee7d68c`, release commit `Release v0.85.0` authored/committed `2026-09-04T11:57:38+02:00`.

Previous accepted upstream baseline: `v0.84.4`, SHA `b79e4cc834970cca69daebffab7df1da7d1e52c4`. Previous accepted Go baseline before this audit: `abd95ba55b58b3986961b03fcc5c014d6d775c0c`.

## Exact artifacts and hashes

- Upstream checkout: `/workspace/tmp/pi-mono-audit` at exact tag `v0.85.0` / SHA `107d79f11072bbc8a3a757ed7fd69596bee7d68c`.
- Official npm tarball: `/workspace/tmp/pi-ai-0850/pi-ai-0.85.0.tgz`.
- Official npm tarball SHA-256: `46188bdacb555a07466a0111f3963f20932a16199e4d6cfb8d44a7fe5fc6e342`.
- Changed-path list: `/workspace/tmp/pi-ai-0850/changed-paths.txt`, 51 rows, SHA-256 `db461a56838926cf60d4ae0196ed98fcc215616dacff013ad8c235bb8ad9b83f`. Regenerated command: `git diff --name-status b79e4cc834970cca69daebffab7df1da7d1e52c4 107d79f11072bbc8a3a757ed7fd69596bee7d68c -- packages/ai`.
- Whole upstream test corpus: `/workspace/tmp/pi-ai-0850/test-corpus-142.txt`, 142 rows, SHA-256 `56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065`.
- Changed-test list: `/workspace/tmp/pi-ai-0850/changed-tests.txt`, 29 rows, SHA-256 `0b58c13688745fd74837bcefb868d2f5064649dcb4c57a5e134e08be0fd9d711` (28 live-corpus changed rows plus deleted `cloudflare-gateway-binding.test.ts`).
- Whole-corpus manifest: `docs/v0850-142-test-manifest.md`; validation command `python3 scripts/validate-test-manifest.py docs/v0850-142-test-manifest.md /workspace/tmp/pi-ai-0850/test-corpus-142.txt`.

## Changed-path matrix (51 executable rows)

| # | Status | Upstream path | Go disposition |
| ---: | --- | --- | --- |
| 1 | M | `CHANGELOG.md` | Release/package metadata documented in `RELEASE.md`; no Go runtime code. |
| 2 | M | `README.md` | Release/package metadata documented in `RELEASE.md`; no Go runtime code. |
| 3 | M | `package.json` | Release/package metadata documented in `RELEASE.md`; no Go runtime code. |
| 4 | M | `scripts/generate-models.ts` | Ported in `scripts/generate-models.go`; exact regeneration/fault gates required. |
| 5 | M | `src/api/anthropic-messages.ts` | Ported managed mid-conversation effort, beta headers/overrides, binding drop_block, result `ProviderThinkingLevel`, temperature omission, and catalog gates. |
| 6 | A | `src/api/cloudflare-ai-binding.ts` | Adapted in `cloudflare_ai_binding.go`: sentinel and early binding fetch validation; actual JS Workers `env.AI.fetch` runtime is N/A to Go. |
| 7 | D | `src/api/cloudflare-gateway-binding.ts` | Deleted upstream; superseded by Cloudflare AI binding helper/adaptation. |
| 8 | M | `src/api/openai-codex-responses.ts` | Covered terminal SSE without trailing blank line through Go SSE parser and Codex regression test. |
| 9 | M | `src/api/openai-completions.ts` | Ported vLLM `priority` request field from completions compat. |
| 10 | M | `src/api/openai-responses-shared.ts` | Ported `supportsMaxOutputTokens` request emission gate. |
| 11 | M | `src/api/openai-responses.ts` | Ported `supportsMaxOutputTokens` request emission gate. |
| 12 | M | `src/api/pi-messages.ts` | Ported `providerThinkingLevel` on `Message`; existing pi-messages terminal propagation test covers it. |
| 13 | M | `src/index.ts` | Exports mapped where Go has public analogues: assistant-message frames and Cloudflare binding helper. |
| 14 | M | `src/models.ts` | Regenerated into `models_generated.go` from exact v0.85.0 source/npm provider data; comparator `1336/1336` passed. |
| 15 | M | `src/providers/cloudflare-ai-gateway.ts` | Provider metadata/API deltas are generated/catalog-tested; no separate hand-coded provider module in Go. |
| 16 | M | `src/providers/faux.ts` | Provider metadata/API deltas are generated/catalog-tested; no separate hand-coded provider module in Go. |
| 17 | M | `src/providers/openrouter.ts` | Provider metadata/API deltas are generated/catalog-tested; no separate hand-coded provider module in Go. |
| 18 | M | `src/types.ts` | Ported type/compat fields: `ProviderThinkingLevel`, `VLLMPriority`, `SupportsMaxOutputTokens`, `SupportsMidConvoEffort`, Cloudflare binding adapter types. |
| 19 | A | `src/utils/assistant-message-frame.ts` | Ported in `assistant_message_frame.go` and deterministic frame/reduce tests. |
| 20 | M | `src/utils/node-http-proxy.ts` | Reviewed; no additional Go runtime change beyond generated catalog/tests. |
| 21 | M | `src/utils/retry.ts` | Ported v0.85.0 NO_PROXY/no_proxy matching including root/subdomain/bracketed IPv6. |
| 22 | M | `src/utils/uuid.ts` | Reviewed; no additional Go runtime change beyond generated catalog/tests. |
| 23 | M | `test/anthropic-auth-token.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 24 | M | `test/anthropic-cache-write-1h-cost.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 25 | A | `test/anthropic-mid-conversation-effort.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 26 | M | `test/anthropic-sse-parsing.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 27 | A | `test/anthropic-thinking-binding-e2e.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 28 | A | `test/assistant-message-frame.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 29 | M | `test/baseten-models.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 30 | A | `test/cloudflare-ai-binding.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 31 | D | `test/cloudflare-gateway-binding.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 32 | M | `test/constrained-sampling.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 33 | M | `test/generate-models-strict.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 34 | M | `test/github-copilot-anthropic.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 35 | M | `test/github-copilot-oauth.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 36 | M | `test/node-http-proxy.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 37 | M | `test/openai-codex-stream.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 38 | M | `test/openai-completions-cache-control-format.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 39 | M | `test/openai-completions-thinking-as-text.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 40 | M | `test/openai-completions-tool-choice.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 41 | M | `test/openai-completions-tool-result-images.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 42 | A | `test/openai-completions-vllm-priority.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 43 | M | `test/openai-responses-compat.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 44 | M | `test/openai-responses-namespace.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 45 | M | `test/openrouter-cache-control-models.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 46 | M | `test/pi-messages.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 47 | A | `test/pre-generation-error.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 48 | M | `test/qwen-token-plan-models.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 49 | M | `test/tool-call-id-normalization.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 50 | M | `test/uuid.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |
| 51 | M | `test/xai-responses.test.ts` | Mapped in `docs/v0850-142-test-manifest.md`. |

## Go implementation/fix/adaptation decisions

- Generated exact v0.85.0 text catalog: 1336 models across 39 providers, including Cloudflare AI Gateway `workers-ai/` route metadata, Claude Fable 5.1 managed effort compat, Baseten/Fireworks/Qwen/OpenRouter/xAI/ZAI changes, and stale Grok 4 removals.
- Generated image catalog check retained exact upstream parity through `scripts/check-model-regeneration.sh`.
- Added `OpenAICompletionsCompat.VLLMPriority` and request serialization as OpenAI chat `priority`.
- Added `OpenAIResponsesCompat.SupportsMaxOutputTokens` and omitted `max_output_tokens` only when compat explicitly disables it.
- Added public `Message.ProviderThinkingLevel` and Anthropic `SupportsMidConvoEffort` compat; Anthropic request builder now inserts historical/current effort markers, sends managed-effort beta headers, enables `block_binding.drop_block`, defaults top-level output effort to `high`, omits temperature, and records the active provider thinking level on results.
- Added Anthropic model-level/request-level `Anthropic-Beta` override behavior so configured beta headers replace generated defaults.
- Added `AssistantMessageFrame` encoder/reducer for compact stream frame serialization/reconstruction.
- Added optional timestamp support to `UUIDv7` while preserving existing no-arg behavior.
- Added v0.85.0 NO_PROXY matcher over Go retry transport, including exact root/subdomain behavior and bracketed IPv6 normalization before falling back to Go proxy defaults.
- Added Codex terminal SSE no-trailing-blank regression coverage; Go SSE parser already flushes final unterminated events.
- Added Cloudflare AI binding sentinel and early fetch-adapter validation. Actual JavaScript Workers `env.AI.fetch` transport is represented as an adapted public helper because Go has no native Worker FetchFunction runtime.
- Classified TS-only synchronous stream construction throws in `pre-generation-error.test.ts` as Go channel-adapted behavior: Go providers emit deterministic pre-dispatch `ErrorEvent` rather than throwing from stream construction.

## Local validation evidence

- `go test ./inference/provider/anthropic ./inference/provider/openai ./inference/provider/openairesponses ./inference/provider/openaicodex ./tests` — passed after provider patches.
- `go test ./...` — passed after Anthropic, Codex SSE, Cloudflare binding, catalog, proxy, UUID, vLLM, Responses changes.
- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0850/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-mono-audit/packages/ai/src/providers` — `upstream pairs: 1336`, `generated pairs: 1336`, exact match.
- `TMPDIR=/workspace/tmp GO_TMPDIR=/workspace/tmp ./scripts/check-model-regeneration.sh` — text metadata comparator passed; image model regeneration comparator passed.
- Deliberate text catalog fault gate: corrupted `models_generated.go`; comparator failed as expected; restored exact file; clean comparator passed.
- Deliberate image catalog fault gate: corrupted `images/models_generated.go`; comparator failed as expected; restored exact file; clean comparator passed.
- `make check` — passed after explicit UUID timestamp preservation was corrected for repeated deterministic test runs.
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...` — passed.
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- `go vet ./...` — passed.
- `make staticcheck` — passed (`staticcheck@v0.7.0`).
- `make check-logging` — passed.
- `make test-repro` — passed.
- `make sbom-check` — passed: 18 components, SHA-256 `ce1ca6899c2139d8df011926664b21ea8d3d8520f96033b11ea179be240066d3` at pre-commit revision `abd95ba55b58`; rerun after final commit remains required for final artifact evidence.
- `make vuln-check` — passed: `govulncheck: no reachable vulnerabilities for go1.26.6`.
- `make license-check` — passed; warnings limited to non-Go assembly dependency-inspection notices from `go-licenses`.
- Clean-checkout validation from `/workspace/tmp/go-ai-v0850-clean` with current patch applied passed: `go test ./...`, 142-row manifest validation, model/image regeneration comparators, and `git diff --check`.
- Hosted CI, final SHA, and rollback SHA are pending until after commit/push.
