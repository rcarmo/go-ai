# Unchanged Durable generation and context originals

This bounded packet owns the five exact pi v1.7.0 Durable originals below at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The isolated native lane starts at `d557b0a191976d8bf0c78e54488cf02162dfe062`. It complements, rather than reclassifying, the earlier transcript/harness batch: generation and generation recovery were unrepresented, and the context-range, continue and compaction receipt is narrowed to the public SQLite contracts in these selectors.

## Pinned originals

| File | Git blob | SHA-256 | Result |
| --- | --- | --- | ---: |
| `harness-generation.test.ts` | `c74e856699464ceac76dad54656016c6e4d1ba15` | `f30df0dc101da156dc366ff08961ac9d346d549b2fc158461e9a5df4030d54d6` | 26 |
| `harness-generation-recovery.test.ts` | `d88c34ea70e9801b0ef4eb29ab504cb0fb29f111` | `3af8f443ad4e08b3907e5150fe1f21333142653825464eed8f285a73366cba46` | 9 |
| `harness-compaction.test.ts` | `cc6b701e1f7a144ea1347312569fcebe27525340` | `092378ce9747ec53213c493e021cc5d3ee21d07132311360201c7c794a090a3c` | 114 |
| `harness-context-range.test.ts` | `d44cab3db53bc2ce4776967234418525920194db` | `acd35d7585a3ac1063c4f335a7779d5b86e3422559707e8c3e2b408cce09d2ca` | 1 |
| `harness-continue.test.ts` | `627e6218fd5400d5213808b15f500ba240b6668b` | `c6021bdd60c8bbc8e53232b177cb37894c41e7208c698a45942144c8d8e57081` | 13 |

Every staged file matched its pinned Git blob and SHA-256 byte for byte; the detached checkout remained unchanged. Node 26.13.0 and Vitest 4.1.12 ran all five files together, one worker at a time with `NODE_OPTIONS=--max-old-space-size=650` and the shared `/tmp/go-ai/v170-focused-verify.lock`: **163 passed, 0 failed, 0 skipped**.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock /tmp/go-ai/v170-focused-verify.lock \
  node ../../node_modules/vitest/vitest.mjs run --maxWorkers=1 --fileParallelism=false \
  test/harness-generation.test.ts test/harness-generation-recovery.test.ts \
  test/harness-compaction.test.ts test/harness-context-range.test.ts \
  test/harness-continue.test.ts
```

## Native public SQLite comparison

The production paths use SQLite, `Harness`, `ConversationHandle`, commit publications, real provider request channels, persisted submissions/tasks and public context views. The controls cover:

- generation admission, attribution, durable retry deadlines, partial progress, cancellation, provider state and terminal reopen;
- interrupted preparation/request, committed-partial conversion, retry and deferred poll recovery without redispatch;
- continue without a new user entry, aborted tool handoff, busy/request-ID admission and length-limit boundaries;
- manual, automatic and overflow compaction, failure/no-model/cancellation, policy and retry ordering;
- off-line context scans that must not advance past an entry committed by a concurrent writer.

`TestV170GenerationProviderSessionsStayConversationLocal` proves stable, request-local provider identities across two concurrent conversations. `TestV170ContextRangeRescansEntryCommittedDuringOffLineScan` reproduces the unchanged `harness-context-range` race through a wrapped real SQLite scan: the snapshot contains only the entry admitted before the scan, a concurrent commit succeeds while the scan is paused, and the next public read contains that missed entry plus the later entry.

The context-range test first failed by timing out because a delegated storage scan held the Session admission line. The demonstrated fix in `context_read.go` releases that line after capturing the pinned snapshot and tail, performs the wrapper scan off-line, and does not cache the delegated result. A concurrent commit can publish without the stale scan becoming the current retained range. No ownership, redaction, cancellation, storage schema, model, request or limit behavior changed.

The first production fix remains narrowly scoped to this race; the existing generation, retry, deferred, continue and compaction controls passed before it and after it. The representative locators are retained in the assertion ledger; `partial` sites disclose JavaScript private helper identity, Chord/MemoryStorage-only observations and cases whose observable SQLite result is broader than a one-to-one Go API shape.

## Reproduction and limits

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
flock -w 300 /tmp/go-ai/v170-focused-verify.lock \
  go test ./durable -run 'SELECTOR' -count=2 -shuffle=42
```

`SELECTOR` is the exact native locator set in the lane receipt. It runs twice, followed by focused `go vet ./durable` and Staticcheck `v0.8.2 ./durable` under the shared pure-Go 650 MiB environment. This packet is a scoped unchanged-source comparison, not full acceptance or acceptance profiling; it does not claim JavaScript object identity, private Session counters, Chord tables or alternate storage-engine behavior.
