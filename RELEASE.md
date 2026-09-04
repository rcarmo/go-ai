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
- Changed paths: `51` rows, stored in `/workspace/tmp/pi-ai-0850/changed-paths.txt`, SHA-256 `db461a56838926cf60d4ae0196ed98fcc215616dacff013ad8c235bb8ad9b83f`.
- Changed tests: `29` rows, stored in `/workspace/tmp/pi-ai-0850/changed-tests.txt`, SHA-256 `0b58c13688745fd74837bcefb868d2f5064649dcb4c57a5e134e08be0fd9d711`.
- Whole upstream test corpus: `142` rows, stored in `/workspace/tmp/pi-ai-0850/test-corpus-142.txt`, SHA-256 `56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065`.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.85.0:

- Exact generated text catalog refresh to `1336` models across `39` providers.
- Exact image catalog regeneration/comparator retained through `scripts/check-model-regeneration.sh`.
- `OpenAICompletionsCompat.VLLMPriority` plus OpenAI chat `priority` serialization.
- `OpenAIResponsesCompat.SupportsMaxOutputTokens` plus `max_output_tokens` omission when explicitly unsupported.
- `Message.ProviderThinkingLevel` and `AnthropicMessagesCompat.SupportsMidConvoEffort`.
- Anthropic managed mid-conversation effort markers, active/default effort, result thinking-level persistence, managed beta headers, `block_binding.drop_block`, temperature omission, and beta override behavior.
- `AssistantMessageFrame` encoder/reducer for compact stream frame serialization/reconstruction.
- Optional timestamp input for `UUIDv7(timestampMs ...int64)`.
- v0.85.0 `NO_PROXY`/`no_proxy` matching, including root/subdomain and bracketed IPv6 normalization, in the retry transport proxy path.
- Codex terminal SSE event without trailing blank line covered through the Go SSE parser and Codex regression test.
- Cloudflare AI binding auth sentinel and early fetch-adapter validation. JavaScript Workers `env.AI.fetch` is documented as adapted because Go has no native Workers FetchFunction runtime.
- `pre-generation-error.test.ts` is documented as channel-adapted: Go provider APIs emit pre-dispatch `ErrorEvent`s instead of JavaScript construction-time throws.

N/A/adapted decisions and exact per-test evidence are in `docs/v0850-142-test-manifest.md` and `docs/v0850-release-ledger.md`.

## Validation evidence

Current local evidence captured during the v0.85.0 audit:

- `go test ./inference/provider/anthropic ./inference/provider/openai ./inference/provider/openairesponses ./inference/provider/openaicodex ./tests` — passed.
- `go test ./...` — passed.
- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0850/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-mono-audit/packages/ai/src/providers` — `upstream pairs: 1336`, `generated pairs: 1336`, exact match.
- `TMPDIR=/workspace/tmp GO_TMPDIR=/workspace/tmp ./scripts/check-model-regeneration.sh` — text metadata comparator passed; image model regeneration comparator passed.
- `python3 scripts/validate-test-manifest.py docs/v0850-142-test-manifest.md /workspace/tmp/pi-ai-0850/test-corpus-142.txt` — manifest validation passed.
- `make check` — passed after fixing explicit UUID timestamp preservation under `-count=3`.
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
- Deliberate text catalog fault gate: corrupted `models_generated.go`; `./scripts/check-model-regeneration.sh` failed as expected; restored exact file; clean comparator passed.
- Deliberate image catalog fault gate: corrupted `images/models_generated.go`; `./scripts/check-model-regeneration.sh` failed as expected; restored exact file; clean comparator passed.

Pending before completion:

- Final Rui-authored commit/push, hosted CI/SBOM artifact evidence, final SHA, rollback SHA, and auditor notification.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation/fix/adaptation/N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
