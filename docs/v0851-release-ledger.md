# v0.85.1 release ledger

Audit target: official upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.85.1`, SHA `d981de1229ef899957bbe968bc8dcda02a21f477`, npm published `2026-09-05T12:05:47.996Z` with matching `gitHead`.

Previous accepted upstream baseline: `v0.85.0`, SHA `107d79f11072bbc8a3a757ed7fd69596bee7d68c`. Previous accepted Go runtime baseline: `90d17907b2ce26ffe5f46cd061edc8209e357bed`; current repository baseline before this audit: `dfdb68e4e734d730f89e34a4b77f098d68cd8b74`.

## Exact artifacts and hashes

- Official npm tarball: `/workspace/tmp/pi-ai-0851/earendil-works-pi-ai-0.85.1.tgz`.
- npm SHA-256: `af7d11986179445ce6fe88b37d57de22f823c0ffd3a65cae31c555b7f5e99253`.
- npm raw SHA-512: `f958152090e40ced9e7d824a104aaf3d31f8ce69c8697740a6919b3bebca140f6acb93dd8458807a7b8502453ea220927ce0b874c1c3cab8dd41e2f86680b909`.
- Changed paths: `docs/v0851/changed-paths.txt`, 9 rows, SHA-256 `ee26f669d92dc77b265731165a2ff69ccb67defba92517cbbd5f97a186e187d2`.
- Changed tests: `docs/v0851/changed-tests.txt`, 3 rows, SHA-256 `f7e274bf229c90fc22ba22384c5b89f71a5c6801f77067d099525a9cdc537610`.
- Whole test corpus: `docs/v0851/test-corpus-142.txt`, 142 rows, SHA-256 `56f8742065a4ad01d73e5aee53035324f2e7333a735222ab15db870819e29065`.
- Source delta: 9 files, `+128/-23`.

## Changed-path matrix (9 canonical rows)

| # | Upstream path | Go disposition |
| ---: | --- | --- |
| 1 | `packages/ai/CHANGELOG.md` | Release metadata only; documented in `RELEASE.md`/this ledger. |
| 2 | `packages/ai/package.json` | Package version/npm provenance only; documented in `RELEASE.md`/this ledger. |
| 3 | `packages/ai/scripts/generate-models.ts` | Ported via `scripts/generate-models.go` cost-tier emission and v0.85.1 generated catalog inputs; exact generation comparator and negative self-test cover drift. |
| 4 | `packages/ai/src/api/openai-responses.ts` | Ported in `inference/provider/openairesponses/responses.go`: explicit prompt-cache models emit `prompt_cache_options` for none/long retention and avoid legacy `prompt_cache_retention`; legacy models retain `24h` behavior. |
| 5 | `packages/ai/src/image-models.generated.ts` | Regenerated into `images/models_generated.go`; v0.85.1 adds MAI Image 2.6 and MAI Image 2.6 Flash, full-record image delta `+2/-0/0`. |
| 6 | `packages/ai/src/types.ts` | Existing Go `OpenAIResponsesCompat.SupportsExplicitPromptCacheMode` is now exercised by v0.85.1 production tests and generated GPT-6 Astra metadata. |
| 7 | `packages/ai/test/cache-retention.test.ts` | Mapped to `inference/provider/openairesponses/v0851_cache_retention_test.go` production request serialization tests. |
| 8 | `packages/ai/test/max-thinking.test.ts` | Mapped to `tests/models_v0851_catalog_test.go` and `tests/supports_xhigh_upstream_test.go` GPT-6 Astra thinking-map coverage. |
| 9 | `packages/ai/test/supports-xhigh.test.ts` | Mapped to `tests/supports_xhigh_upstream_test.go` GPT-6 Astra supported-level assertions. |

## Catalog deltas

- Text/chat: `1336 -> 1354` records across 39 providers / 9 APIs; full-record delta `+20/-2/18 changed`.
- Images: `50 -> 52` records; full-record delta `+2/-0/0`, additions `microsoft/mai-image-2.6` and `microsoft/mai-image-2.6-flash`.
- GPT-6 Astra generated records cover OpenAI, Azure OpenAI, OpenAI Codex, GitHub Copilot, OpenCode, OpenRouter, and Vercel AI Gateway wrappers with upstream costs/context/compat. Direct OpenAI/Codex records include the long-context pricing tier above 272000 input tokens.

## Local validation evidence

- Focused `go test ./inference/provider/openairesponses -run TestV0851Responses -count=1` — passed.
- Focused `go test ./tests -run 'Test(RegisterBuiltinModels|V0850CatalogCounts|V0851|SupportsXHighIncludesGPT6)' -count=1` — passed.
- `python3 scripts/validate-v0851-inventory.py` and `--self-test` — passed.
- `python3 scripts/validate-v0851-catalog-delta.py` and `--self-test` — passed, including text/image non-ID metadata corruption failure.
- `python3 scripts/test-check-model-regeneration.py` — passed, proving generated text and image comparators fail on non-ID metadata drift.
- `TMPDIR=/workspace/tmp GO_TMPDIR=/workspace/tmp ./scripts/check-model-regeneration.sh` — passed from exact v0.85.1 tag/npm artifacts.
- Full local gates passed: `make check`, shuffle, race, vet, staticcheck, logging, repro, SBOM, vuln, and license. Clean-checkout validation passed with `go test ./...`, v0.85.1 inventory/catalog/manifest validators and self-tests, model-regeneration negative self-test, exact regeneration comparators, and `git diff --check`. Final runtime SHA, hosted CI, and SHA-linked SBOM are pending. README/SBOM release publication remains blocked until runtime acceptance.
