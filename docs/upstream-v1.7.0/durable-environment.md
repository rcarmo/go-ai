# Durable environment public contracts

This lane covers the six unchanged Durable environment originals at pi v1.7.0 pin `fb0fe823539f6d04850f6e34748c01f01f212e1f`. It starts at native `cbca8ba81df8a13ee1c88521a078203540a637b1` and owns only line scanning, Node-environment conformance, list races, inherited-writer spill, filesystem/shell behavior, and head truncation in the following source files.

## Pinned originals

All six files were byte-verified against the pinned Git blobs and the working checkout remained clean. Their SHA-256 values were:

| File | SHA-256 | Result |
| --- | --- | --- |
| `env-line-scan.test.ts` | `177b8fd52d5392bb4de4a81fe54e49c583a34d3e25a4b7d57b2a31a063fdb5e8` | 4 passed |
| `env-node-conformance.test.ts` | `b052a243a51aa777a4c835f15721ce08c2c875c976ca1c1d29a49e3641afbb9b` | 51 passed |
| `env-node-list-race.test.ts` | `df8c9c44f217d38f9cc56e8c8484ed3018d02959b937fa24ec844f576385fd35` | 1 passed |
| `env-node-spill.test.ts` | `8d28d92e9ee8bf7200cfafff871e50d32909305d855986f8e83c370c16918be0` | 1 passed |
| `env-node.test.ts` | `27d23c2473c758952a9023e8462c59287952a9f1f03dea06e73cbf6881927cd0` | 52 passed, 1 skipped |
| `env-truncate.test.ts` | `e0b88d0353921059e25b19d70d5596811fab35452c49e611728b7dc75ffcbc2c` | 8 passed |

Node `v26.13.0` and Vitest `4.1.12` ran all six originals under `NODE_OPTIONS=--max-old-space-size=650` and the shared `/tmp/go-ai/v170-focused-verify.lock`: **117 passed, 0 failed, 1 skipped**. The skip was the upstream Windows-only detached-process case. No original source changed.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock /tmp/go-ai/v170-focused-verify.lock \
  unshare -Urn node ../../node_modules/vitest/vitest.mjs run \
  test/env-line-scan.test.ts test/env-node-conformance.test.ts \
  test/env-node-list-race.test.ts test/env-node-spill.test.ts \
  test/env-node.test.ts test/env-truncate.test.ts
```

## Native mapping

The unchanged implementation passed the new behavior-named public-interface controls without a production edit:

- `TestEnvLineScanWholeFileAgreement` exercises 5,000 randomized byte streams through `durable.BinaryReader.ScanLines`, including invalid UTF-8, BOM, empty lines, out-of-range selections, offsets and decoded-byte counts.
- `TestEnvPublicFileContracts` uses the public `durable.FileSystem` interface for byte-exact truncate/extend, flush, portable errors and pre-aborted writes.
- `TestEnvShellSpillWaitsForInheritedWriter` uses the public `durable.Shell` interface to retain late inherited output and the complete caller-owned spill.
- `TestTruncateHeadContract` checks UTF-8 totals, complete-line byte limits and first-line overflow through `tools.TruncateHead`.

Existing locators cover the remaining representable pieces: per-conversation environment routing is exercised with public durable APIs and reopened SQLite storage; list removal races, binary-reader identity, directory paging, polling watch budgets, raw spill/failure ownership, and interleaved stream decoding use local production adapters.

The focused environment selector passed **twice** with shuffle 42 under Go `1.27.3`, `CGO_ENABLED=0`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB`, one build package and the shared lock. Focused `go vet ./durable/tools` and pinned Staticcheck `v0.8.2 ./durable/tools` passed. Staticcheck used the existing local module proxy cache; no production source changed.

## Limits

No backend or host-process identity claim is made. SQLite is the native storage path; Memory, Journal, Node, Chord, IndexedDB and alternate backends are not substituted. Node monkey-patching of `fs`, `FileHandle` sync spies, Buffer-less child-runtime execution, WSL/Git Bash selection, taskkill behavior, JavaScript Result/object identity, and Node native-watcher identity remain upstream execution observations rather than Go identity claims. The Go watcher proves equivalent public polling behavior only.

The typed Go interfaces cannot express JavaScript `NaN`, fractional file sizes, or object-reference identity. Validation of negative/invalid representable bounds remains covered. The delayed Node write-stream backpressure mock is likewise not reproduced as a host-facility identity test; the Go public shell/spill test proves complete settlement and spill bytes instead.

The assertion locator ledger is `docs/upstream-v1.7.0/assertions/durable-environment.jsonl`. This is a bounded unchanged-source comparison, not full acceptance or acceptance profiling.
