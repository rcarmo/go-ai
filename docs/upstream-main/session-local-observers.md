# Session-local commit listeners

Private standalone Sessions now publish every memory adoption and each storing transaction's own changes to local listeners. Runtime `148c77b08c1d84a9cfca278380b3b872544a7ffe`, base/rollback `de8a7b2f538e8736a08e634be9620d5e5cc9de32`, separates these events from the stored-observer lane's folded durable writes. Timer/maintenance/Close storage of an already-observed hold produces no second local event.

Source is pinned to `3e0eec29f50e7861e9db91f2d12723267411b4f3`: `session-deferred-commits.test.ts:564-596`, `session/session.ts` (`ObservedCommit`, `followCommits`, `#hold`, `#publish`) and the listener-failure arm of `session/writer.ts`. These durable trees are unchanged from c9. No upstream TypeScript execution occurred in this increment.

## Publication and failure

`preparedSessionPublication.observed` retains the sealed transaction's own publication. `stored` independently includes selected held writes. Memory events contain documents only and have a nil sequence; stored events carry a sequence, own table writes and own document changes. A read-dependent entry can therefore store an old hold without re-showing that document locally. Hosts still receive the entire stored batch.

Local callbacks run synchronously after adoption, before stored host callbacks. Every member of the copied local set runs, including a later member detached by an earlier callback. The first returned error or error-valued panic is retained while peers finish. Hosts see the current stored publication before the Session failure is latched. The already-published call resolves; later admissions fail with the first cause. Failed memory holds are dropped at shutdown. Stored data survives reopening. These errors receive no guarded `listener_error` report.

The existing asynchronous watch/subscription/report callbacks keep their off-line reentry contract. They have not moved under the Session line, and Close does not join their caller-owned work.

## Private Go boundary

`followSessionCommits` rejects Harness Sessions. Callbacks must not block, mutate events, call Storage/Session/task APIs or invoke host code. Public shared-value ownership and scheduler waiter/abort/report integration have no adapter yet. Source queues host callbacks before the microtasks awakened by its own listeners; native Harness wakeup ordering needs separate controls before this lane can serve it.

An immutable `*sessionCommitListener` represents Source function identity. Adding the same object twice consumes one native process slot. Each detacher deletes that identity, so an old detacher also removes a later re-add. Like Source's returned closure, a caller-owned detacher retains its Session/listener until the caller drops it. Close clears the Session's listener set. This differs from the released fields of a spent guarded host observation. No unsafe closure comparisons or new public mutable-map API are used.

## Assertion mapping

`durable/session_local_observers_main_test.go:TestMainSessionDeferredLocalObserverOracle` retains the entire source564 progression: deferred `a/b/c`, immediate `d`, then fire; watch values `a/ab/abc/abcd`; local kinds `memory/memory/memory/stored`; and four literal own-operation arrays for `a/b/c/d`. Non-document/retired changes fail explicitly instead of being filtered from the Source assertion. Native reserved-conversation setup and actual watch/callback receipts adapt `open()`/`flush()`. Sequence absence/presence is an additional native union-shape check.

Source-derived controls in `session_local_observers_controls_test.go` check copied-set membership/function identity, local-before-host order, an actual held timer store without duplicate local delivery, first fatal error/healthy peers/current-call success/reopen state in both stored and infinite-memory modes, and registration while real storage is blocked. The admission/Close function checks shared native process capacity, duplicate identity at capacity, slot reuse, closed refusal and callback-set release. These are controls, not additional complete upstream test bodies.

`session_local_observers_mixed_test.go` covers both read-dependent and changed-dependent entries: local listeners see only own tables/documents, while hosts receive the folded stored operations.

## Baselines and focused checks

Unchanged `de8a7b2` plus a thin real `SubscribeCommits` adapter fails source564 on all three backends (0.069s): watches already pass, but only one local `stored` event with folded `abcd` appears. The adapter supplies no synthetic memory event or rewritten own operations. Source-derived set/timer/membership/fatal controls fail 15 backend leaves; the corrected fatal fixture alone confirms six failures (0.105s). Mixed-entry controls fail six leaves (0.102s).

Adapter frame/storage-method and retirement-field compile errors were corrected before credited runs. The first candidate fatal fixture incorrectly reopened an owned SQLite store and used a timer callback whose healthy-Snapshot assumption did not allow the deliberately failed Session. The final fixture uses the existing native reopen helper and an infinite memory window; it still fails unchanged production. These fixture failures required no production fix.

Go 1.27.3/GOMAXPROCS4, prototype excluded through inherited logical and canonical overlay paths:

* Six new functions plus the three affected legacy callback-reentry/report-context/settlement-failure regressions pass normal3/shuffle42 CGO1/0 (0.806/0.386s), race3/shuffle7 (2.471s). The later admission/Close function alone passes normal3 both modes (0.066/0.044s), race3 (1.081s).
* Durable vet and Staticcheck v0.8.2 pass both modes, plus module verification, formatting, diff and prototype-content checks. Staticcheck uses a physical package copy excluding the prototype.
* Seven active mutations are rejected: folded own operations (3 backend leaves), omitted memory delivery (3), duplicate timer delivery (3), swallowed fatal errors (6), skipped peers after first fault (6), guarded detachment during the current copied local iteration (3), and hosts before locals (3). An ambiguous timer-mutator anchor was corrected before execution; the initial fatal tally included the memory-mode parent and was corrected from saved logs without rerunning tests.

The main selection was:

```text
^(TestMainSession(DeferredLocalObserverOracle|Local)|TestObservationCallbackInitiatesCommitAndStructuralFrame$|TestReport140ListenerErrorsAndPanicsKeepCommitContext$|TestMainSessionFailureDuringSettlementPreventsPublication$)
```

Completed stored-observer/retention/lifecycle/deadline gates and the recorded generation baseline were not repeated. No full suite, profiling, dependency/coalescer changes, CI dispatch or release action. Raw sources, overlays/mutations, package copies, logs and completed build cache are disposed after analysis; dependencies/toolchains, native v1.4.0 and the protected prototype are preserved.

Next: conversation-view memory delivery and numerical window/clock controls. Public/Harness wiring, immutable shared values, tracker counts/public WeakRef lifetime, rejected JS thenables/non-error thrown values and full generation integration are unverified.
