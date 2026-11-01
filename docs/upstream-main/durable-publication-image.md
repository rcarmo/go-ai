## Internal Session publication image

[Definition wrapper authority](definition-wrapper-authority.md) closes a cache-projection hole: successful adapter Snapshot data can no longer be replaced by a native token/incarnation cache hit. Adapter definitions are rederived per read; native migration caching remains intact. Broader wrapper/pipeline dependency integration stays open.

Native internal task/context reads now have an immutable Session-owned publication image, separate from the backend's latest image. This is groundwork for the group pipeline, not an enabled asynchronous publisher or a completed source test family.

The source-derived regression settles two batches through the real private group primitive without publishing either through Session. Raw storage contains seq 2/two entries. The old internal Session read incorrectly returned those entries on memory, journal and SQLite; all three baseline leaves fail. The new read stays at seq 0. The fixture uses the private owned writer deliberately; external callers cannot bypass Session ownership this way.

### Implementation

- Open captures the native immutable image after ownership/limits checks without adding a wrapper Snapshot call.
- Exact memory/journal/SQLite internal reads use the published image while retaining native health admission, context cancellation, closing/poison checks and backend failure classification. Only allocator high-water is refreshed from a separately synchronised scalar: reservations are global and independent of publication.
- Context busy checks use published tasks/documents/conversations, not a later raw batch. This retains the shared idle predicate and its ownership/background/host/failed-input semantics.
- The current single-batch commit adopts its validated native image at the publication boundary. `adoptReadImage` requires the next contiguous receipt and the same image seq. It rejects final-group images passed for an earlier receipt, skipped parts and duplicates. It neither clones maps nor replays full state validation; its input is a trusted immutable native candidate.
- A production adoption mismatch after storage success fails the Session and closes it, instead of publishing an inconsistent view. Close drops the retained image. The existing close-only requeue writes intentionally bypass publication after hosts/observers have stopped; reopen reads their actual durable seq.

### Checks

`session_read_image_main_test.go` and `session_read_image_adopt_main_test.go` have twelve substantive CGO leaves/eight without CGO:

- Stored but unpublished group entries stay out of internal reads.
- Only the first receipted candidate becomes visible while the raw image already has the second; wrong image seq, gaps and duplicate adoption reject without advancing the image. After the second adoption, allocator and normal legacy commits continue at seq 3.
- Attempting a legacy commit past an unpublished prefix fails closed, fences further reads and joins failure-driven Close. The later global baseline check below rejects that stale append before storage; adoption still rejects gaps independently.
- An unpublished raw task does not make a published context busy.

Go 1.27.3/GOMAXPROCS=4, owned empty homes and project scratch: full root normal tests and vet pass in both CGO modes; durable elapsed 116.305/111.356 seconds. These are different workload variants, not a performance comparison. All new leaves plus context/SQLite Snapshot/Session gate controls pass three race repetitions with shuffle 7; the earlier image/adoption set also passes seed 42. Non-CGO focused checks pass three repetitions with seeds 7/42. Module verification and dependency/asset tripwires pass. Scoped staticcheck 0.8.2 reports only the existing usage_json.go:11 U1000 finding; the wider gate is open. Baseline/verification logs and fixtures are disposed after analysis.

### Public Snapshot and ordered scans

Exact native `Session.Snapshot`, `ScanConversations`, `ScanEntries`, `ScanTasks` and `ScanSubmissions` now read the published image under the Session line and native health/admission gate. Snapshot clones the image; scans use the existing filtering, cursor and detached-page helpers. Allocator high-water remains independent of record publication.

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

### Conservative preparation baseline

`settlePreparedPublication` now rejects a preparation whose base seq differs from the published image. `commitPartsAt` independently checks that seq against raw storage after core admission/ownership checks and before preparation or persistence. Raw Apply/group callers retain their existing current-state semantics through a nil expected seq. The Session serial writer supplies its preparation seq. ID reservations remain independent of commit seq.

The initial dependency audit found whole-state reads in `Tx.finalizeTaskWrites`: candidate tasks/new-work owners are checked and active documents of terminal task owners are retired. `Tx.current`, `currentDocument` and definition preparation also have direct immutable-image reads; point-getter instrumentation alone cannot describe this work. The baseline is deliberately global. It provides no keyed footprint or disjoint preparation, and does not retry callbacks. An internal stale preparation reaches the existing fatal Session boundary in production.

`session_preparation_baseline_main_test.go` prepares two sealed jobs at seq 0. After the first is stored, with or without publication, the second rejects without a second append/receipt or recovered write. Six CGO/four non-CGO leaves cover both baselines. Removing the raw writer check fails all three stored-unpublished backend cases; the overlay is deleted after analysis. The earlier gap/failure/Close test now also reopens and verifies only the original two stored parts: stale legacy work is rejected before persistence, while the adoption gap fence remains intact.

Fresh full root normal/vet pass in both CGO modes; module verification passes. The first combined focused race run exposed a pre-existing fixture race in `TestSession120HarnessFatalCauseReportedOnceOutsideLine`: unsynchronised replacement of `Session.store` raced with the running wake publisher, including publication of wrapper fields. The fixture now installs its fully initialised adapter on the Session line. It passes thirty race repetitions per shuffle seed 7/42, and the expanded Session/preparation/group/frame/crash/lifetime suite passes three race repetitions per seed. A passing retry preceded diagnosis; it did not clear the race by itself. Diagnostic JSON events/markers and mutation overlays were deleted after analysis.

No API, dependency, storage format, asset or release-ref changes. The earlier full durable race run predates this increment. Keyed dependency tracking, wrapper projection and independent writer-health admission remain unfinished.

### Native read-health admission

Exact native Session image reads now use `storeCore.sessionReadAdmission` instead of joining the storage writer line. The short ownership mutex protects the closing/poison checks and reservation scalar; no backend work runs under it. Raw storage reads and wrapped Snapshot calls retain writer admission. Session reads still own the Session line, and Session commits remain serial.

`claim` seeds the scalar from recovered/imported state. Successful reservation settlement updates it; reads never consult the concurrently changing raw Snapshot. Append settlement and journal/SQLite reclaim failures use the same synchronised poison setter. Existing first-cause classification and failure-driven Close remain in place. The lock order stays storage line then ownership mutex; Close releases the ownership mutex before waiting for the storage line.

`session_health_main_test.go` adds twelve CGO/eight non-CGO leaves. Six initial native leaves failed because Snapshot timed out behind the held append. Reads now return the old image while a private writer is held; settled-but-unpublished data stays hidden until receipt adoption. The tests also cover cancellation, independent reservation high-water, uncertain settlement fencing, and a real Close seal that waits for admitted settlement. Wrapped reads still call the adapter, cancel at its native admission gate and preserve its raw data afterwards. These fixtures deliberately use the private owned writer; they do not enable concurrent Session commits.

Removing either the poison or Close check fails its three backend cases. An initial overlay probe used Bun's canonical working-directory alias and did not replace Go's logical `/workspace` source path; it passed without exercising the mutation. Including the actual `go list` package path makes both mutations fail as required. Probe copies, overlays and logs were deleted after analysis.

Go 1.27.3/GOMAXPROCS=4: fresh root normal/vet/staticcheck v0.8.2 pass with CGO disabled/enabled; module verification passes. The new tests pass ten normal repetitions in each mode with shuffle 42; affected admission/Session failure/SQLite/raw-group controls pass five race repetitions with shuffle 42. Earlier versions of the new tests also passed focused race stress. The final full CGO durable race suite passes in 615.222 seconds (`-race -json -count=1 -timeout=1200s`), with 4,067 completed named test/subtest pass events, zero failures and zero unfinished tests. This replaces the older full-race checkpoint for the current tree. Different workloads prevent a performance comparison. Raw JSON events and empty test homes were removed immediately after analysis. No acceptance profiling, API/dependency/format/asset/release changes or overlap enablement.

### Remaining boundary

Wrapped storage still uses its existing guarded paths, including authoritative adapter data, errors and post-Open replacement. The adapter regression deliberately preserves its raw seq/data contract even when the native publication image is older. Safe wrapper projection is unresolved, so Session overlap remains disabled. Native read-health admission no longer queues behind core backend work, but callers still need the Session line. Disjoint preparation remains unimplemented.

[Frame-bearing receipt reconstruction](durable-group-settlement.md) now supplies a one-prefix-at-a-time correctness baseline, without retaining all full candidates; it repeats strict validation and has no performance claim. Serial Session commits now use trusted-candidate receipts. Before enabling overlap, finish wrapped read projection, Session group receipt integration, keyed/global dependencies, group writer-health/failure propagation and ordered adoption/publication. A future publisher cannot capture the final raw group image for each part. Keep the [continuation contract](durable-group-contract.md), deferred persistence, resource bounds and close/join requirements intact. Full semantic convergence, acceptance and CPU/alloc_space/alloc_objects profiling remain unfinished.
