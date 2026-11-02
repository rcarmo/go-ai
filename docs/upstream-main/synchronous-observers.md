# Synchronous observers

Private standalone Sessions now have a synchronous **stored-commit** lane. Runtime `3bb8baef0cb9af092e88775bc70f77f45d433038`, based on `40776312273db1100974f453a5e209aaca2f5531`, delivers stored publications after receipt adoption, including the held batch at Close, then ends attached observers. [Private Session-local listeners](session-local-observers.md) now receive memory/own stored changes in runtime `148c77b`; synchronous document observers and public/Harness wiring are incomplete.

The translated source is pinned to `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. Upstream main was subsequently observed at `3e0eec29f50e7861e9db91f2d12723267411b4f3`; exact Git comparison finds no durable source/test changes between those pins. Its AI changes and release metadata require a separate review.

## Runtime boundary

`observeStoredCommits` is private and rejects Harness Sessions. Callbacks run under the existing serial Session line and must not block, call Session APIs or mutate the read-only publication. Source imposes the same synchronous observer restriction (`types.ts:1097-1124`, `session.ts:893-919`). This trusted internal boundary adds no public mutable-map API. Existing asynchronous watches/subscriptions and their reentrant callbacks/reporters retain their off-line contract; Close neither moves nor joins those callbacks.

Preparation owns the actual stored publication independently of listener membership. Ordinary commits, dependent held writes, timer/unload/reload stores and healthy Close notify the stored lane after adoption. Memory-only commits do not notify it. Membership is captured at publication in insertion order; each captured guard checks attachment at callback admission. Detach is idempotent, suppresses later ending and releases callback/report/Session fields. A callback already admitted on another goroutine is in flight; detach does not join caller-owned work.

A guarded advance error is reported before detach and `listener_error` ending. Detachment during advance/report suppresses that ending. Guarded errors leave the Session and other listeners healthy; ending and fast-report-hook faults are contained. Normal Close ends attached observers once with `failed`/`session closed`; failure/Close refusal preserves the original cause object. Go `WatchEnd.Reason` adapts source `WatchEnd.kind`.

The private report hook adapts the source test's `RecordingSession.report`: it only records the error/context and `ReportInfo{Type:"listener_error"}`. It has the same nonblocking/no-Session-calls restriction as the private callbacks. It does not route through native generic/Harness reporting. Public reporting reentry and scheduler waiter ordering need their own integration controls before that boundary can be exposed.

## Source assertions and native controls

| Source at c9 | Native function | Scope/adaptation |
| --- | --- | --- |
| `session-observers.test.ts:389` | `TestMainSyncCommitObserverCloseAfterLastPublication` | Two stored advances, then one normal-close failure ending. |
| `:403` | `TestMainSyncCommitObserverFailureCause` | Real controlled append refusal; original error identity/no advance. Native confirmed refusal is `StorageRejected{Reason:"disk gone"}`. Raw append I/O failures retain native sanitised-uncertainty semantics. |
| `:414` | `TestMainSyncCommitObserverDetachNoEnd` | Real first callback, detach twice, no later advance/end. The callback-receipt read adapts source `flush()`. |
| `:427` | `TestMainSyncCommitObserverGuardedListenerError` | Panic with the original error, one classified report/ending, healthy peer and later commits; private recording hook as above. |
| `session-deferred-commits.test.ts:521-562` | `TestMainSessionDeferredStoredObserverOracle` | Entire `a/b/c/d/e/f` progression and storage/replica/count/type assertions; only registration adapter changed to the actual synchronous lane. |

The first four live in `durable/session_sync_observers_main_test.go`. `durable/session_sync_observers_controls_test.go` adds source-derived insertion order/detaching a later captured guard, self-detach/report ordering, contained report/end panics, attachment while real storage is blocked, and new-lane Close-refusal ending. `durable/session_sync_observers_lifetime_test.go` directly checks spent guard fields/source-list release and shared native observer-process bounds. These native invariants do not port source public-value WeakRef/identity assertions. The stored oracle now lives in ordinary discovery at `durable/session_deferred_observers_main_test.go`.

Against unchanged `4077631` plus a thin real legacy-subscription API adapter, close/guard assertions fail three backends each; detach already passes. The final typed-refusal failure fixture fails all three only for its missing ending. A raw-I/O probe instead exercised native sanitised uncertainty; an intermediate Session-fence probe was replaced by the stronger real confirmed-refusal fixture. No storage error contract changed. The additional order/fault/membership controls fail 18 backend leaves on old production; the new-lane Close-refusal ending fails three more. Missing ends/reports are left missing by the legacy adapter, never fabricated.

## Stored-observer counterexample at c9f4c7b9

The historical oracle at `4077631:durable/testdata/upstream-main/pending/session_deferred_observers_test.go` preserves the complete source521 progression and assertions. Its legacy adapter waits for the first three asynchronous callbacks. On unchanged `bd2123f`, Go 1.27.3/GOMAXPROCS4 gives three publications for four stored batches: CGO1 memory/journal/SQLite 0.079s; CGO0 memory/journal 0.053s. Storage reaches `abcdef` and earlier replicas match; Close's final `["document"]` publication is missing. Those recorded baselines were not rerun.

Close previously stopped legacy subscriptions before flushing, then adopted the held batch without preparing a publication. The new stored lane stays attached through that real write/adoption/publication and ends afterwards. Legacy subscriptions still stop at their original boundary. The promoted oracle retains `abcdef`, all per-prefix value/operation comparisons, equal publication/storage counts and the four literal type arrays. Its first three receipt reads still consume actual callbacks; separate controls establish synchronous delivery. No final callback is manufactured by the adapter.

## Focused checks

Go 1.27.3, GOMAXPROCS4, prototype excluded through logical/canonical overlay keys:

* Ten new functions plus six affected existing observation/report/publication-failure regressions pass normal count3/shuffle42 in CGO1/0 (1.190/0.529s) and race count3/shuffle7 in CGO1 (3.225s).
* Final oracle review retains source403's caller-message substring and source427's complete report-info equality. Only those two strengthened functions rerun: normal3/shuffle42 CGO1/0 0.158/0.052s, race3/shuffle7 1.245s; final vet/Staticcheck pass both modes. Production runtime is unchanged from `3bb8bae`.
* Six active mutations each fail three backend leaves: omit Close publication, wrap first cause, omit detach, release before report, omit ending-fault report, and capture membership during preparation. The first Close mutation had an unused variable and failed compilation; it was corrected before runtime-failure credit.
* Durable vet and Staticcheck v0.8.2 pass both CGO modes; module verification, formatting, diff and protected-prototype content checks pass. Staticcheck uses an exact physical package copy excluding the prototype. Its command is `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./durable`; no global binary is required.

The focused selection was:

```text
^(TestMain(SyncCommitObserver|SessionDeferredStoredObserverOracle)|TestObservation(StopCancelCloseNoJoinAndListenerFailure|PublicationSubscriptionAndProcessBounds|CallbackInitiatesCommitAndStructuralFrame|NoPublicationBeforeAdoptionOrOnRejection)$|TestReport140ListenerErrorsAndPanicsKeepCommitContext$|TestMainSessionFailureDuringSettlementPreventsPublication$)
```

Completed retention/lifecycle/deadline gates and the recorded generation baseline were not rerun. No upstream TypeScript execution, full suite, profiling, dependency/coalescer change, CI dispatch or release action. Raw source, mutation/overlay/package copies, logs and completed build cache are disposable after analysis; installed dependencies/toolchains and native v1.4.0 are preserved.

## Next source boundary

[Source564 memory/stored kinds and own operations](session-local-observers.md) now pass through the private local lane, with separate fatal-error handling and no duplicate timer delivery. Conversation-view memory updates and public/Harness ordering/report integration need controls.

Document observers (`observeDoc`), queued/cancelled attachment, address-following retirement/recreation and owner endings still lack a synchronous native API. Rejected JS thenables, arbitrary non-error thrown values, public immutable/shared values, source tracker counts/WeakRef lifetime, retained contexts and acquisition diagnostics are unported. Typed Go error returns currently adapt synchronous throws only. Fractional/extreme windows, duration ceilings and full-width clocks precede public persistence/Harness generation wiring. Full convergence is unverified.
