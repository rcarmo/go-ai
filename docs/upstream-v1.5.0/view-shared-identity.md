# Conversation view document identity

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
