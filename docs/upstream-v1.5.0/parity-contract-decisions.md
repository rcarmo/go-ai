# Decisions needed before parity implementation

**Status:** P00 investigation memo at `88bfce9`; no runtime changes. It records source contracts, measured Go oracles, conflicts and choices for Rui. The official v1.5.0 target is `f9a26365568127461c47a07a37dcb42c140a3ac5`. Verify refs before resuming; local `HEAD=88bfce9`, `origin/main=c422344` when written. The untracked continuation prototype was not changed.

## Decisions requested

| Ref | Source contract | Native observation | Choice required |
| --- | --- | --- | --- |
| D1 — AfterRun after model tool round | Pinned `harness-after-run.test.ts` source694: selected `afterRun` runs on final `answer()` after the model returns from tool rounds. `generation.ts:864` calls `asksAfterRun(runtime, context)` before committing the answer; source test also expects history/context boundaries after mid-run compaction. | Pending `harness_after_run_history_test.go`, `TestMainAfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction`: callback is never invoked after tool round, even when explicitly selected and without gate/compaction. It fails before later history assertions. Native `handoffToolGeneration` creates a pending successor and resets live run tool state; Go finish paths choose `afterRun` locally. The precise state/selection loss point has not been isolated; ordinary afterRun/cutoff and recovery controls need preservation, but no existing test is proven to require skipping the hook. Native HookAPI lacks the source's whole-history range scan. | **Recommend source-aligned hook invocation on the answer path.** Approve carrying/recomputing hook eligibility across handoff while preserving cutoff, run boundaries and unrelated successor state? The bounded range-read surface needed for the exact post-compaction kind sequence remains separate follow-up work; its assertion must not be silently waived. |
| D2 — Close signal and interrupted attempt | Pinned source429 checks that an orderly close during drain causes the existing task to run again after reopen without re-asking the hook. Its task waits for a gate or abort signal and has an abort handler; the close subcase does not independently assert an abort count. Source758 requires close to signal the invocation during an in-flight call and drains/stores that call before storage closes. | Pending source429 `TestMainAfterRunCloseInDrainResumesInterruptedAttempt`: native does not signal the running drain; abort handler count0, executions1, commits1; phase commits while closing and task finishes. Pending source758 `TestPendingGroupCommitCloseSignalsInvocationDuringCallInFlight` records related signal mismatch. | **Recommend source-aligned attempt fencing while allowing an already admitted storage write to settle.** At what exact point is an attempt interrupted/its late commit fenced? Does a completed abort-handler outcome count as decided? Which existing close/requeue expectations should change? |
| D3 — Opposite wait cycle | Pinned `harness-group-commit.test.ts` source226 (#11141): after first wait is admitted, the opposite second wait is rejected with a reason containing `wait cycle`; first completes. | Pending `TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits`: both opposite waits commit; neither settles; no refusal during 4.243s. Native has waiting states, `On`/policy and `MemoCandidate`, so the case is expressible. | **Recommend refusal at admission of the edge that closes the cycle.** Investigate whether the effective dependency image in the current serialized Session is sufficient, or whether P05 is required. Preserve valid acyclic/shared waits; the two explicit runtime-change pauses apply to D1 and D2, not this investigation. |
| D4 — Refusal, latch and error identity | Pinned session-group source414 and related441/461/486/680 distinguish earlier successful parts, in-call refusal, later SessionFailed results, first error identity and deferred publication. | `session_group_commit_refusal_test.go` exceeds native `MaxWrites` and is rejected before admission, unlike source414's in-storage predicate. Native reports `StorageRejected`, does not latch, and a later operation succeeds. The separate settlement-failure control `TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError` passes for an uncertain settlement: earlier part survives, failed part does not, Session latches, but native reports its own uncertain-outcome classification. | **No policy change until the refusal mechanism is faithful.** Which confirmed refusal classes must latch? Which exact error identity must later reads/writes/close expose? Do invalid pre-admission inputs remain non-latching? Approve only after a faithful in-call refusal oracle exists. |

## P00 evidence and trace

### D1 — source/native call paths

- Source: `packages/durable/src/harness/generation.ts` at `f9a26365`, `answer()` around864 and `asksAfterRun()` around894; test `packages/durable/test/harness-after-run.test.ts` source694.
- Native: `durable/generation.go` (`finishGeneration`, `finishWithAfterRun`), `durable/generation_handoff.go` (`handoffToolGeneration`). `tool.go` and `hooks.go` call handoff from distinct control paths. Handoff creates a fresh `prepare-next` successor with input fields, transfers `pi.live.run` retaining `first`, and deliberately does not copy the old request/offer state. The finish path decides afterRun from locally selected hooks; `generation.go` includes successful Stop, Length and ToolUse among selection conditions. A lost-selection/checkpoint explanation is a hypothesis: the exact post-tool loss point is not isolated.
- Oracle: `durable/testdata/upstream-main/pending/harness_after_run_history_test.go`, test `TestMainAfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction`.
- Observed failure: hook skipped. The fixture checks that the active context omitted the summarized input while committed run history retains it; those assertions cannot be evaluated because callback does not run. Its exact source `api.entries(minEntryId..maxEntryId)` sequence is separately unportable through HookAPI and is not relaxed into a false pass.
- Candidate acceptance only after D1 approval: pending oracle unchanged; `TestMainAfterRunChildRestartLimitCompletesGeneration`, `TestMainAfterRunHookReadsCommittedEntryBoundaries`, `TestMainCutoffHooksAtLimitNotPastLimit`, `TestMainAfterRunRunWithoutRecordedFirstEntryAnswersAsFirst`, and handoff tests found from call graph.

### D2 — source/native close path

- Source: `packages/durable/test/harness-after-run.test.ts` source429 and `packages/durable/test/harness-group-commit.test.ts` source758.
- Native: `durable/harness.go:Close`, `durable/task_scheduler.go`, `durable/close_requeue.go`, `durable/session.go`, invocation cancellation and outcome-settlement callers.
- `Harness.Close` sets closing/sealed, unsubscribes and cancels harness life, then waits for scheduler completion before acquiring the Session line with `taskBookkeeping` to call `endOnLine`; it dispatches the resulting runtime cancels off-line, joins invocations, calls `requeueAtClose` and closes Session. Runtime contexts originate from `context.WithCancel(context.WithoutCancel(h.life))`, so cancelling harness life alone does not cancel them. `taskBookkeeping` takes the Session line; a held admitted commit may delay `endOnLine` and signal dispatch. This explains a plausible source758 delay, but a safe off-line signal/fence design is not yet demonstrated. `requeueAtClose` preserves restart counts and skips failed Sessions.
- Pending source429 numbers: `aborts=0`, `executions=1`, `commits=1`. Native allows the running phase to commit while closing and records a completed task. Keep this distinct from orderly close while an afterRun task is already running: `TestMainAfterRunCloseInFinishReasks` expects eligible hook work to be asked again after reopen.
- Relevant controls: `durable/close_requeue_140_test.go` tests repeated clean close/reopen without restart-budget consumption and rejected requeue followed by crash recovery; `TestMainGroupCommitChildEndsBeforePhaseWhenMarkUnpublished` covers unpublished abort marks.
- Candidate acceptance after D2 approval: both pending close oracles separately and together with those controls; close while handler waits, while Session commit is admitted, after task decision, abort callback return/error, rejected/uncertain requeue, reopen; prove old attempt cannot commit late while an already admitted durable call is drained.

### D3 — source/native wait path

- Source: `packages/durable/test/harness-group-commit.test.ts` source226 (`#11141`), opposite conversation-owned memo waits. The test expects the second cycle-forming wait to be refused and the first task to complete.
- Native: `TaskRuntime.commit` in `durable/task_runtime.go:74` copies the returned `TaskState`, builds the task execution and stages a `put-task`; `Session.finalizeTaskWrites` calls `taskScheduler.prepareTaskWrites` before settlement. `prepareTaskWrites` currently returns immediately unless a staged execution is terminal. Thus it does not itself check a newly staged waiting edge. `boundWaitCycle` in `task_scheduler.go:1658` traverses durable waits, owned work and volatile bound waits for invocation-wait admission; that is not evidence that task state admission checks persisted opposing `On` edges. Pending fixture captures both admitted waits and bounded no-settlement. Validate all other task-write validation before claiming a complete absence of checks. A candidate is a bounded effective-candidate check at task-write finalisation, before storage; whether it can reuse the bound-wait traversal and how it treats pending P05 edges needs investigation. Do not call this unportable.
- Candidate acceptance: two-node and longer cycles reject at admission; acyclic chain, shared acyclic dependency, foreign-owner rejection and abort still work. If this needs P05, name the exact serial-admission limitation and stage P05 before cycle fix.

### D4 — refusal mechanism and failure fence

- Source: `packages/durable/test/session-group-commit.test.ts` sources414/441/461/486/680.
- Native: `durable/storage_group.go`, `durable/session_receipt.go`, `durable/session_publication.go`, `durable/session_group_commit_main_test.go`.
- Pending fixture `durable/testdata/upstream-main/pending/session_group_commit_refusal_test.go` has a MaxWrites overflow before storage admission. It is not a faithful source414 backend-predicate refusal. Pending README records same first failing assertion: later operation succeeded because Session was not latched.
- Separate control `TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError` exercises append/settlement error classified by native as uncertain outcome. It proves earlier data survives and failed data does not; it does not prove source refusal error identity.
- Native error routing: `storeCore.settlementError` returns an existing `StorageRejected` without poisoning the store; other append failures poison it and become `ErrPoisoned`. `Session.backendResult` latches the first error that reaches it as a mutation result. Pre-admission errors can return before that boundary. These facts do not establish the Session result of every backend refusal; trace and test the actual call path.
- Next investigation: locate a native in-call backend refusal injection path that preserves the actual SQLite boundary. If none exists, specify the missing test seam/API as a gap; do not model it using pre-admission MaxWrites.

## Approval gate and next actions

1. Rui chooses D1 semantics; the missing bounded range-read surface remains separate parity work unless an adaptation is explicitly agreed.
2. Rui chooses D2 signal/fence/abort/requeue ordering and legacy expectation migration.
3. Continue P03's admission-graph investigation and determine whether P05 is prerequisite. The pinned source defines the desired refusal; only D1/D2 runtime changes have the previously stated explicit pause.
4. P04 first reports a faithful refusal mechanism and classification table; obtain direction before changing a conflicting latch/error policy.
5. After decisions, update P01–P04 packets with the signed-off contract and only then implement. P05/P06/P07/P08 remain separate architectural decisions; P13 full acceptance/profiling/release remains separately authorised.

## Current focused baselines

At `88bfce9`, each pending file was compiled separately with the protected prototype excluded via a project-owned overlay, pure Go/SQLite, count 1. These are expected failures, not acceptance passes:

| Oracle | Result |
| --- | --- |
| `TestMainAfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction` | Hook not invoked; FAIL 0.171s; history checks unreached. |
| `TestMainAfterRunCloseInDrainResumesInterruptedAttempt` | No abort handler; executions1/commits1, no rerun; FAIL 0.101s. |
| `TestPendingGroupCommitCloseSignalsInvocationDuringCallInFlight` | Invocation not signalled during held call; FAIL 0.592s. |
| `TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits` | No refusal, neither completed; FAIL 4.253s. |
| `TestPendingGroupCommitRefusalFailsTheSession` | Later operation succeeded; FAIL 1.101s. This is the pre-admission MaxWrites fixture, not a faithful in-call refusal. |

The eight named candidate controls from D1/D2/D4 were run together, count 1: eight top-level passes, 0 failures, 0 skips, 0.814s. These include the two close-requeue tests, three afterRun entry/restart controls, cutoff limit, unpublished abort mark and settlement failure fence. `go vet` with the prototype overlay and `git diff --check` passed. This is a focused regression baseline, not whole-file acceptance.

No runtime change, profiling, full suite or external publication occurred. Scratch overlays and logs were removed after analysis.
