# Deferred persistence: source contracts and existing native proof

Internal Session persistence now covers finite/default windows, infinity and unload/version-reload barriers in focused native scenarios. Added retention/reopen controls pass unchanged runtime; the shared-public-value lifetime and tracker-count assertions need further work. Public persistence settings and Harness integration are unfinished. The generation oracle still has its recorded failure; the 25 Session and eleven Harness source bodies have not been executed or completely ported.

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

## Deadline increment, 2 November 2026

The internal policy now uses a 1,000 ms default, the configured default getter or an explicit per-commit window. Each held document retains its earliest deadline. The earliest document arms one foreground timer; a later commit cannot postpone it, infinity arms none, and a stored dependent write retargets or clears it. Explicit negative/NaN windows reject before the callback with `StorageRejected`, the native adaptation of source `TypeError`. The existing infinite-window test entry still selects infinity explicitly. Public options and the Harness remain unwired.

The timer has a third fixed Session dispatch slot, separate from the two background cache timers. Its host callback atomically marks the job fired and returns without waiting for the line or storage. A worker requests admission; any admission that reaches the line first drains the fired job before its own work. This preserves the source test's `fire()`/on-line-read barrier without scheduling yields or observer acknowledgements. A fired callback stores every hold even if the manual clock has not advanced, matching the pinned test. Storage settlement and contiguous receipt adoption precede a stored-only publication; readiness is released off line. Failure latches the original backend cause, closes storage and drops volatile holds without retrying them.

`durable/session_deferred_deadline_main_test.go` translates the c9f4c7b9 scenarios at lines 347, 365 and 378 and numeric-window rejection at 273. It preserves the two-conversation `one`/`two` values, one stored batch/two changes/one publication, 2,000 ms default, +500 ms clock move, 100 ms earlier and 60,000 ms later windows, and infinity/default/immediate-release sequence. Native adaptations use the reserved root, raw document append operations, real sequence/delta counts, queued asynchronous stored subscriptions and Snapshot as the on-line barrier. Complete synchronous observer semantics and upstream TypeScript execution are outside this increment; corpus dispositions keep their earlier scope. Two further native tests cover nonblocking host admission, stale/repeated fires, refusal/closure/reopen; arm/cancel callbacks also reenter Snapshot.

Before runtime edits, the three timer scenarios failed nine backend leaves on unchanged `4856a592a3225bb9f4cf12d433c9040c2e06b47c`; numeric-window rejection failed three. The baseline adapter selected the existing real infinite-only primitive because finite/default policy did not exist. An initial cleanup-function signature error was corrected before those credited runs. The earlier generation baseline was not rerun.

The expanded context regression gate caught an unnecessary second `Context.Err()` check on ordinary admission, which moved the existing off-line fixture's pause under the line. The second check now runs only after actual timer maintenance. Context tests and counters are unchanged; the focused regression passes three repetitions in both CGO modes (1.147/0.742s).

Final Go 1.27.3, `GOMAXPROCS=4`, prototype excluded:

```text
-run '^Test(MainSession|Session|Document|Transaction|MainContext|MainWatchPending|MainTranscriptPublicationWindowPendingFold|GenerationProgress|Generation104ConfiguredPartialIntervalProductionPath|Close140)'
normal -count=3 -shuffle=42: CGO1 54.750s; CGO0 41.415s
race   -count=3 -shuffle=7:  CGO1 221.606s
```

Active later-wins, missing-stored-publication, missing-receipt-adoption, background-timer, blocking-host and clock-recheck mutations fail 3/3/3/9/3/3 backend leaves. The last mutation checks that an early host fire still stores holds. Final durable vet/Staticcheck v0.8.2 pass both CGO modes, with the same physical prototype exclusion for Staticcheck; module verification, formatting, diff and prototype-content checks pass. Comparisons use equivalent assertions; these timings are development checks, without profiling or performance claims.

Close-time stored-observer delivery, unload/version reload/retirement, retention/lifetime, conversation-view memory delivery and synchronous observers still need implementation and source tests. Fractional/extreme windows, the native duration ceiling and full-width custom-clock conversion need numerical checks before public/Harness wiring. Durable task intent has not been split from later generation presentation; the recorded generation oracle is unresolved. No dependency, coalescer, release-ref or asset changes, full suite, profiling, hosted dispatch or publication. All completed source extracts, copies, overlays, mutation fixtures and logs are disposed after analysis.

## Unload and version-reload barriers, 2 November 2026

Runtime: `a96e05f4f97c5655ee81e82b7c9f72d829cb4a99`; base/rollback: `1dd49497aaea5036e4e5ee90d91a9d9daad2b6c6`. This increment reuses the existing native deadline fixture, manual foreground timers and reserved root. It changes no public persistence options or Harness policy.

### Source-to-native assertions

All references below are to `packages/durable/test/session-deferred-commits.test.ts` at c9f4c7b9. The source bodies were inspected before runtime changes; native assertions use real storage, not a simulated receipt.

| Source | Native proof and result on unchanged 1dd4949 |
| --- | --- |
| 391: healthy Close/reopen and failed flush still closes Storage | Existing `TestMainSessionDeferredFallbackAndClose` and `TestMainSessionDeferredCloseRefusalStillCloses` reused. Added failed-end/exact-cause and original-backend-closed assertions to the refusal test; they pass unchanged. `StorageRejected` models the controlled backend refusal while preserving error identity. |
| 410: failed Session stores nothing | Existing `TestMainSessionDeferredFailureDoesNotFlush` retained; no new baseline rerun or production fix. |
| 420: unload stores holds, clears cache/holds, later Snapshot reloads the value | New `TestMainSessionDeferredUnloadDocuments` fails on all three backends because the stored text is empty. |
| 429: another-version Snapshot stores one delta before migration; later commit yields `held+v2` | New `TestMainSessionDeferredDefinitionVersionReload` fails on all three backends because no delta was stored. `held`, `+`, `v2`, version 2 and `migrated: true` are preserved. |
| 445: retiring batch contains change then retirement, with the exact held append delta; later Snapshot is absent | New `TestMainSessionDeferredRetirement` passes unchanged on all three backends. No retirement-specific production fix. Native command names adapt `document.change`/`document.retire`; the test inspects actual admitted backend payloads. |

Four further test functions are labelled source-derived/native controls, not additional upstream bodies. `SessionImpl.#loadDocument` stores selected holds before dropping a tracker and calling migration. Rollback after typed acquisition, a throwing Snapshot migration and two successive version reloads inside a callback distinguish that barrier from folding everything into a later successful commit. The multiple-prefix case also stages an entry with a freshly reserved ID and an edit before the second reload, then checks final values, three contiguous receipts, stored publication count and readiness. Cancellation controls reject before admission and preserve/adopt an admitted store while returning cancellation to its reader.

The first run fails 12 backend leaves (unload, version reload, rollback barrier, multiple prefixes). Migration-failure and admitted-cancellation controls add six failures; cancellation before admission already passes. A failure-count shell regex was corrected against the existing second log without rerunning or changing the tests. Source TypeScript was not executed; native typed definition/transaction calls, the internal policy entry and queued asynchronous subscription frames are explicit adaptations. Full synchronous-observer parity is not credited.

### Implementation and verification

`storeHeldDocuments` stores a selected set through the existing writer and contiguous receipt adoption, publishes only to stored subscriptions, removes only those holds and retargets the deadline. Unload stores all holds before clearing migrated-value caches. Current Snapshot/watch/transaction definition acquisition stores a held document before a different-version reload. Historical snapshots stay on their existing path.

Once this maintenance owns the line, storage settlement uses the caller's trace values without its cancellation; caller cancellation is checked before and after the reload barrier. No admitted store/receipt is abandoned. An in-callback reload refreshes the Tx's immutable base/history and invalidates provisional candidate caches while retaining staged writes and reserved IDs. The outer commit uses that refreshed baseline. It does not rerun the callback or invent a receipt. Multiple stored prefixes share the line-release readiness channel so none remains permanently queued.

Final Go 1.27.3, `GOMAXPROCS=4`, protected prototype excluded:

```text
-run '^Test(MainSession|Session|Document|Transaction|MainDefinition|Definition|MainContext|MainWatchPending|MainTranscriptPublicationWindowPendingFold|GenerationProgress|Generation104ConfiguredPartialIntervalProductionPath|Close140|Invocation.*Document|Task.*Document|Harness.*Document|Observation)'
normal -count=3 -shuffle=42: CGO1 67.808s; CGO0 45.600s
race   -count=3 -shuffle=7:  CGO1 248.151s
```

Active skip-unload, skip-reload, flush-all-instead-of-selected, skip-Tx-rebase, fresh-per-prefix-readiness, skip-post-store-cancellation and skip-adoption mutants fail 3/15/3/3/3/3/3 backend leaves. The rebase mutant rejects with `invalid commit sequence`; the adoption mutant rejects the stale storage baseline. Both CGO modes pass durable vet and Staticcheck v0.8.2 using the same physical prototype exclusion, plus module verification, formatting, diff and prototype-content checks. These are affected development gates, not performance measurements or a full-suite result.

The canonical Session review entry and its generated TSV evidence now point to these native increments; dispositions, inherited evidence and inventories remain unchanged. Retention-zero pins, old-revision collection, conversation-view memory delivery, synchronous observers (including Close delivery) and numerical window/clock edges remain unverified before public/Harness integration. The source391 body does not itself assert Close observer delivery. Durable generation intent/presentation are still unsplit; the recorded generation failure was not rerun. No dependency/coalescer/release-ref/asset changes, profiling, hosted dispatch, publication or full-convergence claim. Completed source extracts, package copies, mutation fixtures, overlays, logs and 1.4 GB build cache are disposed after use; installed dependencies/toolchains remain.

## Retention and recovery controls, 2 November 2026

Three new functions in `durable/session_deferred_retention_main_test.go` pass unchanged runtime `a96e05f4f97c5655ee81e82b7c9f72d829cb4a99` (documentation head `b1df4b4`). No production fix follows from this passing baseline. Existing deadline/lifecycle gates and the recorded generation baseline were not rerun.

| Source at c9f4c7b9 | New native proof and remaining assertions |
| --- | --- |
| 600: retention zero keeps a held document cached/attached until storage | `TestMainSessionDeferredRetentionZeroOwnedValue`: `held`, a write-free barrier, real storage unchanged before fire, hold released after fire and cold read of `held`. Native owns the held record separately from the idle migration cache. Source cached/attached tracker counts are unported; the test labels its native cache checks separately. |
| 613: a weakly observed shared snapshot stays alive until storage, then collects | `TestMainSessionDeferredHeldOverlayLifetime`: `x` repeated 1,000 times, five GC rounds before and after storage, and cleared recorded publications. Weak pointers target the actual pointerful Session-owned `*Snapshot` overlay; a native extra-root control stays alive until released. Collection preserves the durable value. The shared public Text-object WeakRef assertion is unported: native public snapshots detach maps. This control cannot establish collection of every underlying map/value still referenced elsewhere. |
| 638: store `a`, hold `b`/`c`, fail and reopen with `a` and no holds | `TestMainSessionDeferredCrashLastStoredMemoryState` preserves the progression, recovered `a`, membership in observed memory states and empty reopened holds. A real asynchronous native document watch replaces `followCommits`; waiting for its frames is an explicit API adaptation. Synchronous observer semantics are unverified. |

The common deadline fixture accepts an optional retention value through the existing `SessionOptions.DocumentRetentionMs` constructor option. Calls without that argument retain their previous behaviour. No mock receipt, fabricated publication, scheduling yield or production acknowledgement is added.

Initial focused baseline passes in 0.156s. Final Go 1.27.3, `GOMAXPROCS=4`, with the protected prototype excluded:

```text
-run '^TestMainSessionDeferred(RetentionZeroOwnedValue|HeldOverlayLifetime|CrashLastStoredMemoryState)$'
normal -count=5 -shuffle=42: CGO1 4.416s; CGO0 1.793s
race   -count=3 -shuffle=7:  CGO1 1.635s
```

Four active compiler-overlay faults force immediate storage at retention zero, evict held ownership, retain the old overlay after storage, or flush a failed Session. They fail 3/3/6/3 backend leaves respectively. The zero-retention and recovery-of-`abc` failures belong to these deliberate faults; the unchanged-runtime baseline has no failures. Logical and canonical overlay paths cover both workspace aliases.

Durable vet and Staticcheck v0.8.2 pass in both CGO modes; formatting, diff and prototype content checks pass. Staticcheck uses a real package copy without the prototype. Its initial symlinked root embed input was rejected; copying root files physically fixes the tooling error, with no suppressed findings. Dependencies and module files are unchanged; prior module/security receipts keep their existing scope.

Next work is source521-599 stored/memory observer and conversation-view delivery, including stored publication at Close. Source600 tracker counts, source613 public identity/GC, numerical window/clock edges and public/Harness integration are unresolved. A read-only object-backed snapshot boundary could preserve existing detached-map callers while representing shared identity, but its ownership, lifetime and observer integration need a tested design. No divergence is accepted here. No upstream TypeScript execution, full suite, profiling, dependency/coalescer/release/asset changes, hosted dispatch or publication. Completed raw extracts, package copies, mutations, overlays and logs are disposed after analysis; installed dependencies/toolchains are preserved.

## Pending stored-observer oracle, 2 November 2026

The complete source521 `a`/`b`/immediate-`c`/`d`-timer/`e`-dependent-entry/`f`-Close sequence is preserved in `durable/testdata/upstream-main/pending/session_deferred_observers_test.go`. Real admitted backend writes reconstruct Storage independently of publication operations. Final `abcdef`, per-prefix replica/value equality, publication count and the exact four change-type arrays are unchanged. The native legacy asynchronous subscription adapter waits for its first three real callbacks; it supplies no fabricated final publication. The fixture is outside default discovery, with an explicit virtual-file overlay invocation in its README.

Unchanged `bd2123f` fails all three CGO backends (0.079s) and both non-CGO backends (0.053s): three publications for four stored batches; the final `["document"]` is missing. Storage reaches `abcdef` and the first three publication replicas match. A result-type compile error and a mistaken deferred-zero/immediate adapter were corrected before those credited baselines. They do not establish implementation failures. Source immediate mode uses the ordinary commit path; zero deferred milliseconds still means deferred.

Close currently stops subscriptions before flushing, and its held writer adopts without preparing a publication. Source c9 explicitly forbids synchronous observers from blocking or calling the Session; legacy native asynchronous listeners may reenter. A separate source-aligned lane can preserve both contracts. [Reviewed ending/guard/order requirements and next controls](synchronous-observers.md#stored-observer-counterexample-at-c9f4c7b9). No production edit or accepted divergence; prior retention/lifecycle/deadline and generation gates are reused without rerunning them. This is a failing baseline, with no Source execution or observer-parity result. Pending-fixture vet and Staticcheck v0.8.2 pass both CGO modes, excluding the protected prototype (physical package copy for Staticcheck); formatting/diff/content checks pass. Canonical Session review/generated TSV evidence changes one row; inventories, flags, dispositions and dependency queue are unchanged. Completed source extracts, package copies, overlays, logs and build cache are disposed after analysis; dependencies/toolchains and native v1.4.0 are preserved.
