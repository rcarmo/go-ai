# Unchanged Durable session originals, batch 1

Nine unchanged pi v1.7.0 Durable test files were selected from the exact pin `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The native lane started at `033fea39d5803fad264edca7e67670226be64b30`, the exact `origin/main` tip at selection. This packet owns session adoption, tails, deferred commits, definitions, documents, forks, group commits, observers and watches. It does not claim scheduler/storage-usage coverage, Chord-only tables/outbox/archive concepts or alternate backends.

## Unchanged originals

| File under `packages/durable/test/` | SHA-256 | Passed |
| --- | --- | ---: |
| `session-adoption.test.ts` | `3d185c615fa6e39d8fea1276b0497430d8c86f22123904435608981db5945181` | 13 |
| `session-conversation-tails.test.ts` | `e7fc989babae389e6f035bda5e415f4984b9835c945ca292de375c84fa27762c` | 5 |
| `session-deferred-commits.test.ts` | `e3d7b5132cf5192e0b0ff69aa5356024769eb1359ef2a85a740deb7cd93d54e7` | 25 |
| `session-definitions.test.ts` | `b7347699bc946373ca9bb2dcb788df63439e7ae730474fbf63a16efaf7e01158` | 4 |
| `session-documents.test.ts` | `0fdbef87e9a65600f81a7df30d755a5448331e141869448285e51995bc5c951c` | 27 |
| `session-forks.test.ts` | `109b19f413e5ded65f4761a825b13f8e744fa858fe75e25969dba40c22142a21` | 14 |
| `session-group-commit.test.ts` | `d7bc4cd24c9e08493c5434172fa28f022c0d6b490208730111375872b4b17511` | 40 |
| `session-observers.test.ts` | `15f5844f84d68aa987f73df505260d6240954b27b01744e444eaa1c1363cc1a3` | 22 |
| `session-watches.test.ts` | `e38a489b35e796c88786db61868e621d831d93140f17dc1180c2555e20540a35` | 28 |

Each file matched its pinned Git blob byte for byte and the checkout remained detached at the pin. Vitest 4.1.12 on Node 26.13.0 ran all 9 files with a 650 MiB Node heap cap under `/tmp/go-ai/v170-focused-verify.lock`: **178 passed, 0 failed, 0 skipped**. An initial harness run did not count as implementation evidence: eight suites could not load the missing `@earendil-works/pi-ai` `dist` exports, while the five tail-only tests passed. Building the exact AI workspace supplied that dependency; a rerun passed all 178 assertions without changing any original test.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock /tmp/go-ai/v170-focused-verify.lock \
  node ../../node_modules/vitest/vitest.mjs run \
  test/session-adoption.test.ts test/session-conversation-tails.test.ts \
  test/session-deferred-commits.test.ts test/session-definitions.test.ts \
  test/session-documents.test.ts test/session-forks.test.ts \
  test/session-group-commit.test.ts test/session-observers.test.ts \
  test/session-watches.test.ts
```

## Native controls

The unchanged production tree was compared through existing public/production SQLite paths before any lane change. Nine selected top-level controls passed, with no skips:

| Original file | Native production-path control |
| --- | --- |
| adoption | `TestSourceSessionHonoursPublicStorageDecorator` |
| conversation tails | `TestSourceV152TranscriptReadGates` |
| deferred commits | `TestMainSessionDeferredInfiniteWindow` |
| definitions | `TestDocumentDefinitionMigrationSnapshotsRequiredBaseAndRollback` |
| documents | `TestSourcePublicStorageLoadsColdDefinitionByAddressAndID` |
| forks | `TestMainViewUpstreamHistoryOracles` |
| group commit | `TestSessionV150OutstandingJobsBound` |
| observers | `TestMainPublicCommitObserverDispatchMembershipAndCanonicalValue` |
| watches | `TestMainWatchPendingDelivery` |

The controls run under Go 1.27.3, `CGO_ENABLED=0`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`, `GOMEMLIMIT=650MiB` and the same shared lock. These are representative public/production controls, not a one-for-one claim over all 178 original assertions. The [mapping ledger](assertions/durable-unchanged-batch1.jsonl) records the limits.

A separate observer failure-value control (`TestMainPublicCommitObserverArbitraryPanicValue`, four subcases) and host-callback redaction fence (`TestTaskRecoveryHostCallbackFencesPreserveNormalErrorIdentity`, three subcases) also passed. They confirm the narrow callback and host-error boundaries used by this packet; they do not certify unrelated scheduler recovery paths.

An exploratory pre-existing matrix, `TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts`, was also run and failed all eight preparation/dispatched failure leaves at its callback-receipt disposition assertions on unchanged `033fea39`. It is outside the owned original set and was not treated as a batch regression or used as a passing guarantee. No production fix was made; that result remains an explicit follow-up rather than being hidden or folded into the selected controls.

## Static and storage limits

`go vet ./...` and Staticcheck v0.8.2 `./...` passed under the bounded pure-Go environment. No production file, backend selection, redaction policy or callback implementation changed in this lane. All selected native storage fixtures use SQLite only; no Memory/Journal or Chord backend was added as a substitute. Full Chord session tables, archive replay, outbox delivery, JavaScript retention ordering and TypeScript type checking retain upstream execution evidence only.

This was a bounded development comparison, not full acceptance or acceptance profiling. The packet has no hidden skips, no provider/network assertions and no production fix to rerun.
