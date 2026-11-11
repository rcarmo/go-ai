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

## Falsy callback panic values

Public stored-observer advance and ending callbacks now retain false, integer zero, the empty string and nil in the report's Failure.Value slot. A nil panic's Go runtime sentinel maps back to the nil slot. Existing error values retain identity. Source guardObserver reports the original throw, then separately normalises a listener_error ending through toError; the native ending now carries the matching Error message for these scalar values. Ending callback faults report once without escaping close.

Two independent source-derived probes pass against the pinned implementation. The first probe initially expected the raw scalar in the ending and failed upstream; its corrected assertion checks new Error(String(reason)). That expectation error is excluded from implementation-failure evidence. Native advance reports then genuinely failed by replacing false/zero/empty-string panics with a generic message. Native nil normalisation and ending reports also failed before their fixes. Both tests now use raw public callback panic(value), including panic(nil); they do not manufacture a Failure in the fixture.

Verification:145 unchanged pinned tests +26 independent probes;30 shuffled public/guard controls (seed42); full durable419.411s/tools18.755s; repository-wide vet, Staticcheck and logging gates. This is ordinary development verification. Full release profiling and acceptance have not run for this slice.

Non-scalar host-value stringification, floating-point number spelling, JavaScript undefined/symbol/bigint and asynchronous rejected-promise bindings still need source-derived coverage and implementation. The private synchronous observer helper retains its earlier panic behaviour.

## Reentrant detach and dispatch membership

A further source-derived probe and raw public native regression cover self-detach followed by an advance fault, detach from reporting before the fault ending, and peer replacement during dispatch. Both detach paths report the original Error once and suppress the detached guard's ending. Replacing a peer skips the detached peer immediately; the new guard first receives the next publication because notifyHosts snapshots membership before delivery. Close ends only the surviving guards, in registry order.

These controls already passed the implementation at2e60d07; no runtime fix was needed. The public callback comment now permits observer attachment/detachment and limits the reentry prohibition to queued Session operations. Verification:145 unchanged pinned tests +27 probes and50 shuffled native repeats (seed42). The last full native/static verification remains the falsy-panic slice above; this follow-up changes tests and comments only. Faulting report callbacks, queued-operation reentry and asynchronous rejection bindings still need separate acceptance.

## Still open

D04 grouped caller-continuation/macrotask bridge, complete D16 assertion crosswalk, asynchronous rejected-promise/language binding behaviour, arbitrary observer rejection/panic values, reentry/guard-report faults, full weak/GC/cache-pressure/lifetime coverage and canonical acceptance for remaining builtin task projections. Record-field presence involving JavaScript undefined and key-order edge semantics still need explicit crosswalk disposition, not a broad Go-adaptation waiver. No full upstream parity or release-profile acceptance claim.

## Register correction

Removed unrelated cold-builtin evidence/path from D08 (requestId presence). Builtin hydration evidence belongs to D14/D17 and its own receipt. D08's original fixed request-ID evidence is preserved.
