## Internal group settlement

Native storage now has an internal multi-batch settlement path. Legacy `Apply` uses its single-batch branch and preserves its public signature, original validation errors, ownership and append fault hooks. Session still prepares, settles and publishes serially; it does not submit groups.

`durable/storage_group.go` defines private result, refusal and frame types. A normal result contains the accepted-prefix count, last seq and an optional refusal pointer. Undefined has a distinct sentinel; null, false, zero and empty string remain present values. `refusalError` produces a non-nil Go error for every present refusal and returns the original native error where available. These control values are never durable JSON or additional accepted input types. The tests exercise the projection, not a JavaScript bridge.

`prepareCommit` retains the existing strict validation, encode/decode detachment and immutable candidate preparation. Input validation failure refuses that batch and its suffix. Backend failure rejects the call instead; a confirmed rollback's StorageRejected is not converted into a normal refusal. Error returns carry no normal result: callers retain their received durability evidence separately.

### Backend behaviour

- SQLite prepares the accepted prefix, writes its frames in one transaction, then adopts the candidate and emits ordered synchronous receipts. Each batch retains its own seq/frame. No schema, checkpoint or journal-format change.
- Memory and journal settle and adopt each batch in order. A later error preserves confirmed earlier batches and receipts. Their series is not a single crash-atomic SQLite transaction.
- An internal receipt sink can reject the call after durability. SQLite has already stored the full accepted prefix; memory/journal have reached the delivered part. The sink must not synchronously reenter storage or invoke untrusted callbacks. Session publication is outside this primitive.
- One core admission covers preparation, settlement and receipts. Cancellation after admission cannot abandon settlement. Close seals new/queued work and joins the running group; Session's writer capability remains required for owned storage.

A group accepts at most MaxWrites batches and at most MaxFramePayloadBytes of encoded accepted payload. Exceeding the payload cap refuses the next batch; exceeding the batch-count cap refuses the call before writes. Individual batch, record, state, ancestry, ID and JSON-authority limits remain unchanged. A future Session writer must split prepared work at those bounds so grouping alone does not turn valid individual commits into refusals.

There is no exported group interface or automatic optional-interface dispatch. Legacy one-batch Apply still calls appendFrame, including wrapped fault hooks. Future Session overlap must retain wrapped Snapshot gates and cannot infer permission to bypass them from a promoted method.

After a SQLite group commits, the core image already contains every stored part. It cannot serve as the Session's per-publication read image while later parts are unpublished. The existing exact-backend taskSnapshot fast path relies on the current serial Session line. The later [internal publication image](durable-publication-image.md) separates exact-backend task/context reads and rejects non-contiguous adoption. Native public/point/legacy read projection now also uses that image. Wrapped projection and core-admission/dependency gates still need integration before overlap; exposing the final raw image early would invalidate that optimisation's premise.

### Frame-bearing receipts

`commitParts` now delivers a private receipt containing batch index, seq and that part's detached encoded `storageFrame`. `commitBatches` preserves its existing index/seq sink through this path, so legacy Apply and all existing settlement/fault checks use the same implementation. No wrapper dispatch or exported API is added.

`commitReceipt.nextImage` reconstructs one prefix from the prior published image. It checks contiguous seq, frame kind/ordinal/high-water, strict payload limits and the record's seq, then runs the existing immutable preparation. Receipt payloads are private and immutable. They share the group's existing aggregate encoded-byte and count bounds. Storing receipts retains encoded payloads, without an array of full materialised prefix images. Transient preparation/GC memory still requires acceptance measurement.

Reconstruction deliberately repeats strict decode/preparation after writer validation. Its CPU/allocation cost has not been optimised or measured; this is a correctness baseline. Session still uses its existing serial single-batch publication and does not yet use frame receipts in production.

`storage_group_receipt_image_test.go` verifies each prefix against a separate sequential memory store, including entries, document revisions, changing document values, detached caller mutations, public isolation, allocator continuity and a following legacy commit. It also covers normal prefix refusal, sink failure after the first reconstructed part, malformed/wrong/gapped receipts and failed-reconstruction base isolation. The SQLite raw image contains all three parts while public Session snapshots progress through seq 0/1/2/3. A mutation binding every SQLite receipt to the last frame fails the prefix test; its overlay is deleted after analysis.

Fresh full root normal tests/vet pass in both CGO modes; module verification passes. New frame/image checks and existing storage-group, journal/SQLite crash, lifetime and Session-image controls pass three race repetitions with shuffle 7/42. No dependency, storage format, asset or release-ref changes. The earlier full durable race pass at a5dedbd predates this change.

### Checks

Go 1.27.3, GOMAXPROCS=4, owned empty HOME/APPDATA, empty ambient credential-file selectors and project-namespaced scratch:

- Full root normal tests pass with CGO disabled and enabled; root vet passes in both modes. Module verification and dependency/accepted-asset tripwires pass.
- New native group tests have 19 leaf cases without CGO and 32 with CGO. They cover ordered receipts/seqs, input detachment, reopen/ID allocation, three prefix-refusal positions, sink errors after receipts, falsy/null/undefined projection, empty calls, ownership, sequential backend failure, payload/count bounds, cancellation and Close.
- SQLite receipt probes start and roll back a fresh transaction on the actual connection. They succeed only after the group transaction has ended. A group uses one append-series call for three frames.
- Real SQLite faults abort the second INSERT, fail COMMIT with a deferred foreign-key constraint, or end the transaction before rollback confirmation. Both table prefixes verify no receipts, unchanged adopted seq/ordinal/high-water, confirmed-rollback usability versus uncertainty poison, empty recovery, successful retry and non-reused IDs.
- A Go overlay deliberately changes the writer to one transaction per frame. Four rollback/recovery cases fail because the first frame survives. The unmodified writer passes. The overlay and logs were deleted after analysis.
- The 32 group cases plus eight earlier Session/SQLite controls pass three race repetitions with shuffle seeds 7 and 42. CGO-disabled group/Session checks pass three normal repetitions with seed 42.
- Scoped staticcheck 0.8.2 reports only the existing `durable/usage_json.go:11` U1000 finding. An initial test compared an interface known to hold a concrete pointer against nil; it was replaced by testing the actual refusal-to-error projection. The wider staticcheck gate is still open.

Diagnostic normal-test timings were 114.610 seconds for durable without CGO and 109.437 seconds with CGO; the longest top-level test was about 4.13 seconds. These are separate workloads and do not measure an optimisation or explain the earlier race timeouts. The later full durable race run passes at a5dedbd in 631.158 seconds with per-test events and a 1200-second timeout; see [publication-image checks](durable-publication-image.md). No acceptance profiling was performed. Disposable logs, overlays, fixtures and build scratch were removed after analysis.

### Remaining implementation

The two source corpus rows remain development-verified-partial. Later [queue, process-crash and collection controls](durable-group-lifetime.md) cover same-core callers, SQLite/journal exits and after-return input/receipt collection. [Harness scope review](durable-harness-scopes.md) fixes four retention cases. Public group storage, Session disjoint preparation, dependency/scan gates for overlapped work, ordered adoption/publication, continuation-aware host integration, deferred persistence, cache write-through, cross-owner/export isolation and in-flight/async lifetime still need implementation or dedicated validation. Source statement-count guards use another schema; native framing needs a separate measured cost disposition.

Follow [the group contract](durable-group-contract.md) before enabling Session overlap. The fixed 64-write staged-validation cost experiment in [cache storage](durable-cache-storage.md), full semantic review, acceptance and pre-release CPU/alloc_space/alloc_objects profiling are unfinished.
