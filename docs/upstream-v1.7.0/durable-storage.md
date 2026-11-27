# Durable storage groups and head markers

All 102 tests in the two changed Durable storage suites passed unchanged at pi v1.7.0, `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The native comparison on `4bbf033e360745c3f41bceaaec84f381d6c880db` needed no production changes. The test-only commit `13343ac8f61180b99bbd9308d9338b61d4db917d` adds a public SQLite witness for group visibility and file reopen.

## Pinned upstream execution

| File under `packages/durable/test/` | Source SHA-256 | Passed |
| --- | --- | ---: |
| `store-storage-commit.test.ts` | `c818f3a9bc961041181a405d32a7f67a358e84453f32f4deead0600c3c7f1bfb` | 22 |
| `store-storage.test.ts` | `20c1e0976dad4630aaf712be08d3e8b586f6c49863f3fa299a097b824ae40006` | 80 |

Both files match the pinned Git objects. The diff from v1.6.1 removes `ChordStore` casts and an unused type import; Bun emits identical JavaScript for each file at both pins. This was an erasure check, not a TypeScript type check.

The private stage used a Git archive at the pin and `package-lock.json` SHA-256 `2a2523fdeacf04ccc5f707132d62ba6a091df196d0b56ff07da1d527f1ff7502`. `npm ci --ignore-scripts --no-audit --no-fund --include-workspace-root=false` selected the `ai`, `chord`, `codemode`, `durable`, `schema` and `telemetry` workspaces. The installed lock agreed with the pin for 220 npm placements and all six workspace links. The four package archives matched the SHA-256 and SHA-512 entries in [inputs.json](inputs.json). The first source-only load could not resolve the public `pi-ai` `dist` exports. Extracting only `package/dist` from its verified official archive into the private `packages/ai` checkout resolved the import; the source tests and config stayed unchanged.

Node v26.13.0 and Vitest 4.1.12 ran with a private empty home in an isolated network namespace. The run had no skips. Its JSON report is `originals-with-ai-dist.json` in the private stage, SHA-256 `7be409a9c4f30039d43c12ee3035aa1f0223b689c64da962117ee0cb4ecce822`. To repeat the run from the verified stage:

```sh
cd "$PI_DURABLE_TEST_STAGE/source/packages/durable"
unshare -Un env -i PATH=/usr/local/bin:/usr/bin:/bin \
  HOME="$PI_DURABLE_TEST_STAGE/empty-home" TMPDIR="$TMPDIR" \
  NODE_OPTIONS=--max-old-space-size=768 node ../../node_modules/vitest/vitest.mjs run \
  test/store-storage.test.ts test/store-storage-commit.test.ts --maxWorkers=1 --reporter=dot
```

## Native results

Eight unchanged Go baseline groups passed twice after the original suites. The new SQLite test passed four repeats. It holds a real SQL insert inside a group transaction and checks that an independent read-only connection can still read the empty prefix and that no receipt has been delivered. A same-store read waits and sees all three entries after the commit. The group uses one `BEGIN IMMEDIATE` and one `COMMIT`; three ordered receipts refer to the exact file-backed entries. The latest lookup finds the marker and a lookup at the first entry finds none, before and after close and reopen. The retained-prefix test verifies the exact native refusal reason, one durable prefix, no suffix and a subsequent commit.

On Go 1.27.3 with `CGO_ENABLED=0` and `GOFLAGS=-p=1`, nine focused groups passed twice at shuffle 42. `go vet ./durable` and Staticcheck v0.8.2 passed. The two compiled controls failed at the expected checks: committing early exposed an unreadable SQLite prefix to the viewer, and ignoring the historical bound returned the marker at the first entry. The early-commit control returned an error, not rows or receipts.

The [assertion ledger](assertions/durable-storage.jsonl) records every assertion in four selected case spans. Go exposes a count and `CommitResult.Kind()`; it does not expose the JavaScript result object or `Error:` string formatting. The in-flight read uses one public SQLite scan; upstream uses two Chord lookups. Chord's outbox, export table rows, imported archives, replayed replicas, host batches, statement counts and JavaScript garbage collection have upstream execution evidence only in this comparison.
