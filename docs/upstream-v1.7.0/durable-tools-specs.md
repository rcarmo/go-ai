# Durable tool specifications and file boundaries

This receipt covers only the seven unchanged pi-durable originals at `fb0fe823539f6d04850f6e34748c01f01f212e1f` requested for the isolated lane: `spec-usage.test.ts`, `tool-args-typebox.test.ts`, `tool-search.test.ts`, `tools-images.test.ts`, `tools-read-differential.test.ts`, `tools.test.ts`, and `types.test.ts`. The native lane starts at exact main `f6f191ec74dc7620ee274b890d9371b406400758`.

## Original verification

The seven worktree files were compared with the exact v1.7.0 Git blobs and remained unchanged. Their SHA-256 values are:

| Original | Git blob | SHA-256 |
|---|---|---|
| `spec-usage.test.ts` | `38e6f303f6d7d9b709ee89cca4a9638f6d0b2cd4` | `8abe0a64c299d700aabfd5c26547c3ef38cd636681cd5e95300b5ee6c1936bfb` |
| `tool-args-typebox.test.ts` | `7a606cf963434fe106e3dc4f253c7bfbe9c959e9` | `af661354a4a3f0062027688f75d789cf45e2e747bf17f70aeec8ba53b96fc848` |
| `tool-search.test.ts` | `80a00b6d83bf37c3f373cb3944ee1aabe4adaec6` | `228174069bea809e7a008c4d6f8bd245447f9f669909fd1bad79e41a1d8f42f4` |
| `tools-images.test.ts` | `38a371fa6396e2665e363d65b9ced0520f6b2886` | `ae800f074a9c959b8be69afb10950f6d6a1fc8f3d0aea254d040e186c21790aa` |
| `tools-read-differential.test.ts` | `b1e30fe4c23e9584820e49e42da7a9d92ed21d0c` | `84b1d1aa23065f106d0f72bb76bdf185ee07808fdaad7fb174905cd2d8afab58` |
| `tools.test.ts` | `6fb283e250e8b4d8682cdf44ecdd107567c46397` | `8d664a5856ac3d9a3c34b0c4e8f973ae4d0d320c63aeadf89629f4b977242eb1` |
| `types.test.ts` | `5cd293f5e467caf69fef00d59cc97f8dd92573a2` | `f49ef246a342a8f08112aa996413eac1ac54e26bf5bb655cbb052dd898295071` |

Vitest 4.1.12 ran from the unchanged package directory with one worker and `NODE_OPTIONS=--max-old-space-size=650`. The shared lock was `/tmp/go-ai/v170-shared-verification.lock`.

- `spec-usage`, `tool-args-typebox`, `tool-search`, `tools-read-differential`, `tools`, and `types`: **49 passed, 1 declared skip**.
- `tools-images`: **33/34 passed in bounded groups**. The selected non-worker/image/read groups and two worker-lifecycle groups were green. `gives the same result as the calling thread, and keeps the caller's bytes` is explicitly gated: its isolated run reached Node's 650 MiB ceiling while processing a 1100×1100 image plus inline Photon work. This is not a native or upstream failure claim.
- A whole-file image run also hit the same heap ceiling after reporting 32 passing cases; the bounded reruns above account for all 34 named cases without changing the original.

## Proven tool-search gaps and fix

Before production changes, `TestToolSearchReferencesOnlyReachableSchema` failed because `schemaSearchText` traversed every object below `$defs`, including the dormant `unused.description = "forecast"`. The baseline then loaded `ledger_inspect` for the unrelated `forecast` query. The fix follows local `$ref` targets through the root while excluding unreferenced definitions, and retains deterministic property traversal plus bounded local-reference handling.

A second baseline test failed because registration's `limit` was the only search limit: a per-call `limit: 1` loaded both matching tools. The input schema now admits an optional positive integer `limit`; a valid per-call request narrows the registration ceiling. Invalid limits still reject before loading tools.

Both regressions run through the public Harness, SQLite, provider requests, durable controls, message receipts and `Conversation.Agent`. They verify tool details, load order, unloaded deferred tools and canonical transaction persistence without changing M1c ownership, usage, redaction or record limits.

## Native comparison and retained boundaries

Existing native public-path controls cover TypeBox-shaped JSON schemas at use, detached/coerced argument values, UTF-16 replay, bounded reads, growing files, image MIME/dimensions/size, processor ownership, shell output windows, diagnostics and storage effects. TypeScript generic inference and JavaScript runtime identity in `spec-usage.test.ts`, `tool-args-typebox.test.ts`, and `types.test.ts` are compile-time/process-specific; locators mark them partial or unmatched rather than claiming equivalent Go static typing. Photon worker event-loop timing is recorded as an upstream observation, not a Go process-equivalence claim.

## Verification

All native runs used Go 1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, `GOGC=60`, `GOFLAGS=-p=1`, and the shared lock.

- The new search tests plus schema/search/argument controls passed twice, shuffled: `go test ./durable -run '^(TestToolSearch120|TestToolSearchReferencesOnlyReachableSchema|TestToolSearchHonorsCallLimit|TestSourceToolParameterSchemasAndCoercion|TestSourceToolArrayArgumentsThroughPublicExecution|TestSourceToolArrayArgumentsUTF16SafeReplay|TestSourceToolPrepareArgumentsErrorKeepsSourceMessage|TestStrictJSONAndBudgets)$' -count=2 -shuffle=42 -timeout=180s`.
- The complete native `durable/tools` package passed: `go test ./durable/tools -count=1 -shuffle=42 -p=1 -timeout=180s`.
- `go vet ./durable` passed.
- `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./durable` passed.
- The assertion validator and v1.7 crosswalk check passed for this ledger.
- `git diff --check` passed.

No broad suite, live credential call, profiling, hosted CI, release, push, or full acceptance was run.
