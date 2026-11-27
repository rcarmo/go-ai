# Unchanged Durable entry, replay and view shapes

This bounded batch ports neutral assertions from four unchanged pi v1.7.0 Durable suites at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. It is based on native `033fea39d5803fad264edca7e67670226be64b30` and is disjoint from the session batch.

## Pinned originals

All files matched the pinned Git objects and were byte-identical to the retained private Durable stage. Chord tables, outbox/archive replay, and IndexedDB/JSONL/Memory backend semantics are outside this batch.

| File under `packages/durable/test/` | SHA-256 | Cases |
| --- | --- | ---: |
| `harness-input-entries.test.ts` | `576cb9f8347b45287a468a55e6e45972d983edc09904e9e38c0e3e76952e29bf` | 12 |
| `harness-view.test.ts` | `cccc828f5697f731c878f37ff04b278e79457d1bc9913d29398f2763395c3183` | 23 |
| `harness-view-run.test.ts` | `a920a476102f878f220836b5afce32c57ccb926decf23d1e2ecad001dcfaed70` | 13 |
| `harness-inspect.test.ts` | `685d8063e16b24310d3270d174803368e4d62bcbbcbf91e308e455fc41e28b90` | 2 |

The four unchanged originals ran with Node `v26.13.0` and Vitest `4.1.12`: **50 passed, 0 skipped**. The run used the shared lock with `GOMEMLIMIT=650MiB`, two Go workers, one build package and an isolated network namespace. The selected files and config came from the verified private stage; no source was edited.

## Native comparison

The unchanged native implementation needed no production correction. Existing SQLite/public-path tests cover input entry placement and canonical inbox authority, view hydration/identity/window replay, operation reconstruction, lifecycle and detached values, and inspection of unsettled work.

Focused view, input and inspection selectors passed twice with shuffle 42. A wider view selector initially included `TestMainBuiltinCanonicalPublicationsV150`, an unrelated existing generation-version failure, so that baseline failure was excluded rather than treated as evidence against these four files. The scoped selectors then passed: view/window/operation/input behavior in 54 seconds, followed by a five-test comparison run in 6.7 seconds.

No JavaScript identity or Chord-only storage assertion is represented as native object-identity or table parity. The assertion ledger records only deterministic public SQLite behavior and explicitly labels shape adaptations.
