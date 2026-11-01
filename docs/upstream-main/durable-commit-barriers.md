## Durable commit barriers at 81f3cd41

Six native subcases verify existing publication barriers and SQLite settlement behaviour. They pass without production changes. `session_group_commit_main_test.go` adapts two source gate assertions; `storage_sqlite_commit_main_test.go` exercises rollback/recovery and poison controls using real SQLite failures. Both source corpus rows stay `development-verified-partial`.

### Verified controls

| Source requirement | Native test and scope |
| --- | --- |
| Read an entry ID leaked by an unpublished commit only after publication | Hold the first memory-store append, attempt public Snapshot admission, verify it remains pending, then release and read the entry. Source uses `tx.entry`; the native adaptation uses public Snapshot |
| A write to an unpublished conversation waits for creation | Hold creation at storage settlement; a second Commit appending to that conversation stays pending, then succeeds after release |
| A statement failure with confirmed rollback leaves the store usable | Abort a frame INSERT using a SQLite trigger. Preserve the earlier entry/seq, expose no failed entry/receipt, remove the trigger, retry at the next seq, reopen and verify both entries and non-reused IDs. Run with empty and custom table prefixes |
| Failed COMMIT stops the store | INSERT succeeds, then a deferred foreign-key constraint fails COMMIT. Require ErrPoisoned, no receipt, blocked reads/mints, close/reopen without the failed entry, and successful retry |
| Unconfirmed rollback stops the store | A trigger ends the transaction before explicit ROLLBACK can confirm settlement. Preserve the conservative ErrPoisoned fence and reject reads |

The four SQLite subcases are two prefix variants plus the COMMIT and rollback-confirmation controls. The two Session subcases use an observed context admission select and channel gates; there are no sleeps to infer admission. A five-second context deadline bounds a failed test. The initial memory fixture probe called a nil optional append hook; its harness now handles an absent underlying hook. This was a test-fixture error, not a production failure.

`SQLiteStorage.append` already distinguishes successful statement rollback (`StorageRejected`) from unconfirmed rollback/COMMIT errors. `storeCore.settle` leaves the former unadopted and poisons the latter. These tests preserve that classification. They do not add group commits, deferred persistence or new receipt callbacks.

### Open differences

The first source Session batching test is a concrete architectural gap: while commit A is in flight, callbacks B/C touching disjoint documents run before A settles; the next storage call batches B/C together. Native `Session.commit` holds its single line through preparation and storage settlement, so B/C wait. Source examples also require key-aware gates, unkeyed-scan barriers and held/deferred document changes. Broad native serialisation satisfies the two gate controls above but cannot close the disjoint-callback requirement.

The raw source storage call accepts several batches, assigns each its own seq/outbox commit, stores their accepted prefix in one backend transaction and delivers ordered receipts after COMMIT. Native `Apply` handles one batch/frame/seq. The tests above do not verify source multi-batch atomicity, prefix refusals, receipt callback ordering, crash tests or queue isolation. Source cost guards assert five statements for one append and thirteen for three in one call; the header's shorthand is not the assertion. Native framing has a different schema and requires a separate storage-format/cost disposition.

Source publication yields between parts of a call so a settled promise's continuation runs before the next publication; it also supports storage-owned publication scopes. The two native barriers do not verify outcome delivery/continuation ordering. Any Go pipeline must define that host contract explicitly instead of assuming goroutine scheduling matches JavaScript microtasks/macrotasks.

Next implementation prerequisites:

1. Review the remaining assertions in `session-group-commit.test.ts` (34 direct declarations, some inside parameter loops) and `store-storage-commit.test.ts` (16 direct declarations plus three tables).
2. Specify the native multi-batch receipt/refusal boundary while retaining legacy Apply and wrapped storage fault gates. Define unknown-outcome, partial-receipt and Close/drain behaviour before changing admission.
3. Separate preparation, storage settlement and ordered publication; capture keyed read/write dependencies and global scan barriers. Preserve ownership, cancellation, byte/record limits, detached public values and actual host joins.
4. Then connect point-read caches and deferred writes to publication. Do not introduce an unused cache or weaken barriers to obtain concurrency.

### Checks

Go 1.27.3/GOMAXPROCS=4 affected race tests pass three repetitions (six subcases per repetition); root normal tests, root vet and module verification pass. The native baseline is 5f8e391. No production code or dependencies changed. Full source group-commit parity, receipt/adoption design, the prior full durable race suite gate and pre-release CPU/allocation profiling remain incomplete. The fixed 64-write validation-cost follow-up from [cache storage](durable-cache-storage.md) is still owned work for storage adoption.

Pinned references: `packages/durable/test/session-group-commit.test.ts` lines 170–254 and `packages/durable/test/store-storage-commit.test.ts` failure/rollback sections, commit `81f3cd412758ebdd92324c7d63b669a189408d0a`. Temporary excerpts and failed-probe artifacts were removed after analysis.
