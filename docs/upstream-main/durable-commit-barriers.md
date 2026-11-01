## Durable commit barriers at 81f3cd41

Eight native subcases verify existing publication barriers and SQLite settlement behaviour without production changes. `session_group_commit_main_test.go` adapts two source gate assertions, `session_scan_barriers_main_test.go` adds keyed-entry/global-conversation scan gates, and `storage_sqlite_commit_main_test.go` exercises rollback/recovery and poison controls using real SQLite failures. Both source corpus rows stay `development-verified-partial`. Both source files have now been reviewed through their final assertions; [implementation requirements](durable-group-contract.md) cover the remaining contracts.

### Verified controls

| Source requirement | Native test and scope |
| --- | --- |
| Read an entry ID leaked by an unpublished commit only after publication | Hold the first memory-store append, attempt public Snapshot admission, verify it remains pending, then release and read the entry. Source uses `tx.entry`; the native adaptation uses public Snapshot |
| A write to an unpublished conversation waits for creation | Hold creation at storage settlement; a second Commit appending to that conversation stays pending, then succeeds after release |
| An unkeyed conversation scan and a keyed entry scan wait for creation | Hold creation inside the callback while the raw backend remains readable and lacks that conversation. Session ScanConversations must include it after release; ScanEntries on the new empty conversation must succeed with no entries/continuation. Removing both Session admission guards in an isolated Go overlay fails both cases |
| A statement failure with confirmed rollback leaves the store usable | Abort a frame INSERT using a SQLite trigger. Preserve the earlier entry/seq, expose no failed entry/receipt, remove the trigger, retry at the next seq, reopen and verify both entries and non-reused IDs. Run with empty and custom table prefixes |
| Failed COMMIT stops the store | INSERT succeeds, then a deferred foreign-key constraint fails COMMIT. Require ErrPoisoned, no receipt, blocked reads/mints, close/reopen without the failed entry, and successful retry |
| Unconfirmed rollback stops the store | A trigger ends the transaction before explicit ROLLBACK can confirm settlement. Preserve the conservative ErrPoisoned fence and reject reads |

The four SQLite subcases are two prefix variants plus the COMMIT and rollback-confirmation controls. The two Session subcases use an observed context admission select and channel gates; there are no sleeps to infer admission. A five-second context deadline bounds a failed test. The initial memory fixture probe called a nil optional append hook; its harness now handles an absent underlying hook. This was a test-fixture error, not a production failure.

`SQLiteStorage.append` already distinguishes successful statement rollback (`StorageRejected`) from unconfirmed rollback/COMMIT errors. `storeCore.settle` leaves the former unadopted and poisons the latter. These tests preserve that classification. They do not add group commits, deferred persistence or new receipt callbacks.

### Open differences

The first source Session batching test is a concrete architectural gap: while commit A is in flight, callbacks B/C touching disjoint documents run before A settles; the next storage call batches B/C together. Native `Session.commit` holds its single line through preparation and storage settlement, so B/C wait. Source examples also require key-aware gates, unkeyed-scan barriers and held/deferred document changes. Broad native serialisation satisfies the gate controls above but cannot close the disjoint-callback requirement.

The raw source storage call accepts several batches, assigns each its own seq/outbox commit, stores their accepted prefix in one backend transaction and delivers ordered receipts after COMMIT. Native `Apply` handles one batch/frame/seq. The tests above do not verify source multi-batch atomicity, prefix refusals, receipt callback ordering, crash tests or queue isolation. Source cost guards assert five statements for one append and thirteen for three in one call; the header's shorthand is not the assertion. Native framing has a different schema and requires a separate storage-format/cost disposition.

Source publication yields between parts of a call so a settled promise's continuation runs before the next publication; it also supports storage-owned publication scopes. The native barriers do not verify outcome delivery/continuation ordering. Any Go pipeline must define that host contract explicitly instead of assuming goroutine scheduling matches JavaScript microtasks/macrotasks.

Further reviewed source outcome assertions (lines 390–564) distinguish callback errors, which reject only that job, from fatal storage failure. Earlier outcomes remain valid; later jobs receive SessionFailed with the first cause. Parts already received can publish even when the enclosing storage call later rejects, while their outcomes still carry that call's error. Other parts of the same rejected call retain its rejection; intervening no-write jobs beyond the failure boundary receive SessionFailed. Rejection/refusal values include null and undefined, so presence cannot use truthiness. These cases still need a native receipt/value-domain mapping and tests; no native parity is established by this review.

Next implementation prerequisites:

1. Source assertion review is complete for these two files: the Session tail adds adoption/cache failure, deferred persistence, scoped publication and continuation failures, writer upkeep/close, 256-job bounds and five seeded interleavings; the store tail adds close/drain, crash recovery, publication batches, cross-owner/read/export isolation and batch-container collection. These requirements still need native implementation/tests; [the proposed boundaries](durable-group-contract.md) keep their failure and lifetime rules separate.
2. Specify the native multi-batch receipt/refusal boundary while retaining legacy Apply and wrapped storage fault gates. Define unknown-outcome, partial-receipt and Close/drain behaviour before changing admission.
3. Separate preparation, storage settlement and ordered publication; capture keyed read/write dependencies and global scan barriers. Preserve ownership, cancellation, byte/record limits, detached public values and actual host joins.
4. Then connect point-read caches and deferred writes to publication. Do not introduce an unused cache or weaken barriers to obtain concurrency.

### Initial checkpoint checks

Go 1.27.3/GOMAXPROCS=4 affected race tests pass three repetitions (six subcases per repetition); root normal tests, root vet and module verification pass. The native baseline is 5f8e391. No production code or dependencies changed. Full source group-commit parity, receipt/adoption design, the prior full durable race suite gate and pre-release CPU/allocation profiling remain incomplete. The fixed 64-write validation-cost follow-up from [cache storage](durable-cache-storage.md) is still owned work for storage adoption.

### Scan checkpoint checks

The scan increment adds no production changes. Go 1.27.3/GOMAXPROCS=4: all four Session gate cases pass three normal repetitions without CGO and ten race repetitions with CGO. All eight native barrier/settlement cases pass three normal repetitions with shuffle seed 42 and three race repetitions with seed 7. Root vet passes with CGO both disabled and enabled. The scan tests hold preparation with the core storage line available; their guard-removal mutation fails both new cases. The first mutation probe targeted a physical symlink alias that Go did not use and therefore passed; remapping the overlay to `go list`'s actual package directory exercised the mutation and failed as expected. Overlay files and probe logs were deleted after diagnosis. Full-suite race and pre-release profiling are still open.

Pinned references: `packages/durable/test/session-group-commit.test.ts` gate sections and newly reviewed lines 565–1017; `packages/durable/test/store-storage-commit.test.ts` failure/rollback sections and newly reviewed lines 515–769, commit `81f3cd412758ebdd92324c7d63b669a189408d0a`. Temporary excerpts and failed-probe artifacts were removed after analysis.
