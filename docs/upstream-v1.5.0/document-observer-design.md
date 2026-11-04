# Synchronous document observer design

P06 needs a synchronous document-observation lane. `DocumentWatch` is asynchronous, coalesces pending frames and cannot establish the pinned `observeDoc` contract. The first implementation slice should cover source77/87/105, followed by retirement/guard/lifetime cases; this design is not an implementation or a parity claim.

Source: `packages/durable/src/session/session.ts:666–716, 1412–1527, 1770–1810` and `packages/durable/test/session-observers.test.ts` at `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native examined at `0013b9d`.

## Source contract and native boundaries

| Source behaviour | Native starting point | Gap |
| --- | --- | --- |
| Observe an absent address: return no observation and attach nothing (77) | `watchDefinitionValue` loads an existing definition/address under the Session line; `WatchDefinition` does not create it | Only asynchronous `DocumentWatch` is exposed; its capacity/lifetime does not implement synchronous observer semantics |
| Return the committed starting value; deliver each stored change and its operations after adoption (87) | `preparePublication` constructs detached `DocumentChange`; `publishLocalCommit` runs on the Session line after receipt adoption, and before stored host callbacks | Local listeners are trusted/private and explicitly reject scheduler-backed Sessions; they are not guarded document observations |
| Deliver the memory-adopted value before a deferred write is stored (105) | `holdPreparedDocuments` adopts `memoryImage`, queues watches, then synchronously calls `publishLocalCommit` with kind `memory` | An async watch callback or later stored notification cannot prove this source assertion |
| Follow one address through retirement/recreation; unpin retired incarnation; emit last value then replacement (132) | `DocumentWatch` tracks addresses and `documentPinned` scans watches | Watch preparation coalesces an address into one queued frame per publication. Source observer loops each document change and can detach between them |
| Listener error reports/detaches/ends only that observer; peer continues (215/469) | Private `sessionCommitObservation` guards stored-commit observers | Raw local-listener errors fail the Session after publication. A document-observer adapter must contain errors inside its guard and return success to the local lane |
| Self/peer detach in the retirement callback suppresses later same-commit calls/pins (496/520) | `publishLocalCommit` takes a copied listener set; raw detached listeners can still be in the current copy | Each guarded document callback must recheck attachment before each change and after each user callback, not just before iterating the copied set |

Source attachment uses `#enqueue`, checks cancellation before/after loading, and detaches if cancelled after creation. Its observer pins the incarnation, keeps the address while absent, releases retired value, and takes a new pin only when recreation remains attached. Source guard reports a synchronous throw or rejected thenable, detaches and ends once with `listener_error`; Go returned errors/panics are synchronous adaptations and do not establish JavaScript thenable behaviour.

## Candidate implementation stages

1. **Private standalone slice:** add a document-observation guard distinct from `DocumentWatch`. Acquire and register under the Session line; absent address returns no handle and allocates no observer/pin. Start from the loaded definition value; do not create or persist a missing document. Use a lifecycle handle whose detach releases callback, report, Session and pin references exactly once. Keep current process quotas/limits; account for this handle in observer capacity and retained-value budgeting rather than allocating an unbounded parallel registry.
2. **Publication:** attach to local adopted changes, not raw storage and not asynchronous watch frames. Stored delivery occurs after receipt adoption; memory delivery occurs during memory adoption before later persistence. Preserve the source's change ordering and own-operations semantics, and validate/detach callback payloads. Register before or atomically with the baseline so an intervening commit cannot be lost. Do not duplicate a local memory event when its hold later flushes to storage.
3. **Guard and end:** execute callback at the pinned synchronous boundary; catch errors/panics at this adapter, report once, detach and deliver one `listener_error` ending. A healthy peer and the commit must continue. A normal Session close gives the synchronous observer a `failed`/close error, unlike the asynchronous watch's native end tag. A Session failure carries its actual cause. Ensure no user callback is made while holding the observer registry/guard mutex; preserve registry lock ordering.
4. **Incarnation/pin:** track address, current ID, version and last value. On retirement clear the retained incarnation/value and unpin before callback; if still attached, recreation pins its new incarnation and emits a root replacement. Recheck attachment after every change, especially after user code and after another observer can detach it. Extend `documentPinned` or provide equivalent bounded pin accounting; current implementation only scans `s.watches`.
5. **Harness:** only after standalone ordering and lifecycle controls pass, integrate scheduler-owned Sessions with task-owner termination, close, pin release and publication/waiter ordering. `followSessionCommits` and `observeStoredCommits` currently return `ErrUnsupported` when `taskScheduler != nil`; simply deleting that guard does not establish safety.

Native `preparePublication` builds changes from final candidate IDs, with IDs sorted and per-document planned operations. Before relying on it for source469/496/520, verify it preserves every source-visible change and order in same-commit retire/recreate scenarios; a final-state collapse could hide intermediate observer callbacks. A separate observer guard must not cause duplicate `MaxPage` registrations or hold stale callbacks after detach/close.

## Callback and ownership decision

The source invokes its observer synchronously and can enqueue later async Session work. Native private local callbacks currently say they must not block or call Session APIs while the Session line is held. A Go callback that synchronously re-enters `Commit` would deadlock. Match the source's observable ordering, while specifying how Go callers enqueue later work without exposing an unsafe synchronous re-entry contract. Do not silently run callbacks asynchronously to avoid that design problem.

Source values are `Readonly<JsonObject>` references. Go `JSON` maps are mutable and native public watch returns are detached. P07 must decide shared identity versus safe ownership for public view/document values; the first private observer slice may use detached payloads for source77/87/105 **content** assertions, but must label identity and any public API difference as unresolved. Never share writable maps merely to mimic object identity.

## First acceptance slice

A worker may implement the private standalone guard after reviewing these boundaries. Keep source assertions in three focused tests:

- **77:** observe missing address → no handle, no callback, no cache/pin/registry mutation; an unrelated commit remains silent.
- **87:** create the source-shaped count document, attach at committed value, commit count3, observe one synchronous advance with exactly `[["s", ["count"], 3]]`; distinguish callback invocation from commit return and verify storage adoption has completed.
- **105:** prepare a document, attach, commit deferred count5 with infinite window, observe count5 before storage changes, then flush and verify no duplicate local advance. Check both memory and stored witnesses explicitly.

Also add negative controls: cancellation at queued acquisition and after attachment, idempotent detach, close/failure and listener error with healthy peer, quota exhaustion/reuse and retained-reference cleanup. Keep Go-only return-error/panic tests separate from source thenable tests. Source77/87/105 do not alone establish full observer parity.

At `0013b9d`, six focused existing functions pass count1: `TestMainWatchAddressRetirementRecreation`, `TestMainWatchAddressAbsentIsolationAndRollback`, `TestMainSessionDeferredLocalObserverOracle`, `TestMainSessionLocalListenerSetAndHostOrder`, `TestMainSessionLocalListenerFatalAfterPublication`, and `TestMainSyncCommitObserverReleaseAndSharedBounds`. They exercise native related mechanisms, not `observeDoc`.

At parent `6a11cc6`, `TestMainDocumentObserverLocalPublicationSeam` adds source-shaped **seam** controls: absent typed lookup does not create a document; a stored update synchronously publishes count3 with its exact set operation; a deferred conversation document synchronously publishes memory count5 before storage advances and its subsequent flush adds no duplicate local event. It does not implement or test synchronous document-observer attachment, pinning, error isolation or close. Six focused functions including this seam, count3/shuffle42: 18 top-level passes, 0 failures, 0 skips, 0.766s. Two compiler-overlay faults suppressing memory or stored local publication are each rejected by the corresponding seam subtest. Vet with the protected prototype overlay and `git diff --check` pass. Run scratch was removed. No runtime source, full suite, profiling, CI or external publication changed in this pass.
