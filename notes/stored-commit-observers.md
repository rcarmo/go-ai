# Public stored-commit observers

Target: pi-durable v1.5.0, f9a26365568127461c47a07a37dcb42c140a3ac5. This advances D16 and its D04 prerequisite; it is not full closure of either.

## Public contract and ownership

Session and Harness now expose ObserveCommits, CommitObserver and CommitObservation.Detach. CommitPublication has the canonical seq/changes shape; Changes is a sealed readonly array. The prior native tables/documents envelope is now private sessionPublication. Existing private/subscription tests were mechanically renamed without changing their assertions.

Canonical changes use the actual public table codecs for conversation, entry, TaskRecord and SubmissionRecord, not serialized native envelopes. Document records carry id/kind/scope/lifecycle/policy/key metadata, not stored value/version/delta counters. Ordinary document changes carry their exact prepared readonly revision and operations. Retirements omit value/ops/version. Definition-free copies carry source.id and scalar source.at (sequence or current), not the private DocumentPoint struct. Array and record nodes are immutable; document-value nodes are grafted without detaching the adopted tracker.

Projection is prepared before storage and stamped with receipt-time lifecycle sequences. An unrepresentable private legacy record ends a guard with listener_error; it is not mislabeled as canonical, and cannot undo durable storage. This limitation is not proof of compatibility for those legacy inputs or unresolved builtin task projections.

## Actual host-dispatch phase

Publication records whether any public hosts are interested before own listeners run. It queues only then. The later dispatch snapshots the current observer registry and each guard rechecks detachment. Own listeners or storage scope cleanup may add/detach observers in between; a listener added after a publication with no hosts does not retroactively receive it.

Writer publication and host dispatch are distinct phases. The writer waits for its storage publication scope, dispatches hosts, then settles its outcome or advances to another part. A pending dispatch gate holds ordered Session continuations while releasing only the Go memory lock to the dispatcher. It does not borrow the private synchronous stored lane. Maintenance/unload and close also dispatch their stored changes; close endings follow the last held publication.

Go async storage scopes now return a completion channel for the end of the host's publication turn (including synchronous cleanup), rather than a void callback with no observable completion. Nil denotes a synchronous scope. This explicit Go host boundary represents the intrinsic JS microtask boundary; it is the sole StoragePublicationScope contract, not an alternate weaker observer API. A deferred host must close it after its turn finishes. Callback completion is still separately awaited and guarded once. Plain Go caller-continuation turns BETWEEN grouped parts remain D04 work; no-op hop was not declared equivalent to a JS macrotask.

## Test-first receipts

- The first native public API regression failed because ObserveCommits was absent; reflection allowed that absence assertion to run, and was removed once the method existed. Subsequent tests call the real public API.
- Independent reference probes establish dispatch-time membership after own listeners, sync/deferred scope cleanup, and the no-host/no-queue distinction. The deferred completion-boundary native test failed before installing the async scope-turn contract.
- Source session-observers:389/403/414/427 lifecycle/guard controls run through public Session commits: stored calls, unique failed-with-session-closed ending, exact first backend failure, idempotent detach/no ending, listener failure/end/report isolation and healthy peers.
- Exact canonical four-table payloads use relative allocated IDs and literal source fields. Fixture-only scalar conversion is on expected JSON, never native-envelope normalization. An initial native task fixture incorrectly used phase work instead of the source phase run; that setup mismatch was corrected, not credited as an implementation failure.
- Source probing caught a mistaken expectation that copy source.at was an object: it is a scalar. The native copy projection then genuinely failed on its private struct; it now emits the source shape. Copy and retirement metadata regressions pass.
- Deferred values publish only when stored: ordinary commits that read the held document AND append an entry, unload, and close. Values retain their adopted identity and ops, and close observes stored then end. A read-only no-write commit does not flush the hold; the original probe's opposite expectation failed against upstream and was corrected. Native close initially omitted its public publication; it failed before the close-flush queue fix.
- Public Harness controls and independent source probes establish terminal stored publication before task-waiter continuation and stored abort request before invocation abort signal. Native held/waiter/abort cases pass50 shuffled repeats, seed42.

Reference runner:145 unchanged pinned tests +24 probes pass, with source/test hashes verified. Final full durable413.879s/tools18.947s and repository-wide vet/Staticcheck pass, including the waiter/abort and held-close controls. Broader observer/publication/deferred controls pass three shuffled repeats; scope/public observer controls pass20–30 repeats. Final follow-up verification is recorded in the conversation/current Plan.

## Still open

D04 grouped caller-continuation/macrotask bridge, complete D16 assertion crosswalk, asynchronous rejected-promise/language binding behaviour, arbitrary observer rejection/panic values, reentry/guard-report faults, full weak/GC/cache-pressure/lifetime coverage and canonical acceptance for remaining builtin task projections. Record-field presence involving JavaScript undefined and key-order edge semantics still need explicit crosswalk disposition, not a broad Go-adaptation waiver. No full upstream parity or release-profile acceptance claim.

## Register correction

Removed unrelated cold-builtin evidence/path from D08 (requestId presence). Builtin hydration evidence belongs to D14/D17 and its own receipt. D08's original fixed request-ID evidence is preserved.
