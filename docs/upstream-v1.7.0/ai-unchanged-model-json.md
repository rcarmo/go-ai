# v1.7.0 AI unchanged model and JSON compatibility

Pin: `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Native base: clean `5917e027185304af08f09718a88351f1cee05ff2` on `/work/go-ai` main. Isolated worktree: `/work/port-lanes/ai-v170-models-json` on `port/v170-models-json`.

## Originals

The pinned detached source checkout matched `HEAD` and these Git objects byte for byte:

| Upstream test | Git blob | SHA-256 |
| --- | --- | --- |
| `packages/ai/test/json-parse.test.ts` | `5783d956849a91c7971f794dc9884feddc126de3` | `ed5137f776ef10f3429d83769dc50eb5d7185ddfc5d260ad82d553d40c8d8581` |
| `packages/ai/test/lazy-module-load.test.ts` | `cb0875c9ee73e012206348a9b295b20cfc25a970` | `8b68a837881194e4a942ac3c72aa90366d0df98daebc925af6cbe11e46585ee9` |
| `packages/ai/test/model-catalog-types.test.ts` | `e5d871909924f5ccfe0a7058e94b9eb9f9b8cf18` | `c47f1953bc68f6af093ead5618100f2cb6e20feda121f891c3fbfd3c303ecdb5` |
| `packages/ai/test/model-cost-tiers.test.ts` | `9eade534ea5bd6b5a8c885f6287708ca728a28cc` | `12c515388e123bf69a9f6ff2fd69f15bfbaaf209aad13fcdf86cc44c1db0ed34` |
| `packages/ai/test/model-data-validation.test.ts` | `777a6ec08b32340e8a38c2ad4fd1063c57dc4400` | `9b9d6888bfb1156cd4e37b94cd9f5f9a0a73ba1925853dd5aa4a15a5244f5f41` |
| `packages/ai/test/model-types.test.ts` | `028a61a4d82cfc772806883bd005594bc8fbac61` | `cc934baaea086f058fdba2cc7cccaf57045092843cfec8fba6eea0b5fa5aa3ee` |
| `packages/ai/test/models-entry.test.ts` | `3936346a3191c8bee08652c402e941d2169d886c` | `1b2e9830d59db8cc95d4a6cb93529502b23e848007351917153b36b0090551e2` |
| `packages/ai/test/max-thinking.test.ts` | `c77f14ce83e9efd058a3921856f033da18b6a7f2` | `c985d6f23d461678312a2c487668573c718add78b7c5b93aaf598d098fdcb926` |
| `packages/ai/test/message-types.test.ts` | `a3265d653077bdc14420985d021718abd3bc1bb5` | `b288469cca9b6a4b975aaf3c68dd71334d913f8d935222d4727df056b534652c` |
| `packages/ai/test/lax-message-content.test.ts` | `24a7df3cb3bb461e6bb67b75e52c8274c11718f9` | `2a8ee2048658a1a0a2a8928cc9570c7db41ed295ca20b1d23098b70d4d39e196` |

Vitest 4.1.12 ran all ten unchanged files with Node 26.13.0, one worker and a 650 MiB old-space limit under the shared `/work/port-lanes/.v170-focused-verify.lock`: **10 files and 73 tests passed**. No credential or live request was used.

## Native comparison

The version-neutral native controls are in `tests/ai_unchanged_model_json_compat_test.go`; existing production-path Codex, catalog, model-store and lax-content controls supplement them.

The unchanged implementation already passed streaming invalid-escape previews, valid escape decoding, cut-off Unicode omission, repaired completed tool arguments, generated grouped catalog routes, typed identity equality, thinking-level clamps and initial max requests, lax nil content and JSON detail round trips.

Two failures were reproduced before production edits:

- completed and streaming tool-argument failures omitted the upstream diagnostic separator `\n\nReceived arguments:\n`, and non-object JSON used different object-error casing;
- restored or fetched chat catalogs retained rows with unknown `type` values instead of dropping them.

Commit `1780a72c` fixes those proven gaps. JSON parser failures now preserve the raw diagnostic envelope; the common provider/catalog filter admits only legacy untyped or explicit chat rows. No change was made to speculative loader, pricing, generator or type-system behavior.

JavaScript's ESM import interception is a language/runtime contract with no Go module-load analogue. The native control verifies the public lightweight model runtime can hold and complete basic model operations without a catalog overlay; `Go` static linkage does not expose Node SDK import observation. TypeScript's compile-time generic `JsonValue` exclusions are partly represented at runtime by JSON serialization and transcript round trips, not by a Go compile-time structural-JSON type system. OpenRouter/Vercel source-rate conversion operates in upstream TypeScript; the native generator consumes the pinned hydrated final catalog, and full-record regeneration plus representative tier calculation establish its published Go result.

## Verification

Under the shared lock, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2`, `GOGC=60` and `GOFLAGS=-p=1`:

- focused model/json, catalog, generator, Codex and parser selection passed twice;
- focused `go vet` passed;
- Staticcheck `v0.8.2` passed for `./...`;
- `make check-model-regeneration` reproduced and matched all 1,701 chat, 74 image and 83 classifier records;
- `git diff --check` passed.

This is a bounded offline receipt, not full v1.7.0 acceptance, acceptance profiling, hosted validation, live-credential replay or release authorization.
