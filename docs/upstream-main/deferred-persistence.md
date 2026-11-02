# Deferred persistence: source contracts and existing native proof

The generation view failure requires a deferred-document facility in Session. Existing native storage, receipt and publication tests remain regression requirements. There is no new passing source/native result in this review.

Target: `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`. The source files contain 25 `it` declarations in `session-deferred-commits.test.ts` and 11 in `harness-deferred-progress.test.ts`; these are declaration counts, not execution results. The recorded [generation oracle](generation-view-oracle.md) already fails nine backend leaves with four frames/four stored touches. Do not rerun that baseline merely to rediscover it. Tracked native `durable/` files are unchanged between its `18aa8c3` checkpoint and `a1983c6`; later root validation changes retain their own scope.

## Session cases

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
