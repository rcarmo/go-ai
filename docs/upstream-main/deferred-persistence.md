# Deferred persistence: source contracts and existing native proof

An internal infinite-window Session primitive now passes focused native scenarios. Public persistence settings and Harness integration are unfinished. The generation oracle still has its recorded failure; the 25 Session and eleven Harness source bodies have not been executed or completely ported.

Target: `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. The source files contain 25 `it` declarations in `session-deferred-commits.test.ts` and 11 in `harness-deferred-progress.test.ts`; these are declaration counts, not execution results. The recorded [generation oracle](generation-view-oracle.md) already fails nine backend leaves with four frames/four stored touches. Do not rerun that baseline merely to rediscover it. Tracked native `durable/` files are unchanged between its `18aa8c3` checkpoint and `a1983c6`; later root validation changes retain their own scope.

## Session cases

The missing-work column records the source review before this internal increment; the implementation and remaining limits are listed below.

| Source lines | Required behaviour | Existing native proof to preserve; missing work |
| --- | --- | --- |
| 189–283 | Hold changes only to existing conversation documents with latest history and initial-fork policy. Other writes, creation and migration store immediately. Reject read-only writes and invalid deadlines. | Document checkpoint/migration tests cover immediate storage, but no native deferred policy exists. A mixed task/document commit cannot qualify merely by removing its document from the stored batch. |
| 285–346 | Fold held changes into dependent writes and into a writing commit that reads the held document. Unrelated and write-free commits leave the hold alone. Callback/checkpoint failure before Storage preserves it. | Transaction validation and rollback remain applicable. Whole-Snapshot dependency guards are insufficient to select which held documents to flush; record actual document dependencies without replaying callbacks. |
| 347–390 | Earliest deadline flushes every hold in one transaction; default/explicit deadlines, infinity and rescheduling are distinct. | Existing cache-expiry and generation-coalescer timers perform different jobs. Add a separately owned persistence deadline; do not turn publication throttling into durability semantics. |
| 391–457 | Close drains earlier admissions then flushes; storage failure still closes. Failure does not flush. Unload, version reload and retirement flush at their specified boundaries. | Existing Close/requeue and first-failure gates protect task recovery. They do not establish held-document draining. |
| 458–520 | Preserve base selection; otherwise count the stored fold as one delta. Bound pending operations and store the final base after overflow. | Existing bounded operations, checkpoint selection and frame limits remain enforced. Do not retain an unbounded sequence of full candidate Snapshots. |
| 521–599 | Stored observers receive exactly the stored batches and no memory-only commits. Watches and Session-local observers receive each memory change once; a flush does not repeat it. | `prepareWatchFrames`, `prepareSubscriptions` and receipt adoption currently share one durable publication. Separate memory delivery from stored-observer delivery before adding holds. |
| 600–end | Pin held values despite retention zero, release old revisions after storage, and reopen at the last stored state without restoring holds. | Existing retention and crash/reopen tests protect durable state. Add hold-specific lifetime and recovery assertions. |

Source `session.snapshot(...)` can inspect the memory value without storing it. It must not become a blanket flush. Conversely, the dependent-commit test includes an actual write to another document; a write-free read leaves the hold intact.

## Harness cases

The eleven harness tests cover throttled persistence, recovery before and after the first stored partial, repeated crashes, a held task becoming sleepable, cost/progress observation while data is held, tool replay, summarization checkpoints and third-party dependent commits. Their complete bodies remain unported. Existing native partial/coalescer tests cover publication throttling, cancellation, joining an admitted write and dropping a trailing pending value. Preserve those contracts while adding separate persistence controls.

The attempt checkpoint in `generation.go` must remain durable before provider dispatch. `syncGenerationStatuses` currently writes presentation in that same transaction. Split the durable intent from the subsequent eligible presentation commit; do not defer the intent, filter the independent counter, invent a callback, or classify a mixed write as a source-eligible deferred commit.

## Native regression anchors

These existing tests retain their recorded results; none was rerun for this source review:

- `TestMainSessionSerialReceiptCapturesExactCandidate`: capture the exact immutable candidate under the storage gate.
- `TestMainSessionReadImageAdoptsOnlyReceiptedPrefix` and `TestMainSessionReadImageGapFailsClosed`: contiguous durable images; no raw later-prefix substitution.
- `TestMainSessionFailureDuringSettlementPreventsPublication` and `TestMainSessionPublicationStageFailureAndCancellation`: first-failure fencing, cancellation and stage ordering.
- `TestClose140RequeueDoesNotConsumeRestartBudget` and `TestClose140RejectedRequeueLeavesCrashRecovery`: graceful shutdown and rejected requeue recovery.
- `TestGenerationProgressTrailingCoalescenceStopDropsPendingAndJoinsWrite` and `TestGenerationProgressConsumedWakeDoesNotArmEmptyWindow`: coalescer ownership and timer admission.
- `TestGeneration104ConfiguredPartialIntervalProductionPath`: existing production-path publication intervals.

## Implementation boundary

Keep durable sequence/receipt adoption unchanged. Represent held document content and its memory-publication witness separately from the receipted image; preserve wrapper data/error authority. Track and bound held operations, selected base, dependent document reads/writes and persistence deadlines. Deliver memory and stored observations on their respective boundaries, with no duplicate memory frame at flush. Add explicit Close/failure/unload/reload/retirement handling and recovery/lifetime tests before wiring generation into the facility.

Session commits remain serial. This work does not require enabling grouped overlap or solving legacy post-return continuation ordering. Neither a fake receipt nor a scheduling yield provides deferred durability.

## Internal infinite-window increment, 2 November 2026

Runtime checkpoint: `46b37396b11f48eb97250c2696e82e3e54ecd7d8`; base/rollback: `18b8b34472e835d95178b3afe42902222d673d2f`. Later receipt edits change documentation only.

`durable/session_deferred.go` holds owned document values and folded operations separately from `readImage`. The internal entry accepts only standalone native Sessions; adapters and task-scheduled Sessions return `ErrUnsupported` before the callback. Ordinary public commits retain immediate persistence. Eligible writes affect existing conversation documents with latest history, initial-fork policy and unchanged version. Mixed writes and creation fall back to immediate storage.

Memory publications update document watches without advancing the durable sequence or notifying stored subscriptions. Writing transactions flush only held documents they access; unrelated writes, write-free reads, callback failures and cancellation preserve the hold. A dependent write combines the pending operations and its own change into one stored delta, or a selected base. Pending operations use the existing Chord-compatible folding helper; crossing the native 4,096-operation budget replaces the tail with the final base. The source's 10,000-operation boundary is still unported. Aggregate encoded flush and retained-value bounds are checked before adopting a hold.

Private transaction preparation folds held content into a provisional next candidate against real durable history. It leaves historical revisions at already stored sequences unchanged. Append-operation tests caught a materialisation/history mismatch in the first implementation; the fix retains the storage validator. Creation with an unrelated hold then exposed an `unreserved ID` rejection in stored-observer preparation. That regression failed all three backends before the preparation snapshot included the callback's minted-ID high-water, without changing the prior receipted image.

Healthy Close stores every remaining hold in one batch; a failed Session discards volatile content. Close still closes storage after flush refusal and preserves the original flush error. Reopening tests inspect the actual memory image, journal and SQLite state. Close-time observer delivery, unload/version reload/retirement, retention-zero pins, old-revision collection, finite/default deadlines, conversation-view memory delivery and synchronous local observers need source-specific implementation and tests before Harness integration. The generation attempt's durable task intent and later presentation have not yet been split.

### Focused evidence

Seven native test functions in `durable/session_deferred_main_test.go` cover memory/storage separation, append folding, dependent read/write commits, unrelated and write-free commits, failure/cancellation, caller detachment, stored-publication replay, immediate mixed writes/creation, native overflow, Close/reopen/refusal and adapter admission. They are source-derived scenarios and native controls, with zero complete upstream `it` bodies credited. Adaptations use the reserved native root, raw document handles, an internal infinite-window entry and channels to observe asynchronous watch callbacks. They add no production acknowledgements, sleeps or yields.

A compatibility control restores the four changed runtime files from `18b8b34472e835d95178b3afe42902222d673d2f` with compiler overlays and maps the new test entry to that runtime's ordinary `Commit`, its only persistence path. The three selected functions fail nine backend leaves: memory changes are already durable, Close adds no folded batch, and failed-Session recovery contains the would-be held value. This is an old-runtime API-adapter control; upstream TypeScript was not run. The separate recorded generation failure was reused without rerunning it.

Go 1.27.3, `GOMAXPROCS=4`, with the protected continuation prototype overlaid out:

```text
-run '^Test(MainSession|Session|Document|Transaction|MainWatchPending|MainTranscriptPublicationWindowPendingFold|GenerationProgress|Generation104ConfiguredPartialIntervalProductionPath|Close140)'
normal -count=3 -shuffle=42: CGO1 37.640s; CGO0 27.648s
race   -count=3 -shuffle=7:  CGO1 187.191s
```

`go vet ./durable` passes in both CGO modes. Staticcheck v0.8.2 passes in both modes using a temporary exact package copy with the protected prototype physically absent and sibling dependencies linked to the unchanged source tree. An initial compiler-overlay attempt still parsed the prototype; an explicit file-list attempt lost test helpers. Those tooling failures receive no passing credit. Module verification, gofmt and diff checks pass. The prototype stayed in place and passed content comparison. No dependency, generation/coalescer, release-ref or asset changes; no full suite, profiling, hosted dispatch or publication. Disposable copies, overlays and logs are removed after analysis.
