# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.85.0`
- Upstream tag/SHA: `107d79f11072bbc8a3a757ed7fd69596bee7d68c`
- Previous accepted upstream baseline: `v0.84.4` / `b79e4cc834970cca69daebffab7df1da7d1e52c4`
- Previous accepted Go baseline before this audit: `abd95ba55b58b3986961b03fcc5c014d6d775c0c`
- Exact upstream checkout: `/workspace/tmp/pi-mono-audit`
- Official npm artifact: `/workspace/tmp/pi-ai-0850/pi-ai-0.85.0.tgz`
- Official npm artifact SHA-256: `46188bdacb555a07466a0111f3963f20932a16199e4d6cfb8d44a7fe5fc6e342`
- Detailed path matrix: `docs/v0850-release-ledger.md`
- Whole-corpus upstream test crosswalk: `docs/v0850-142-test-manifest.md`

## Scope evidence

- Canonical changed-path command: `git diff --name-status b79e4cc834970cca69daebffab7df1da7d1e52c4 107d79f11072bbc8a3a757ed7fd69596bee7d68c -- packages/ai`
- Changed paths: `51` canonical rows, committed at `docs/v0850/changed-paths.txt`, SHA-256 `db461a56838926cf60d4ae0196ed98fcc215616dacff013ad8c235bb8ad9b83f`.
- Changed tests: `29` rows, committed at `docs/v0850/changed-tests.txt`, SHA-256 `0b58c13688745fd74837bcefb868d2f5064649dcb4c57a5e134e08be0fd9d711`.
- Whole upstream test corpus: `142` rows, committed at `docs/v0850/test-corpus-142.txt`, SHA-256 `56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065`.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.85.0:

- Exact generated text catalog refresh to `1336` models across `39` providers.
- Exact image catalog regeneration/comparator retained through `scripts/check-model-regeneration.sh`.
- `OpenAICompletionsCompat.VLLMPriority` plus OpenAI chat `priority` serialization.
- `OpenAIResponsesCompat.SupportsMaxOutputTokens` plus `max_output_tokens` omission when explicitly unsupported.
- `Message.ProviderThinkingLevel` and `AnthropicMessagesCompat.SupportsMidConvoEffort`.
- Anthropic managed mid-conversation effort markers, active/default effort, result thinking-level persistence, managed beta headers, `block_binding.drop_block`, temperature omission, and beta override behavior.
- Strict `AssistantMessageFrame` encoder/reducer for compact stream frame serialization/reconstruction: duplicate start/terminal/order/kind/index errors, queued-delta prefix trimming, tool JSON checkpoint/resume, interleaving, authoritative end metadata/arguments, deep clone/purity, unknown-frame rejection, and pre-generation error omission.
- Optional timestamp input for `UUIDv7(timestampMs ...int64)`, preserving explicit supplied timestamps regardless of prior ordinary monotonic state.
- v0.85.0 `NO_PROXY`/`no_proxy` matching, including root/subdomain and bracketed IPv6 normalization, in the retry transport proxy path.
- Codex terminal SSE event without trailing blank line covered through the Go SSE parser and Codex regression test.
- Cloudflare AI binding auth sentinel and early fetch-adapter validation. JavaScript Workers `env.AI.fetch` is documented as adapted because Go has no native Workers FetchFunction runtime.
- `pre-generation-error.test.ts` is channel-adapted and executable: `tests/v0850_pre_generation_error_test.go` proves applicable Go providers emit pre-dispatch `ErrorEvent`s before any HTTP request instead of JavaScript construction-time throws.

N/A/adapted decisions and exact per-test evidence are in `docs/v0850-142-test-manifest.md` and `docs/v0850-release-ledger.md`.

## Validation evidence

Current local evidence captured during the v0.85.0 audit:

- `go test ./inference/provider/anthropic ./inference/provider/openai ./inference/provider/openairesponses ./inference/provider/openaicodex ./tests` — passed.
- `go test ./...` — passed.
- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0850/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-mono-audit/packages/ai/src/providers` — `upstream pairs: 1336`, `generated pairs: 1336`, exact match.
- `TMPDIR=/workspace/tmp GO_TMPDIR=/workspace/tmp ./scripts/check-model-regeneration.sh` — text metadata comparator passed; image model regeneration comparator passed.
- `python3 scripts/validate-v0850-inventory.py` and `--self-test` — committed 51/29/142 inventory counts/hashes validated; deliberate inventory corruption fails as expected.
- `python3 scripts/validate-v0850-catalog-delta.py` and `--self-test` — committed full-record text delta `+72/-26/79` and image delta `+0/-0/0` validated; baseline/current non-ID metadata corruption fails as expected.
- `python3 scripts/validate-test-manifest.py docs/v0850-142-test-manifest.md` — manifest validation passed against committed `docs/v0850/test-corpus-142.txt`.
- Candidate `fcd8270faee46a2eead7ef13e054f96704791ff4` and CI run `33890620670` are superseded/rejected. Candidate `c2d5d318c4e3052502c784255b856e1a7a12914b` and CI run `33893048444` are superseded by the frame persistence wire-format correction. Wire-corrective local gates passed: `make check`, shuffle, race, vet, staticcheck, logging, repro, SBOM, vuln, and license. Wire-corrective clean-checkout validation passed with `go test ./...`, committed inventory validation/self-test, catalog delta validation/self-test, 142-row manifest validation, model/image regeneration comparators, and `git diff --check`. SHA-linked SBOM/hosted CI will be rerun after the wire corrective commit.
- Deliberate text catalog fault gate: corrupted `models_generated.go`; `./scripts/check-model-regeneration.sh` failed as expected; restored exact file; clean comparator passed.
- Deliberate image catalog fault gate: corrupted `images/models_generated.go`; `./scripts/check-model-regeneration.sh` failed as expected; restored exact file; clean comparator passed.

Pending before completion:

- Corrective Rui-authored runtime commit/push, hosted CI/SBOM artifact evidence, final SHA, rollback SHA, and auditor notification. README normalization is explicitly queued until after auditor acceptance.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation/fix/adaptation/N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
