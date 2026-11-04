# Session observers at v1.5.0

Private stored-commit and synchronous document-observer lanes pass selected source-shaped controls. `Session`, `Harness` and `ConversationHandle` expose synchronous typed document observation with detached Go values. Full readonly identity, task-runtime attachment and Harness integration remain incomplete. Row 338 stays `reviewed-open`.

Source: `packages/durable/test/session-observers.test.ts` and `packages/durable/src/session/session.ts` at `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Stored-commit observations

| Source assertion | Native coverage | Scope |
| --- | --- | --- |
| 389: stored advances precede the single close ending | `TestMainSyncCommitObserverCloseAfterLastPublication` | Private standalone lane; the source's `observeCommits` has no public native equivalent |
| 403: failed ending retains the failure error | `TestMainSyncCommitObserverFailureCause` | Injected native `StorageRejected` identity; arbitrary backend errors may receive native classification |
| 414: idempotent detach suppresses advances and ending | `TestMainSyncCommitObserverDetachNoEnd` | Same lifecycle assertion on the private lane |
| 427: listener failure reports, detaches and ends once; peers survive | `TestMainSyncCommitObserverGuardedListenerError` | Go error/panic adaptation; no JavaScript promise rejection |
| `session.ts:900–906`: adoption precedes notification; notification precedes commit continuations | `TestMainSyncCommitObserverSeesAdoptionBeforeCommitReturns` | New source-derived control: callback sees the receipted Session image and persisted entry before `Commit` returns. Go has no JavaScript microtask queue |
| 187: separate closes use distinct errors | `TestMainSyncCommitObserverSeparateCloseErrors` | New stored-lane analogue of a document-observer assertion; two closes per Session still yield one ending per observation |

Existing controls also test registration-order delivery, detaching a later captured guard, reporter/end faults, membership selected at publication, close-flush refusal and release of callback/source references. Shared capacity is reusable after detach, listener error or close. Explicit reference clearing establishes a native retention invariant; JavaScript `WeakRef`/GC timing is untested.

Callbacks run synchronously under the Session line. They must not block or call Session APIs. Re-entry through those APIs is outside this native callback contract; no test in this pass attempts it. The new visibility control reads the private adopted image and raw storage without re-entering the Session.

`observeStoredCommits` explicitly rejects scheduler-backed Sessions. Watch delivery is asynchronous and cannot replace the synchronous document-observer interface in these comparisons.

## Synchronous document observations

`observeDocumentDefinition` is the synchronous guard behind public `ObserveDefinition` on Session, Harness and conversation handles. It registers in local listener order, pins the active incarnation, returns a detached acquisition value and delivers each prepared document change synchronously after stored or memory adoption. The exported `DocumentObservation.Value` is the attachment baseline; `Detach` is idempotent. Go mutable maps are detached copies and do not implement JavaScript readonly reference identity.

- Source77/87/105 have source-shaped standalone controls: absent attachment, committed count3 with exact set operation outside the storage call, and deferred count5 before storage with no duplicate local advance on flush. The private Go API and detached values adapt the source interface.
- Source132/162 have standalone retirement/recreation and idempotent-detach controls. Retirement clears the Session-owned followed value and pin, while the caller owns the detached initial value; recreation emits a replacement and pins its new incarnation.
- Source176/203/215 have close/failure and synchronous Go error/panic isolation controls. Normal close ends once with `failed`/`session closed`; a backend failure retains its cause. The tests do not establish JavaScript rejected-promise timing.
- Source241/254/296 have queued-baseline, cancellation before and after registration, and malformed-address controls. Go returned errors do not establish JavaScript promise-rejection timing.
- Source469/496/520 have source-shaped same-commit retirement error, self-detach and peer-detach controls. Detached guards retain no recreation pin/value; a healthy peer continues. Full public/Harness and JS callback semantics remain open.
- Source265/309: explicit guard/reference release and retired-value clearing are tested; JavaScript `WeakRef` timing and collection are not. Source353 rejected thenables remain untested.

The synchronous callback runs on the Session line. Pinned `types.ts:1094–1116` requires `advance` to be quick and forbids awaiting or calling the Session. Public Go documentation and controls must preserve that restriction; P07 ownership/identity still needs reconciliation. Task-owner termination has a scheduler-backed source-shaped control below; task-runtime-owned observer acquisition and full Harness integration remain open.

## Focused verification

At parent `3c142ab` plus the two new controls, pure Go/modernc SQLite:

- Two new controls: 2 passes, 0.040s, count 1.
- `TestMainSyncCommitObserver*` plus `TestMainSessionDeferredStoredObserverOracle`: 12 functions × count 3, shuffle 42; 36 passes, 0 failures, 0 skips, 0.823s.
- `go vet` with the protected prototype excluded through the compiler overlay: pass.
- Formatting and `git diff --check`: pass.
- Two compiler-overlay faults were rejected: suppressing stored observer advances fails the commit-return visibility control; sharing one close-error object across Sessions fails the close-error identity control. Runtime source was unchanged.

The first command failed in the Makefile shell because an unquoted regex end anchor was consumed by Make. No tests ran in that attempt. The corrected focused commands above supplied the credited results.

The overlay changed compiler input only for the unrelated untracked continuation prototype. Disposable run data was deleted after analysis. No full suite, profiling, hosted CI, release or push was run.

## Private document-observer verification

At parent `59065bc` plus the private implementation, pure Go/modernc SQLite, the focused set of 24 functions ran count 3 with shuffle seed 42: **72 top-level passes, 0 failures, 0 skips, 2.801s**. It includes 15 synchronous document-observer functions and nine related local/stored/watch/failure controls. The final source-shaped adoption test checks callback timing outside the actual storage append and confirms deferred memory delivery before storage.

Four compiler-overlay faults were rejected: suppressing advances; retaining the retired pin; skipping post-registration cancellation; and omitting task-owner ending. `go vet`, formatting and `git diff --check` passed with the protected prototype excluded through the compiler overlay. The interrupted command and earlier failed fault-script parse were not credited. Staticcheck was unavailable; race testing requires CGO and was not run under the pure-Go restriction.

At that checkpoint the implementation was private and standalone. Public observation API, JS reference identity, rejected thenables, WeakRef/GC timing and group-publication interactions remain open. Synchronous Session re-entry is forbidden by the source contract. No full suite, profiling, hosted CI, release or push ran. Disposable logs, overlays and fault copies were removed after analysis.

## Scheduler-backed document observation

The pinned implementation uses the same synchronous document observer on Session and Harness paths. The private guard supports scheduler-backed Sessions; `Harness.ObserveDefinition` now exposes it. The readonly reference-identity adaptation remains unresolved. The owner-retirement and rollback controls below now attach through the public Harness API; other scheduler controls retain private guard access.

At parent `fa31487`, the first source308-shaped oracle failed at attachment with `ErrUnsupported` (0.063s). The corrected source-shaped fixture uses `test.observers.progress`, `lines: ["started"]`, waits for the real task, and checks retirement with empty operations followed by `ended`, both after the native storage core gate is released. It then closes the Harness and checks no extra calls. Explicit native IDs and private callbacks adapt the source API. A separate derived control checks that an aborted deciding transaction neither delivers retirement nor releases its pin. Queued-attachment/cancellation and Session-failure-cause controls exercise the same private guard with a real scheduler; Go `context.Canceled` adapts source rejection timing.

Four new Harness observer functions plus 26 related focused functions passed count3/shuffle42: 90 top-level passes, 0 failures, 0 skips, 3.268s. A final count1 gate passed in 1.106s. Three compiler-overlay faults were rejected: suppressing the retirement advance, suppressing the owner end, and restoring the scheduler exclusion. Vet, formatting and diff checks passed. The earlier baseline's scalar `status` fixture was corrected to the pinned source's `lines` array before the credited post-change runs.

Callbacks remain synchronous on the Session line; they must not block or synchronously re-enter Session/Storage. JS thenables, public observer ownership, task-runtime-owned attachment and P05 multi-part group ordering remain open. No full suite, profiling, hosted CI, release or push ran; scratch was removed after analysis.

## Public typed observation slice

At parent `43eb05f`, `Session.ObserveDefinition`, `Harness.ObserveDefinition` and `ConversationHandle.ObserveDefinition` expose the existing synchronous guard. Source-shaped public controls cover absent attachment, stored and deferred advances, exact operations, listener failure, detach and Harness close. The source308 task-owner retirement and native rollback controls now attach through the public Harness surface. Harness listener failures use `OnReportWithInfo`; standalone Session callers receive the ending error. Acquisition values and callback values are detached Go maps; this does not establish source identity, JavaScript thenables or task-runtime-owned attachment.

The initial overlaid public source fixture failed to compile because the public API was absent. The final focused set of 23 functions passed count3/shuffle42: 69 top-level passes, 0 failures, 0 skips, 1.803s. Vet, formatting and diff checks passed with the protected prototype excluded by compiler overlay. Two overlay faults, removing public detach and suppressing Harness reporting, were rejected by their respective public controls. No race detector, full suite, profiling, hosted CI, release or push ran. Scratch and fault copies were removed after use.
