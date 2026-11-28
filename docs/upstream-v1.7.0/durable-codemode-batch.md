# Durable Codemode originals on pi v1.7.0

This bounded packet audits the five previously unrepresented unchanged Durable Codemode tests at official pi `v1.7.0`, `fb0fe823539f6d04850f6e34748c01f01f212e1f`, from clean native main `cbca8ba81df8a13ee1c88521a078203540a637b1`.

## Exact originals

The staged checkout stayed byte-for-byte at the pinned release. Both Git blob IDs and SHA-256 values were verified:

| Path | Git blob | SHA-256 |
| --- | --- | --- |
| `packages/durable/test/codemode-checkpoints.test.ts` | `098feb12d3eb96d0cc4d9a3f82666c78068779c2` | `9409c19a882393d3bf656648691b74157de8bc179dd21137d58fa3a9e0415445` |
| `packages/durable/test/codemode-describe.test.ts` | `e172fcffe22dc38755d97dbbd86298dc69487e96` | `9b16f48a954ba3b99ef7ea0867eacccde1fbf9ac05235f8f4ecae1baf88ca9d9` |
| `packages/durable/test/codemode-imports.test.ts` | `df74da0e24571a90723c41dae95bca8003828732` | `2abdc6758279c53d873a8e764d3b06e0bf73ac37e07dfb930832a088df2d7426` |
| `packages/durable/test/codemode-models.test.ts` | `b6d1aa5df84dcc97ac704fa1ef891328d03fca28` | `773033a5f53a098436affe8cc9af98b589f8ac221951a73f9dfb86a9ff997b1a` |
| `packages/durable/test/codemode.test.ts` | `feda583a45ba7d9c97a5d3e375863e87cdab8606` | `55453420e8773f75caa8f2167c1965e50553e8934c399d352e53555fe1e9662d` |

The original Vitest run used Node `v26.13.0`, Vitest `4.1.12`, one worker, the normal `/tmp/go-ai/v170-shared-verification.lock`, and a 650 MiB heap cap. The exact five-file invocation completed 58 cases; only the restart-timeout case exceeded Vitest's five-second process-wide limit. That case passed independently in 857 ms, and the complete `codemode.test.ts` then passed all 42 cases in 19.59 s. Together, the exact originals passed all **59/59** cases with no test or source edits and no skips. The initial timeout is retained here as a disclosed contention artifact, not treated as an implementation failure.

```sh
NODE_OPTIONS=--max-old-space-size=650 GOMEMLIMIT=650MiB \
  node --conditions=source ../../node_modules/vitest/vitest.mjs --run \
  test/codemode-checkpoints.test.ts test/codemode-describe.test.ts \
  test/codemode-imports.test.ts test/codemode-models.test.ts \
  test/codemode.test.ts --maxWorkers=1 --reporter=verbose
```

## Native public and SQLite comparison

No production gap was demonstrated. Existing native tests exercised the public Codemode API and SQLite durability for checkpoint history/forks, description and declaration grouping, deferred discovery, catalog redaction and reuse, classifier failures and envelopes, model/image concurrency, generated-image notices, nested tool results, journal settlement/replay, timeout fencing, output projection/spill limits, namespace prompts and result errors.

The native selector included 26 top-level tests and passed twice with shuffle seed 42 and no skips in 43.760 s. Representative groups are `TestCodemode140StoreCheckpointThresholdAndHistory`, `TestCodemode120PinnedDescribeGroupingAndBudget`, `TestCodemode120QuickJSModelCatalogScopedSanitizedAndReuse`, `TestCodemode120QuickJSClassifierGlobalOptInAndReplay`, `TestCodemode120QuickJSImageGlobalScopedAndSettledReuse`, `TestCodemode120QuickJSJournalAndSettledReuse`, `TestCodemode120ToolErrorOutcomeAndSettledReplay`, `TestCodemode120RuntimePersistsQuickJSDrains`, and `TestCodemode120SixMiBReturnSpillsAndReplays`.

All native work used SQLite through the public Harness paths with `CGO_ENABLED=0`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`, `GOGC=60`, `GOMEMLIMIT=650MiB`, Go 1.27.3 and the same shared lock. `go vet ./durable` and pinned Staticcheck `v0.8.2 ./durable` passed.

```sh
go test ./durable -run '<26 scoped public Codemode groups>' \
  -count=2 -shuffle=42 -timeout=180s -v
go vet ./durable
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./durable
git diff --check
```

## Explicit limits

- `codemode-imports.test.ts` proves a JavaScript module-mocking property: importing the extension must not evaluate `quickjs-wasi`. The native port has no TypeScript module loader, so this is an exact-process gap rather than evidence against native Codemode construction.
- Node `worker_threads`, TypeScript `MemoryStorage`, JavaScript object shapes and wall-time scheduling are not claimed as native process parity. Assertions are adapted only where the same public observable behavior is checked through Go records, SQLite and the embedded/public Codemode runtime.
- Published Node worker subprocess tests are not part of this receipt; skipped opt-in Node-worker adapters are not counted as native passes. The exact 1.7.0 worker/dist was used by the unchanged original checkout to execute `CodemodeSandbox`; the native comparison remains public Go Codemode and SQLite only.
- No production fix, broad suite, live model request, profiling, publication, or full acceptance is claimed.
