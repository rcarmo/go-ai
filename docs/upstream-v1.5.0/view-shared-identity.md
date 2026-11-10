# Conversation view document identity

## Public readonly watch migration

The three source160/330/525 identity oracles now pass through `ConversationWatch.Value()` and callbacks. `ConversationView` exposes `Entries` and `Docs` as sealed, comparable `ReadonlyValue` handles. Unchanged nodes retain identity; callbacks and initial access use the same readonly representation. `Copy(limits)` explicitly returns detached data. Legacy content/operation tests request that conversion through test helpers; the identity tests use the public readonly values directly.

New controls verify empty-array identity, retained entry nodes through trimming, late attachment to adopted docs, peer stop/failure isolation, equal-content/new-identity remounts and protection against writable pointer/container authority. The source570-shaped SQLite test now tracks an individual readonly entry node, alongside the earlier backing-array controls. It keeps a positive retention control through three GCs, releases it, then verifies collection within three further GCs.

A public wire regression first failed because views exposed native entry envelopes. View values and append operations now project public `EntryRecord` fields (`conversationId`, without native `conversation`/`seq`/`position`). Storage envelopes retain their existing codecs.

Verification: eight readonly-watch functions pass five shuffled repetitions (seed42); final full durable713.829s/tools22.450s; repository-wide vet/Staticcheck, logging and diff checks pass. The preceding full run passed376.429s/15.796s. No performance conclusion is established from this timing variance; notes/shared-readonly-views.md records the subsequent disk-pressure observation and project-cache cleanup.

D14/D15 stay open for the complete wrapper/lifetime/API assertion crosswalk. Public synchronous `Harness.ObserveConversation` is now implemented on the same mounts, including literal source525 windows, acquisition-only cancellation, guard fault isolation/reporting, failed-with-session-closed endings and stable mount/observer registration order. Synchronous controls pass ten shuffled repetitions. Final combined full run after the reproduced deferred-maintenance race fix: durable384.947s/tools18.434s. The broader stored/document observer work remains separate. Row314 remains `partial`. This is development verification; acceptance profiling and release gates are outstanding.

## Historical failing baselines

The records below describe the former mutable-map API and are superseded by the public migration above.

Pinned `harness-view.test.ts:330` requires two simultaneous observers to share the same `docs` object. Native watches expose equal contents through different maps. Row 314 stays `partial`.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Retained oracle

`durable/testdata/upstream-main/pending/harness_view_shared_docs_test.go` preserves the source's `a.value.docs === b.value.docs` assertion through the public native accessor, `ConversationWatch.Value()`. It compares live map identities with `reflect.Value.UnsafePointer`; content equality is checked separately. No returned value is mutated to establish identity.

The fixture fails because `Value()` deep-copies `w.current`, including its documents. This is a public accessor mismatch. It does not establish whether native could share an internal mount. The fixture continues after the identity failure and verifies the rest of source330: one listener fails with the original error, its peer receives the `pi.live` change to `x`, and the peer remains open.

Native's existing `TestConversationWatchTranscriptDeltasDetachedAndOffLine` requires detached returned values and permits Session re-entry from asynchronous callbacks. Returning shared mutable document maps would affect that ownership contract. This pass makes no runtime or API change.

## Related source assertions

- Source160: `TestPendingViewSource160UnchangedMapIdentity` checks two touching frames, the exact thinking-level delta, unchanged contents, and both map identities. The content/count/operation checks pass; unchanged `pi.live` identity and unchanged `docs` identity fail. The source's initial empty entries-array object identity is unexecuted: Go slice headers/backing pointers cannot establish that JavaScript object-identity assertion.
- Source274 requires independent watches and a rebuilt mount with a different document object after the last observer detaches. Existing `TestMainViewUpstreamMountOracles/independent-remount` checks delivery and content reconstruction; internal mount teardown/recreation identity is unverified.
- Source525: `TestMainViewSource525IndependentWindowContents` passes with the source's six notes, windows `{min:2,max:3}` and `{min:5}`, and one appended note. `TestPendingViewSource525WindowDocsIdentity` separately fails the same-revision shared-`docs` identity assertion in that exact window shape.
- Source570's existing Go lifetime adaptation checks backing arrays, not JavaScript individual EntryRecord identities. It was not rerun in this pass.

## Focused results

At parent `1d88f15` with the pending fixture, pure Go/modernc SQLite:

- Initial pending oracle: one failure, 0.058s. Only the shared-document identity assertion failed.
- After adding an independent content-equality precondition: count 3, three expected failures, 0.074s. Listener isolation still completes in each run.
- Five existing functions, count 3/shuffle 42: 15 passes, 0 failures, 0 skips, 1.790s. These cover mount/retirement/remount delivery, listener isolation, atomic publication, queued acquisition cancellation/close, and detached ownership/off-line callback behaviour.
- `go vet` with the pending oracle compiled: pass. Formatting and `git diff --check`: pass.

The compiler overlay excludes only the unrelated untracked continuation prototype and adds the pending fixture. Run data is deleted after analysis. No full suite, profiling, hosted CI, release or push was run.

## Source160 and source525 follow-up

At parent `c422344`, the two new pending functions each fail on every run: count 3/shuffle 42, six expected failures in 0.258s. Source160 reports two identity failures per run; source525 reports one. Five focused passing functions (including the new source525 content control), count 3/shuffle 42: 15 passes in 1.975s. Vet with the pending fixtures compiled, formatting and diff checks pass.

The first source160 setup used the bare transcript fixture, which emitted a whole-agent replacement during its first configuration normalisation. Initialising native configuration before recording, as the source's `openChat(chatSetup())` does, yields the exact source leaf delta and reaches the identity assertions. That earlier setup failure and the compile probes are uncredited.

The pending README contains the shared overlay recipe. All three identity oracles remain failing until the ownership contract is reconciled. Native callbacks and `Value()` both detach public maps; internal mount identity is still unverified. No runtime/API change, full suite, profiling, hosted CI or push occurred in this follow-up. Disposable run data was removed after analysis.
