# v1.7.0 Anthropic strict-tool schema

## Pinned original and receipt

Scope: the unchanged original `packages/ai/test/anthropic-strict-tool-schema.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f`, compared with the native Anthropic Messages production path from `/work/go-ai` main `393e8ed78fbcef88b22a58a7447a08d55dd5f402` in an isolated detached worktree.

The checked-out original matched its pinned Git blob, `587231f4cca3a0d38eea64d1919ed062d92ecd68`, byte for byte: SHA-256 `eeb7df219677ca888be09eccd95aed2ba7e1d79da99d2fe49ad1124506829afd`. Vitest 4.1.12 ran it unchanged on Node 26.13.0 with one worker and a 650 MiB old-space limit: **4 tests passed, 0 failed, 0 skipped**. The file's `baseUrl` is the loopback-only `127.0.0.1:9`, and its payload callback stops the request before transport; no provider credential or live service was used.

The unchanged native Anthropic package passed the focused strict-schema controls before any edits. The retained controls call the public `goai.Complete` adapter, assert the `OnPayload` view and then repeat each case through the actual Anthropic HTTP request path. They cover legacy strict omission and closed-object projection, strict nullable and required normalization, root `$defs` and `definitions`, draft-07 tuple preservation, locally unsupported minimum/maximum/nested-array/regex classes, and supported strict-string constraints. No production mismatch was proven, so no production file changed.

Verification uses the shared `/tmp/go-ai/v170-shared-verification.lock` and `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, `GOGC=60`, and `GOFLAGS=-p=1` controls:

- unchanged original: Vitest with `--maxWorkers=1 --no-file-parallelism`;
- focused native selection: `TestSourceV160AnthropicStrictToolSchemaWire`, `TestSourceV161AnthropicNonStrictTupleDialect`, and `TestAnthropicNonStrictTupleRootFilteringV161`, each run twice;
- `go vet ./inference/provider/anthropic`;
- Staticcheck v0.8.2 for the Anthropic package; and
- `git diff --check`.

This is a bounded local receipt, not full v1.7.0 acceptance. No live Anthropic credentialed case, full suite, or acceptance/profiling gate was run or claimed.
