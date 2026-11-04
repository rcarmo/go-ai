# Conversation view document identity

Pinned `harness-view.test.ts:330` requires two simultaneous observers to share the same `docs` object. Native watches expose equal contents through different maps. Row 314 stays `partial`.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Retained oracle

`durable/testdata/upstream-main/pending/harness_view_shared_docs_test.go` preserves the source's `a.value.docs === b.value.docs` assertion through the public native accessor, `ConversationWatch.Value()`. It compares live map identities with `reflect.Value.UnsafePointer`; content equality is checked separately. No returned value is mutated to establish identity.

The fixture fails because `Value()` deep-copies `w.current`, including its documents. This is a public accessor mismatch. It does not establish whether native could share an internal mount. The fixture continues after the identity failure and verifies the rest of source330: one listener fails with the original error, its peer receives the `pi.live` change to `x`, and the peer remains open.

Native's existing `TestConversationWatchTranscriptDeltasDetachedAndOffLine` requires detached returned values and permits Session re-entry from asynchronous callbacks. Returning shared mutable document maps would affect that ownership contract. This pass makes no runtime or API change.

## Related source assertions

- Source160 requires unchanged-part reference reuse across revisions. Equal-content replay controls do not establish it; no new oracle was run for that assertion.
- Source274 requires independent watches and a rebuilt mount with a different document object after the last observer detaches. Existing `TestMainViewUpstreamMountOracles/independent-remount` checks delivery and content reconstruction; internal mount teardown/recreation identity is unverified.
- Source525 requires independent windows sharing one mount's document object. The source330 public-identity mismatch is relevant, but that window-shaped identity assertion was not run here.
- Source570's existing Go lifetime adaptation checks backing arrays, not JavaScript individual EntryRecord identities. It was not rerun in this pass.

## Focused results

At parent `1d88f15` with the pending fixture, pure Go/modernc SQLite:

- Initial pending oracle: one failure, 0.058s. Only the shared-document identity assertion failed.
- After adding an independent content-equality precondition: count 3, three expected failures, 0.074s. Listener isolation still completes in each run.
- Five existing functions, count 3/shuffle 42: 15 passes, 0 failures, 0 skips, 1.790s. These cover mount/retirement/remount delivery, listener isolation, atomic publication, queued acquisition cancellation/close, and detached ownership/off-line callback behaviour.
- `go vet` with the pending oracle compiled: pass. Formatting and `git diff --check`: pass.

The compiler overlay excludes only the unrelated untracked continuation prototype and adds the pending fixture. Run data is deleted after analysis. No full suite, profiling, hosted CI, release or push was run.

The pending README contains the overlay recipe. The shared-identity assertion remains failing until its ownership contract is reconciled.
