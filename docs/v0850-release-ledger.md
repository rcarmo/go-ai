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

## Changed-path matrix (51 canonical rows)

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
| 20 | M | `src/utils/node-http-proxy.ts` | Ported v0.85.0 proxy responsibility in Go `retry.go`: custom environment proxy function honors `NO_PROXY`/`no_proxy` root-domain, subdomain, and bracketed IPv6 bypasses. Covered by `tests/v0850_uuid_proxy_test.go` and `tests/retry_proxy_test.go`. |
| 21 | M | `src/utils/retry.ts` | Reviewed retry/fetch helper delta. Go retry transport already owns retry dispatch; v0.85.0 proxy-specific behavior is mapped to upstream `src/utils/node-http-proxy.ts` and covered in `retry.go`/proxy tests. |
| 22 | M | `src/utils/uuid.ts` | Ported optional timestamp support to `UUIDv7(timestampMs ...int64)` while preserving explicit supplied timestamps independently of ordinary monotonic clock state. Covered by `tests/v0850_uuid_proxy_test.go` and `make check -count=3`. |
| 23 | M | `test/anthropic-auth-token.test.ts` | Existing Anthropic auth-token/header tests plus `inference/provider/anthropic/v0850_beta_override_test.go` cover model/request `Anthropic-Beta` override replacing generated betas. |
| 24 | M | `test/anthropic-cache-write-1h-cost.test.ts` | `inference/provider/anthropic/anthropic_cache_write_1h_cost_test.go` covers 1h cache-write usage/cost accounting and fallback zero/default handling. |
| 25 | A | `test/anthropic-mid-conversation-effort.test.ts` | `inference/provider/anthropic/v0850_mid_conversation_effort_test.go` covers historical/current effort markers, `ProviderThinkingLevel`, `block_binding.drop_block`, beta headers, temperature omission, and direct/OpenRouter catalog gates. |
| 26 | M | `test/anthropic-sse-parsing.test.ts` | Existing Anthropic SSE parser tests plus `v0850_mid_conversation_effort_test.go` cover signed thinking/reasoning deltas, terminal pending errors, raw stops, and managed thinking-level persistence. |
| 27 | A | `test/anthropic-thinking-binding-e2e.test.ts` | N/A live Anthropic credential conformance; deterministic Go request/stream coverage for marker/binding replay is in `v0850_mid_conversation_effort_test.go`. No hidden skip counted. |
| 28 | A | `test/assistant-message-frame.test.ts` | `assistant_message_frame.go` and `tests/assistant_message_frame_v0850_test.go` cover strict start/terminal/order/kind/index grammar, queued delta prefix trimming, checkpoint/resume, interleaving, authoritative end metadata/args, deep clone/purity, and pre-generation error omission. |
| 29 | M | `test/baseten-models.test.ts` | `tests/models_v0850_catalog_test.go` verifies Baseten Kimi K2.7 Code text+image metadata from exact generated catalog. |
| 30 | A | `test/cloudflare-ai-binding.test.ts` | `cloudflare_ai_binding.go` and `tests/cloudflare_ai_binding_v0850_test.go` expose the binding auth sentinel and early fetch-adapter validation; JavaScript Workers `env.AI.fetch` transport remains Go-adapted/N/A. |
| 31 | D | `test/cloudflare-gateway-binding.test.ts` | Deleted upstream; superseded by `cloudflare-ai-binding.test.ts`. Go replacement/adaptation covered by `cloudflare_ai_binding.go`, `tests/cloudflare_ai_binding_v0850_test.go`, and existing Cloudflare HTTPS dispatch/header tests. |
| 32 | M | `test/constrained-sampling.test.ts` | Existing strict/constrained sampling Go tests remain green across OpenAI/Responses/Anthropic/Mistral schema conversion paths; exact catalog delta comparator guards metadata drift. |
| 33 | M | `test/generate-models-strict.test.ts` | N/A private TS generator helper; Go uses committed full-record `docs/v0850/*jsonl` baseline/current data, `scripts/validate-v0850-catalog-delta.py`, generator checks, and negative mutation self-tests. |
| 34 | M | `test/github-copilot-anthropic.test.ts` | Existing GitHub Copilot Anthropic tests cover Claude Code headers/tool-name normalization and beta suppression; managed-effort betas are separately covered for Anthropic models. |
| 35 | M | `test/github-copilot-oauth.test.ts` | Existing Go OAuth policy/runtime tests cover token/header/catalog behavior; JS credential-store UI remains adapted/N/A. |
| 36 | M | `test/node-http-proxy.test.ts` | `retry.go` custom proxy function plus `tests/v0850_uuid_proxy_test.go` cover root/subdomain and bracketed IPv6 `NO_PROXY`/`no_proxy` bypass behavior. Source responsibility maps to upstream `src/utils/node-http-proxy.ts`. |
| 37 | M | `test/openai-codex-stream.test.ts` | `inference/provider/openaicodex/v0850_terminal_sse_test.go` proves terminal SSE frames without trailing blank lines; existing Codex SSE/WS tests cover transport/header behavior. |
| 38 | M | `test/openai-completions-cache-control-format.test.ts` | Existing OpenAI completions cache-control format tests remain green against regenerated v0.85.0 compat metadata. |
| 39 | M | `test/openai-completions-thinking-as-text.test.ts` | Existing OpenAI completions thinking-as-text tests remain green against regenerated v0.85.0 compat metadata. |
| 40 | M | `test/openai-completions-tool-choice.test.ts` | Existing OpenAI completions tool-choice request-shape tests remain green; no new Go production change required. |
| 41 | M | `test/openai-completions-tool-result-images.test.ts` | Existing OpenAI completions tool-result image routing/downgrade tests remain green; no new Go production change required. |
| 42 | A | `test/openai-completions-vllm-priority.test.ts` | `compat.go`, `openai.go`, generated catalog compat, and `inference/provider/openai/v0850_vllm_priority_test.go` cover `vllmPriority` -> OpenAI `priority` serialization/omission. |
| 43 | M | `test/openai-responses-compat.test.ts` | `inference/provider/openairesponses/v0850_max_output_tokens_test.go` covers `supportsMaxOutputTokens`; `v0850_stale_error_cleanup_test.go` covers terminal stale-error cleanup. |
| 44 | M | `test/openai-responses-namespace.test.ts` | Existing `inference/provider/openairesponses/v0842_namespace_additional_tools_test.go` covers namespace/additional-tools replay; regenerated v0.85.0 metadata retained. |
| 45 | M | `test/openrouter-cache-control-models.test.ts` | Exact v0.85.0 catalog regeneration and full-record delta comparator cover OpenRouter cache-control model metadata drift. |
| 46 | M | `test/pi-messages.test.ts` | `inference/provider/pimessages/pimessages_test.go` covers terminal `providerThinkingLevel` propagation into assistant messages. |
| 47 | A | `test/pre-generation-error.test.ts` | `tests/v0850_pre_generation_error_test.go` proves applicable Go providers emit pre-dispatch `ErrorEvent`s before any HTTP request when auth is missing; Go channel contract is the language-adapted equivalent of TS synchronous throws. |
| 48 | M | `test/qwen-token-plan-models.test.ts` | `tests/qwen_token_plan_upstream_test.go`, `tests/models_v0850_catalog_test.go`, and the exact catalog delta comparator cover updated Qwen Token Plan metadata/allowlists. |
| 49 | M | `test/tool-call-id-normalization.test.ts` | Existing Anthropic/OpenAI/Codex tool-call ID normalization tests remain green; strict frame tests also verify authoritative tool-call IDs/arguments. |
| 50 | M | `test/uuid.test.ts` | `utils_text_uuid.go` and `tests/v0850_uuid_proxy_test.go` cover optional timestamp UUIDv7 generation, explicit timestamp preservation, and monotonic ordering. |
| 51 | M | `test/xai-responses.test.ts` | Existing xAI Responses tests plus regenerated catalog verify Grok request routing/shape and removal of stale Grok 4 model entries. |

## Go implementation/fix/adaptation decisions

- Generated exact v0.85.0 text catalog: 1336 models across 39 providers, including Cloudflare AI Gateway `workers-ai/` route metadata, Claude Fable 5.1 managed effort compat, Baseten/Fireworks/Qwen/OpenRouter/xAI/ZAI changes, and stale Grok 4 removals.
- Generated image catalog check retained exact upstream parity through `scripts/check-model-regeneration.sh`.
- Added `OpenAICompletionsCompat.VLLMPriority` and request serialization as OpenAI chat `priority`.
- Added `OpenAIResponsesCompat.SupportsMaxOutputTokens` and omitted `max_output_tokens` only when compat explicitly disables it.
- Added public `Message.ProviderThinkingLevel` and Anthropic `SupportsMidConvoEffort` compat; Anthropic request builder now inserts historical/current effort markers, sends managed-effort beta headers, enables `block_binding.drop_block`, defaults top-level output effort to `high`, omits temperature, and records the active provider thinking level on results.
- Added Anthropic model-level/request-level `Anthropic-Beta` override behavior so configured beta headers replace generated defaults.
- Added strict `AssistantMessageFrame` encoder/reducer for compact stream frame serialization/reconstruction, including duplicate start/terminal/order/kind/index errors, queued-delta prefix trimming, tool JSON checkpoint/resume, interleaving, authoritative end metadata/arguments, deep clone/purity, unknown-frame rejection, and pre-generation error omission.
- Added optional timestamp support to `UUIDv7` while preserving existing no-arg behavior and preserving supplied timestamps regardless of prior ordinary monotonic state.
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
- Corrective `make check` — passed, including new committed inventory and full-record catalog-delta validators/self-tests.
- Corrective `TMPDIR=/workspace/tmp go test -shuffle=on ./...` — passed.
- Corrective `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- Corrective `go vet ./...` — passed.
- Corrective `make staticcheck` — passed (`staticcheck@v0.7.0`).
- Corrective `make check-logging` — passed.
- Corrective `make test-repro` — passed.
- Corrective `make sbom-check` — passed at superseded pre-corrective HEAD; rerun after corrective commit is required for SHA-linked SBOM.
- Corrective `make vuln-check` — passed: `govulncheck: no reachable vulnerabilities for go1.26.6`.
- Corrective `make license-check` — passed; warnings limited to non-Go assembly dependency-inspection notices from `go-licenses`.
- Corrective clean-checkout validation from `/workspace/tmp/go-ai-v0850-corrective-clean` with patch applied passed: `go test ./...`, committed inventory validation/self-test, catalog delta validation/self-test, 142-row manifest validation, model/image regeneration comparators, and `git diff --check`.
- Runtime candidate `fcd8270faee46a2eead7ef13e054f96704791ff4` and CI run `33890620670` are superseded/rejected. Replacement corrective commit, hosted CI, final SHA, and rollback SHA are pending.
