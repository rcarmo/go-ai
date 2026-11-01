## Internal Session publication image

Native internal task/context reads now have an immutable Session-owned publication image, separate from the backend's latest image. This is groundwork for the group pipeline, not an enabled asynchronous publisher or a completed source test family.

The source-derived regression settles two batches through the real private group primitive without publishing either through Session. Raw storage contains seq 2/two entries. The old internal Session read incorrectly returned those entries on memory, journal and SQLite; all three baseline leaves fail. The new read stays at seq 0. The fixture uses the private owned writer deliberately; external callers cannot bypass Session ownership this way.

### Implementation

- Open captures the native immutable image after ownership/limits checks without adding a wrapper Snapshot call.
- Exact memory/journal/SQLite internal reads use the published image while retaining core admission, context cancellation, closing/poison checks and backend failure classification. Only allocator high-water is refreshed from the raw image: reservations are global and independent of publication.
- Context busy checks use published tasks/documents/conversations, not a later raw batch. This retains the shared idle predicate and its ownership/background/host/failed-input semantics.
- The current single-batch commit adopts its validated native image at the publication boundary. `adoptReadImage` requires the next contiguous receipt and the same image seq. It rejects final-group images passed for an earlier receipt, skipped parts and duplicates. It neither clones maps nor replays full state validation; its input is a trusted immutable native candidate.
- A production adoption mismatch after storage success fails the Session and closes it, instead of publishing an inconsistent view. Close drops the retained image. The existing close-only requeue writes intentionally bypass publication after hosts/observers have stopped; reopen reads their actual durable seq.

### Checks

`session_read_image_main_test.go` and `session_read_image_adopt_main_test.go` have twelve substantive CGO leaves/eight without CGO:

- Stored but unpublished group entries stay out of internal reads.
- Only the first receipted candidate becomes visible while the raw image already has the second; wrong image seq, gaps and duplicate adoption reject without advancing the image. After the second adoption, allocator and normal legacy commits continue at seq 3.
- Attempting a legacy commit past an unpublished prefix fails closed after storage, fences further reads and joins failure-driven Close.
- An unpublished raw task does not make a published context busy.

Go 1.27.3/GOMAXPROCS=4, owned empty homes and project scratch: full root normal tests and vet pass in both CGO modes; durable elapsed 116.305/111.356 seconds. These are different workload variants, not a performance comparison. All new leaves plus context/SQLite Snapshot/Session gate controls pass three race repetitions with shuffle 7; the earlier image/adoption set also passes seed 42. Non-CGO focused checks pass three repetitions with seeds 7/42. Module verification and dependency/asset tripwires pass. Scoped staticcheck 0.8.2 reports only the existing usage_json.go:11 U1000 finding; the wider gate is open. Baseline/verification logs and fixtures are disposed after analysis.

### Public Snapshot and ordered scans

Exact native `Session.Snapshot`, `ScanConversations`, `ScanEntries`, `ScanTasks` and `ScanSubmissions` now read the published image under the Session line and existing core health/admission gate. Snapshot clones the image; scans use the existing filtering, cursor and detached-page helpers. Allocator high-water remains independent of record publication.

`session_public_image_main_test.go` verifies stored-but-unpublished entry groups, conversation/task/submission visibility before and after adoption, Snapshot/page detachment, and post-Open adapter replacement. The entry/Snapshot baseline failed six leaves across memory, journal and SQLite. An overlay restoring both previous production files also fails the record-scan regression on all three backends. The overlay was deleted after use.

Fresh full root normal tests and vet pass with CGO disabled/enabled on Go 1.27.3, GOMAXPROCS=4; module verification passes. New public and prior internal image/adoption/fatal-read tests pass three race repetitions with shuffle 7; public tests also pass three race repetitions with shuffle 42. These are development correctness checks. The later full durable race run is recorded below; acceptance profiling is unfinished.

### Legacy and point-read projection

Native projection now lives in the guarded Snapshot read, shared by public Snapshot, typed entry reads, definition/current/historical reads, document-state values and commit-subscription baselines. All existing Snapshot call sites own the Session line. Exact native reads clone the published image; adapters still execute their own Snapshot implementation.

`ConversationHandle.Entries` now enters the Session line and reads a detached published Snapshot. Storage and Session share the legacy ascending seq/position pagination helper, preserving page bounds, unknown-conversation checks and cursor errors. Raw storage remains authoritative for direct storage callers. No new Session list API or unused cache helper was added.

`session_legacy_image_main_test.go` fixes twelve failing baseline leaves across memory, journal and SQLite: entries, typed entries, definitions and subscription baselines previously exposed stored-but-unpublished records. A document-state regression retains the earlier published value and rejects aliases; restoring the old guard through a disposable overlay fails all three backends. Paging, cursor/limit rejection, cancelled admission, adapter line ownership, post-Open adapter data and nonfatal request errors are covered.

Fresh full root normal tests and vet pass with CGO disabled/enabled; module verification passes. Focused legacy/public/internal image, document-state, SQLite adoption and fatal-read tests pass three race repetitions with shuffle 7/42. Full `go test -race -json ./durable -count=1 -timeout=1200s` passes with CGO enabled, Go 1.27.3 and GOMAXPROCS=4: package elapsed 631.158 seconds, 3,980 completed named test/subtest events, zero failures or unfinished tests. This clears the outstanding development durable race gate at this workload/revision. Earlier 180/420-second timeout causes have not been established. Raw JSON events, exit marker and mutation overlays were deleted after analysis. Acceptance profiling and full semantic convergence are unfinished.

### Serial Session receipt integration

`Session.commit` now calls `settleBatch`, which submits one batch through `commitParts`. Its synchronous receipt captures the exact validated native candidate while the core still owns admission. The receipt's index, count and result sequence are checked; refusal errors retain their existing projection. No raw core image is recaptured after the writer returns.

`adoptReceipt` checks header/sequence continuity on the Session line. Serial commits pass the captured trusted candidate and adopt it without decoding, validation replay or cloning. A future earlier group part can omit the candidate and reconstruct from its frame. Passing a final candidate for an earlier receipt rejects; duplicate/gapped adoption leaves the published image unchanged. Existing production adoption errors still fail/close the Session through `backendResult`.

`session_receipt_main_test.go` captures two serial receipts before either publishes, then verifies exact first-prefix visibility, rejection of the future/duplicate candidate, trusted-map reuse and a following normal commit at seq 3. The group-prefix reference test now uses `Session.adoptReceipt` without a trusted candidate. Capturing the old published image instead of the validated storage candidate in an overlay fails all three backend leaves; the overlay is deleted after analysis. The malformed-receipt suite also rejects negative indices.

Fresh full root normal tests/vet pass with CGO disabled/enabled; module verification passes. All group/frame/receipt/lifetime/crash tests plus Session image/failure/wrapper controls pass three race repetitions with shuffle 7/42. The earlier full durable race run predates this increment. Single-batch public API, native append hooks, wrapped read contracts, ownership, cancellation and observer/publication sequencing are unchanged. No performance gain is claimed; the serial candidate path avoids introducing frame-replay work.

### Prepared publication boundary

`Session.commit` now calls `prepareSessionPublication`, `settleBatch` and `publishSessionPublication` in order under its existing line. Callback execution, sealing and task/document finalisation still precede observer preparation. The prepared value holds the sealed transaction, original image and detached watch/subscription work; it creates no concurrent queue or array of prefix images.

The shared publisher rechecks the first failure, adopts the receipt image, then preserves the existing order: context invalidation/retention; task-adopt state; telemetry/ancestry/trace capture; task effects; watch/subscription enqueue; changed-definition cache invalidation; document sweep/timer scheduling. Caller rollback, observer-ready release, stop dispatch and telemetry delivery stay in the outer commit defer after line release. Cancellation after admitted storage does not discard durable publication.

`session_publication_failure_main_test.go` adds nine CGO/six non-CGO leaves. Failure injected during frame settlement fences the normal commit; after joined failure-driven Close, reopen retains its durable entry. Staged checks inject failure between settlement and publication and require an unchanged published image. Another staged check cancels the context after settlement, publishes once and rejects duplicate publication without adding an observer frame. Removing the publisher's failure recheck fails all three backend cases; the overlay is deleted after analysis.

Fresh full root normal/vet pass with CGO disabled/enabled; module verification passes. Session/image/receipt/failure/wrapper and storage-group/frame/crash/lifetime checks pass three race repetitions with shuffle 7/42. The earlier full durable race run predates this increment. No public API, dependency, storage format, accepted asset or release ref changes; no performance gain is claimed.

### Remaining boundary

Wrapped storage still uses its existing guarded paths, including authoritative adapter data, errors and post-Open replacement. The adapter regression deliberately preserves its raw seq/data contract even when the native publication image is older. Safe wrapper projection is unresolved, so Session overlap remains disabled. The core-admission gate still blocks reads behind an active backend call; this increment does not provide disjoint preparation.

[Frame-bearing receipt reconstruction](durable-group-settlement.md) now supplies a one-prefix-at-a-time correctness baseline, without retaining all full candidates; it repeats strict validation and has no performance claim. Serial Session commits now use trusted-candidate receipts. Before enabling overlap, finish wrapped read projection, Session group receipt integration, keyed/global dependencies, health/failure gates and ordered adoption/publication. A future publisher cannot capture the final raw group image for each part. Keep the [continuation contract](durable-group-contract.md), deferred persistence, resource bounds and close/join requirements intact. Full semantic convergence, acceptance and CPU/alloc_space/alloc_objects profiling remain unfinished.
