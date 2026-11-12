# Submission record reads and raw replacement

Primary submission reads now return the pinned lifecycle records across Storage, Session and Tx. Raw storage accepts complete replacements and duplicate request keys. This slice follows `12c3981`; full upstream acceptance remains open.

Target: pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## API and control flow

- `Storage.Submission(ctx, id)` and `SubmissionByRequest(ctx, conversation, request)` replace the missing scalar getter and native `Request` method. `Submissions(Query)` is removed. `ScanSubmissions` returns `Page[SubmissionRecord]` across Storage/Session/Tx.
- Records expose source queued/placed/done/unanswered states and source optional fields. Native private input payloads remain in snapshots; primary readers do not expose them through `Value`. Incomplete native envelopes are not given invented lifecycle fields.
- Status filters use the public lifecycle, including placed and unanswered, instead of private pending/failed spellings. Cursor ordering and pagination use the existing source ID cursor.
- `Tx.Submission` reads committed state before the first table write and returns `ReadAfterWrite` afterwards. `Tx.SubmissionByRequest` uses the same fence. Native tests that previously read staged lifecycle records now check committed records; trace/null-detail/first-settlement/ownership assertions remain.
- Session getters touch exactly the submission or request key. Request lookup does not additionally touch the conversation or submission record key. Native reads use the published image; installed wrappers remain authoritative. Cancellation causes and first backend failure identity retain precedence.
- Raw replacement may change conversation, type, request key and optional presence. Old keys no longer resolve the moved record. Raw continue records may reference an unpersisted answer ID, as the literal upstream case does.
- The source SQLite request index is non-unique. The independent probe reproduces first-indexed-row lookup for duplicate keys and reindexing after replacement. Original MemoryStorage instead tracks the latest writer for duplicate keys; that behaviour is not claimed as SQLite parity. The native engine remains SQLite-only.
- Raw transaction creation does not apply Harness deduplication. Harness dedup now uses the primary request lookup instead of an unordered native map walk. The old walk could choose a different duplicate and report a cross-type conflict. The regression checks repeated dedup results and no replacement write.

These changes retain the native database envelope. They do not establish source database interchange or direct canonical builtin execution. At this checkpoint creation/placement/settlement were still native. The [transaction follow-up](submission-transactions.md) now ports complete creation results, dedicated public argument unions, deferred change assembly and original error/predicate order. The Harness still has its historical split lookup/write admission, so complete source admission atomicity/order is not accepted by the dedup lookup regression alone.

## Test-first evidence

Three literal cases from `src/testing/storage-conformance.ts:778–957` are translated without changing their lifecycle/index assertions:

1. Per-conversation request indexes, complete input replacement and all original filtered one-item cursor pages.
2. Passive write queued/done/unanswered records, including an allocated but unstored entry ID and exact detail.
3. Entryless continue placed pages and done/unanswered records, including an allocated but unstored answer ID.

The initial native packet failed on missing primary API/result types, raw identity replacement and the missing Tx request getter. A direct existing Tx submission read incorrectly succeeded after a table write. Separate duplicate-key controls failed with `request ID conflict`, both in raw storage and raw Tx creation. The Harness dedup regression then failed with a cross-type conflict from the unordered map loop.

The source cases and independent probes pass against the retained original. Test-first bridges used to compile the missing APIs are removed; native acceptance calls the primary methods directly. [Assertion mapping](../docs/upstream-v1.5.0/task-table-assertions.tsv) distinguishes literal cases and source-derived controls.

Ownership controls retain original detail and trace through input mutation, scalar/request/scan read mutation, SQLite reopen and reclaim/reopen. Empty and absent request keys remain distinct through explicit replacement. Existing raw tests that froze identity, required unique keys or required persisted/visible continue answers were incompatible with source Storage and now assert the source behaviour. Runtime-helper validation is not presented as raw Storage conformance.

## Development verification

- Full durable **415.914s**, tools **19.304s**.
- Ten focused shuffled repetitions, seed42: **9.552s**.
- Repository-wide vet, Staticcheck v0.8.2 and logging pass.
- Ten independent compiler-overlay faults fail semantic assertions: raw identity freezing; raw request uniqueness; raw answer visibility; private status filtering; raw rather than published submission image; raw rather than published request image; omitted submission dependency; omitted request dependency; borrowed detail payload; disabled Tx submission read fence. Disposable overlays/scripts are deleted.
- Reference: **155 unchanged source cases +43 independent probes**:147 tests from seven original files, eight unchanged Storage conformance cases,35 admission probes and eight Storage/transaction probes. Hashes and the complete pinned test/support tree are checked; the retained snapshot has no `.git`, so commit identity is not independently reverified.

The first full run found one remaining native staged-read fixture for empty/absent trace. Moving that lookup after commit preserves the trace assertions and follows the source table-read fence; the subsequent full run passes. The earlier slice's queued progress/close error-order caveat remains open under D20.

These are development receipts, without race coverage or pre-release CPU/allocation acceptance. D04 continuation turns, canonical builtin execution/migrations, remaining document/record/API contracts, lifecycle/readonly/viewer coverage, the full assertion audit and release profiling remain open.
