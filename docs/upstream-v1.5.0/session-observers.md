# Session observers at v1.5.0

The private standalone stored-commit lane passes the selected pinned observer assertions. Synchronous document observation and Harness integration are incomplete. Row 338 stays `reviewed-open`.

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

The source's `observeDoc` cases require acquisition values, operation delivery and address pins under a synchronous observer guard. Native exposes asynchronous `DocumentWatch` delivery but no corresponding synchronous document-observation handle.

- Cases 77/87/105/132: absent attachment, stored and memory adoption, and retirement/recreation have related watch/local-listener controls. Their synchronous document-observer assertions were not executed here.
- Cases 162/176/187/203/215: detach, end and listener-error contracts have stored-lane analogues above. Document pin release, watch-versus-observer ending and same-commit behaviour still need their own oracle.
- Cases 241/254/296: atomic/cancelled attachment and promise rejection on invalid attachment were not executed. Go returned errors do not establish JavaScript rejection timing.
- Cases 265/309: detached-observer and retired-value collection require document-lifetime evidence. Stored-lane reference clearing does not establish those assertions.
- Case 353: rejected listener promises have no native callback equivalent. Returned Go errors are synchronous and cannot establish asynchronous rejection behaviour.
- Cases 469/496/520: retirement-time throw, self-detach and peer-detach must suppress later callbacks and recreation pins in the same commit. Stored-lane peer-detach controls do not establish document pin/incarnation behaviour. These assertions remain unexecuted.

These are fixture/API gaps, not passing source tests. No runtime contract or API changed in this pass.

## Focused verification

At parent `3c142ab` plus the two new controls, pure Go/modernc SQLite:

- Two new controls: 2 passes, 0.040s, count 1.
- `TestMainSyncCommitObserver*` plus `TestMainSessionDeferredStoredObserverOracle`: 12 functions × count 3, shuffle 42; 36 passes, 0 failures, 0 skips, 0.823s.
- `go vet` with the protected prototype excluded through the compiler overlay: pass.
- Formatting and `git diff --check`: pass.
- Two compiler-overlay faults were rejected: suppressing stored observer advances fails the commit-return visibility control; sharing one close-error object across Sessions fails the close-error identity control. Runtime source was unchanged.

The first command failed in the Makefile shell because an unquoted regex end anchor was consumed by Make. No tests ran in that attempt. The corrected focused commands above supplied the credited results.

The overlay changed compiler input only for the unrelated untracked continuation prototype. Disposable run data was deleted after analysis. No full suite, profiling, hosted CI, release or push was run.
