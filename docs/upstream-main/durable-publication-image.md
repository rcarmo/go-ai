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

### Remaining boundary

Public Snapshot/scans and wrapped-storage projection still use their existing guarded storage paths. Wrapper Snapshot behaviour, including failures and post-Open adapter replacement, is unchanged. They must not be switched to an unguarded cache. The existing core-admission health gate also still blocks internal reads behind an active backend call; this increment does not provide disjoint preparation.

Before enabling overlap, finish the public/wrapped read projection, per-part candidate/receipt construction, keyed/global dependencies, health/failure gates and ordered adoption/publication. A future publisher cannot capture the final raw group image for each part. Keep the [continuation contract](durable-group-contract.md), deferred persistence, resource bounds and close/join requirements intact. Full race, semantic convergence, acceptance and CPU/alloc_space/alloc_objects profiling remain unfinished.
