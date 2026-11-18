# Upstream v1.5.2 restart

The working target for resumed parity work is [pi v1.5.2](https://github.com/earendil-works/pi/releases/tag/v1.5.2), commit `b4afdddf44e059c67ccf5097e5317d913521b470`. The tag resolves to that commit, GitHub lists the release as stable, and npm publishes pi-ai, pi-durable and pi-codemode at 1.5.2. Use that exact release, not moving `main`. The v1.5.0 register and baseline 39 remain historical evidence; this does not establish a v1.5.2 baseline or change the acceptance scripts' pin.

## What changes the plan

The release notes mostly cover CLI session-directory behavior, outside this port, but [the tag diff](https://github.com/earendil-works/pi/compare/v1.5.0...v1.5.2) is larger than the notes imply. Direct tree comparison found 121 changed Durable path entries, 84 AI, 7 Codemode, 19 Schema and 1,037 Chord. These are file counts, not counts of behavior changes or port obligations. Chord's relevant subpaths are in scope; its unrelated apps and CLI are not. Pay particular attention to:

1. **Durable persistence and recovery.** Port the SQLite grouped-commit tests first: exact receipts and refusal prefixes, address replacement, and reopening. Then measure native statement growth against the source contract. Work on the public Storage bootstrap and database compatibility together so storage-layer changes do not discard history. Carry forward Generation2 and Tool3 safe/unsafe restart assertions on real SQLite fixtures; classify older native tests against the new source rather than assuming they are obsolete.
2. **Public runtime changes.** Keep the Storage admission and cold-read work already landed. In addition to `Tx.submission`, v1.5.2 adds conversation-level compaction settings and changes thinking budgets and WebSocket connection options. Reconcile those at their actual public boundaries; do not combine them with a package-wide rename pass.
3. **Dependencies and acceptance.** Inventory changed tests and relevant Chord imports before switching the repo-wide release gate. Pin exact published model assets and test dependencies; pay particular attention to provider request shapes, schema equality and Codemode. Continue focused tests only; defer a full run to a designated sweep. Keep original runs, native ports and fault controls separate.

## First resumed slice: committed submission lookup

The unchanged new case in [`harness-submit-in-commit.test.ts`](https://github.com/earendil-works/pi/blob/b4afdddf44e059c67ccf5097e5317d913521b470/packages/durable/test/harness-submit-in-commit.test.ts) submits an input, waits for settlement, and reads the submission and its transcript entry inside a later commit. It also checks that an unknown ID is absent. The v1.5.2 public interface now includes `Tx.submission`; the existing implementation had been private in Go.

[`TestSourceV152TxSubmissionReadsPlacedInput`](../durable/transaction_submission_v152_test.go) failed before the runtime change (0.408s). The port exports `Tx.Submission` and routes the existing abort consumer through it; the table-write fence and lookup behavior are unchanged. The test checks exact `done`, conversation ID, `pi.user`, and missing-ID results through `Harness.Commit`. Four focused Go groups, shuffled twice, passed in 2.513s. One compiled control detected dropping the entry link. Vet, Staticcheck, logging and diff checks passed.

Unchanged v1.5.2 originals passed: `harness-submit-in-commit` (24), `sqlite-commit` (12), `harness-tasks-recovery` (25) and `harness-tools-recovery` (10), no skips. The Harness tests ran with the pinned source and the published 1.5.2 pi-ai provider JSON (44 files from the integrity-checked npm tarball), not the live-generated catalogue. The SQLite source result is a reference run, not a native grouped-commit acceptance result. No full Go suite ran.

## Grouped SQLite transport and refusal prefix

Pinned `sqlite-commit.test.ts` exercises incarnation replacement in either write order and bounds statement growth for groups of one and 150. The native port was red in 4.215s: it returned the wrong refusal reason and issued 152 SQL statements for 150 small batches (5 for 3).

The public commit now preserves the accepted replacement, refuses the third incarnation with the source reason, and omits all later batches. The Go tests check the receipt, current address and absent IDs both live and after reopen. Native frame storage writes compact groups in one `json_each` insert with a 512 KiB argument cap. Large frames bind exact blobs directly in reusable power-of-two statement shapes below 100 parameters. Tested native groups of 1, 3 and 150 each issue 3 SQL statements including transaction boundaries; 3 and 150 execute identical SQL. Tested oversized compact groups split by payload size, and `EXPLAIN QUERY PLAN` finds no stored-table scan in the exercised compact inserts. This is not the upstream eight-statement layout or upstream SQLite file compatibility: those require the canonical schema and its indexed checks. The six generated upstream random cases and full schema query-plan corpus still need native mapping.

Focused Go group, rollback, uncertain acknowledgement and crash/reopen tests passed in 21.822s (two shuffles). Three compiled fault controls detected and were restored: per-frame SQL, stale refusal text and a doubled array cap. All 12 unchanged v1.5.2 SQLite source tests passed again; vet, Staticcheck, logging and diff checks passed. No full suite.

## Read-only SQLite connection

A `SQLiteOptions.ReadOnly` connection now opens an existing native file in SQLite `mode=ro`. It does not acquire or release the writer's ownership lock or issue schema or configuration writes. It rejects allocation, commit and reclaim. When SQLite reports an external change, it reloads the checkpoint and frame tail from one SQL snapshot. The focused viewer follows a writer across a commit, a reclaim and another commit; the writer remains exclusive after the viewer closes. An absent path is not created.

Red before implementation: 0.252s (the viewer tried to acquire the writer lock and created a missing database). Focused two-shuffle: 6.320s. A stale-prefix fault control was detected and restored. Vet, Staticcheck, logging and diff checks passed. All 5 source `harness-read-only` tests passed; that source result is not native Harness acceptance. The native Harness still needs read-only admission, scheduling and cache policy. Original SQLite schema compatibility remains open.
