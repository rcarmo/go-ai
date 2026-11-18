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

## Read-only Harness

`Options.ReadOnly` now separates inspection from startup recovery and scheduler activation. The Harness reads the current SQLite image, opens an existing root without changing it, and returns `ReadOnlySession` for the exercised writes and scheduling operations. A commit with no writes remains valid. Closing a viewer does not requeue the driver's tasks or publish wake notifications.

The running-driver test uses a real blocked model call, checks the transcript, `LiveDoc`, the running task, paused inspection and inactive wake, then sees a later writer commit from the same viewer. An empty database stays empty. The first test was red in 0.341s because Open attempted recovery. Focused two-shuffle: 5.609s; two compiled controls detected and restored (recovering on viewer open and serving a stale image). All 5 unchanged v1.5.2 source tests ran again, including the original JSONL-only case (not a native backend). Vet, Staticcheck, logging, the retained v1.5.0 register check and diff checks passed. The upstream schema and the native proof of writable recovery after a viewer close remain open.

## Inspecting an upstream-produced SQLite file

A read-only open can now read selected canonical records from the original SQLite schema, including exact stored entry sequences, running tasks, request-indexed submissions and historical document deltas. The native viewer reads one SQLite transaction and does not apply migrations. A writable open of an upstream file refuses before creating native tables, rather than silently overlaying two formats.

The [upstream producer](../durable/testdata/pi-durable-v152/generate-sqlite-viewer.mjs) creates the fixture using public v1.5.2 `Storage.commit`. The resulting schema-v2 database regenerated byte-for-byte (`c64fb90105d18dfc0b79f6224beb87a1e70a8219a2759383dc28cd246d39ce8f`). The unmodified Go backend rejected the file (0.018s); the focused viewer/codec/reopen regressions passed twice in 2.237s. Compiled fault controls detected and restored dropping stored deltas and permitting writable admission. All 83 unchanged upstream SQLite storage tests passed. Static checks passed. Native writable migration, durable SQL parity and v1-produced fixture acceptance remain open; the read-only projection is not a waiver for them.

## AI request options and tool-only assistant messages

The v1.5.2 output clamp reserves the larger of 4,096 tokens and 15% of estimated input, then applies the capped fallback when the proportional reserve leaves too little output. The seven source cases are ported through the public Go clamp; the fixed-only implementation was red for the proportional and near-full cases (0.021s).

OpenAI completions now sends explicit `null` for tool-only assistant messages by default. A model can require `""` without inserting an extra turn; the separate after-tool-result switch still inserts the extra turn. The three Go cases inspect the real HTTP request, including the presence of `content`. The default was red (0.034s). The model generator now carries the optional field, but the pinned model schema and catalogue have not been regenerated for 1.5.2.

Four packages compiled; the selected Go tests passed twice in `tests`, OpenAI completions and Responses. The two unchanged upstream files passed all 10 cases. Fault controls detected and restored a missing proportional reserve and an ignored non-null switch. Vet, Staticcheck, logging and diff checks passed. No full suite.

## Codemode reference and guest output changes

`ResolveCodemodeSchemaRef` now URI-decodes local pointer segments before JSON Pointer unescaping, preserves the returned object identity and rejects malformed escapes without fetching anything. Declaration expansion uses the same resolver. The unchanged v1.5.2 prelude extracted from the pinned source prints `NaN`, `Infinity` and `-Infinity` in console output and throws `NaN` with the source error message. Its embedded asset hash and SBOM provenance now reflect 1.5.2; the WASM asset is unchanged.

The focused resolver and production QuickJS output tests were red on the escaped-space lookup and `null` output, then passed twice (Durable 2.038s, QuickJS 0.753s). Both deliberate faults were detected and restored. The unchanged upstream declarations and sandbox files passed 175 cases, zero skipped. Vet, Staticcheck, logging, guest SBOM self-tests and diff checks passed. Broader Codemode lifecycle and replay parity remain open.
