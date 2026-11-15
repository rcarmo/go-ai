# Canonical builtin execution

Target: official pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

GenerationTask2 and ToolTask3 are now real definition tokens held by every Registry. They execute canonical records through the generic TaskRuntime. Generation1 migration preserves its input/checkpoint. Tool1 migration adds the model discriminator; Tool2 preserves its input. Registry replacement is refused. Source: harness/registry.ts10–26, generation.ts162–175 and tool.ts136–158.

The canonical handlers read source input/checkpoint fields and committed records directly. They do not reconstruct private generation/tool checkpoints. Canonical nested calls create pi.tool3 with `kind:nested`, and use the source `taskId/result` family document. The private nested schema guard is excluded only when the document's owner has the canonical tool execution representation; existing private forgery controls retain their assertions. Canonical task commits no longer invoke the private generation-status projector.

## Uncommitted production-admission migration

The worktree after `89c031ebcdc7374b28240ca09511bfcd7e9f8c22` is **not regression-green**. Normal Submit now creates source Generation2 records and uses canonical Inbox/live `taskId` boundaries. The obsolete private explicit-continue producer has been removed. These changes have not been committed or pushed.

Production-path failures led to fixes for sequential selection, addTools removal clearing, codemode agent conversion/nested `taskId` cleanup, prompt projection, transcript image references/hydration and UTF-16 argument diagnostics. Reset now queues a source write draft with `head:self`. Public native Settlement conveniences derive their answer/task from source records; they no longer depend solely on private checkpoint payloads. Full public-API alignment still needs audit.

Scheduler-written fault/orphan outcomes now use the source live.ts cleanup at the final terminal transition; ordinary owned work delays it. A deferred poll uses the response decoder instead of the contribution codec, which rejects deferred handles. The production deferred-fault, real HTTP tool-commit/reopen, owned compaction, overflow and UTF-16 diagnostic packets now pass. Tool execution intentionally stays in the `call` handler after its intent commit (tool.ts136–239); the trace fixture now distinguishes model/nested pi.tool by `input.kind` and reads call IDs from input. The passive-write fixture uses canonical `data`, retaining its payload-mutation and stale-head assertions.

## Canonical recovery and abort packet (uncommitted)

Execution follows plan revision 145. Pinned tests and source determine the native refactor; native backwards compatibility is not required. The whole worktree is red. Last verified pushed revision stays `89c031e`.

Assertion translations:

| Pinned source | Native target | Assertion scope |
| --- | --- | --- |
| harness-generation.test.ts287–304 | TestDeferredRepollKeepsRequestAttemptAndSkipsRequestHooks | Two increasing stored live poll times, done/input answer; supplemental one request/two polls/hook counts and poll attempt1 |
| harness-generation.test.ts307–328 | TestGenerationAbortConvertsOnlyCommittedPartialAndNoSyntheticAssistant | Production streamed partial, stored-publication barrier, unanswered/aborted input, exact entry kinds/text/stopReason, exact live failure and task outcome; supplemental no-partial, usage and reopen |
| harness-generation.test.ts330–374; generation.ts582–597 | TestDeferredAbortCancelsRemoteOnceAndReportsFailure | One cancellation, no early fetch, unanswered/aborted input, exact live failure, aborted task outcome, cancellation error identity; supplemental provider panic, attribution/telemetry and repeat-abort |
| harness-deferred-progress.test.ts228–248 | TestMainHarnessDeferredPollAfterCrash | Stored handle, crash/reopen, clock1000→61000, done/input answer, one fetch; supplemental no redispatch and stored poll attempt1 |
| generation.ts finishToolRound; scheduler owned-work holding | TestGenerationHookHeldHandoffReopenDoesNotReplayHooksOrRequests | Supplemental canonical completing/terminal outcome preservation, assistant entryId, held child resumes once without hooks/request replay; removed private Builtin.Hold dereference |
| harness-generation.test.ts203–218; prepare/failResponse | TestGenerationDurableRetryUsageAndReopenDeadline | Supplemental durable retry deadline, failed receipt, usage and reopen; observed canonical retry state replaces private checkpoint polling |
| harness-generation.test.ts511–546; prepare/failResponse | TestGenerationRetryRepreparesCommittedAgentToolsPromptAfterReopen; TestGenerationRetryDecisionReadsCurrentSettingsAfterResponse | Supplemental changed agent/model/tools plus host stream/policy on next preparation/decision; settings belong to Harness, not pi.agent |

These are bounded translations and source probes. The two whole source files contain additional assertions not accepted natively by this packet. Public stored publications replace private checkpoint reads and polling/Gosched. Terminal source tasks discard checkpoints, so attempt assertions inspect stored poll/retry invocations. Typed native ID values in expected JSON were a fixture comparison error; exact numeric JSON expectations now match without dropping fields.

Before runtime fixes: reopen/repoll/expiry passed; cancellation lacked attribution and a provider panic faulted the run; partial/no-partial production stream conversion passed. The crash-poll and held-handoff fixtures initially failed on obsolete terminal attempt/private hold access. A private commitPartial live fixture failed, then hung its cleanup behind a provider gate; it now emits a real partial and honours cancellation. Host retry-policy changes still retried, and temperature disappeared from stored stream options after fixture corrections.

Runtime refactors: source best-effort deferred cancellation uses the guarded provider boundary and reports host/models errors while preserving aborted settlement and host identity. Response classification reads current Harness settings instead of the phase-cached agent policy. Stream checkpoint projection retains temperature. No runtime change was made for already passing partial/no-partial or reopen/repoll cases.

Verification (Go1.27.3, CGO=0, GOMAXPROCS=2, sequential -p1):

- Deferred/abort focused packet:1.054s. Recovery/abort/held-handoff three shuffled repetitions:5.783s. Extended packet including retry/live presentation: three shuffled repetitions11.710s.
- Complete actual-tree commands, no exclusions: durable111.808s failed/panicked in the private held-handoff fixture; after correcting it, durable1200.059s timed out during failed live-partial fixture cleanup. Tools failed9.224s/9.773s on large-image provider input. Other tested packages passed in those runs. The live-partial fixture now passes1.304s; no subsequent full-tree passing receipt exists.
- Original unchanged harness-generation:26 pass, zero skips,1.949s; separate unchanged harness-deferred-progress:11 pass, zero skips,5.069s. Each command also ran90 supplemental TypeScript probes; these are separate from original and native assertions.
- Whole-tree vet, Staticcheck v0.8.2, logging and diff check pass after the extended packet. Semantic mutations and another full regression are next; no new commit/push or full acceptance.

Remaining reproduced baseline failures include blocking compaction expectation, edited-ancestor HTTP context, environment abort envelope, event turn/tool starts/updates, failed-run projection/withdrawal trace/owner, handoff private successor assumptions, model-error envelope and tools large-image input. Later suite coverage is incomplete because the full command timed out. Additional legacy generation/recovery fixtures still require source reconciliation.

Analysed logs and transient scripts are removed after recording their findings. Pinned source/dependencies are preserved.

## Later baseline and controls (uncommitted)

Five compiled semantic mutations were detected: absent cancellation attribution, unguarded throwing cancellation, omitted partial conversion, omitted temperature projection and phase-cached retry policy. Every mutation was restored; the extended packet including canonical handoff/model-error checks passes three shuffled runs13.336s. An initial anchored mutation command selected no tests and supplies no fault credit; the corrected commands executed the assertions.

The next full actual-tree command failed after durable1200.157s and tools9.545s. It reported59 top-level failure groups before timing out in TestM1bIntentBeforeDispatchAndPinnedContext cleanup. That fixture read retired private intent fields and its provider ignored cancellation; canonical request checkpoint/cutoff/model invariants, unchanged intent after configure and exact provider context now pass three shuffled repetitions0.442s. The full suite has not been rerun after that correction.

Observed groups are an incomplete baseline, with runtime and fixture causes still to be reconciled against source:

- `TestAutomaticCompactionRetryDecisionFollowsCurrentSettingsAfterResponse`
- `TestOverflowCompactionOneRecoveryAndNoOrdinaryRetry`
- `TestEntryContextActualHTTPSystemToolsEditedAncestorReopen`
- `TestEnvironmentFactoryAbortAndFailureFenceEffects`
- `TestAgentEventsActualPartialsAndToolEndAdjacentToResult`
- `TestAgentEventsCommittedToolLifecycleAndReentrantListener`
- `TestFailedRun140InboxPolicies`
- `TestFailedRun140BoundedWithdrawalRecovery`
- `TestFailedRun140WithdrawalJoinsOwnedHosts`
- `TestGeneration140PacingAlarmAloneResumesRetryAndPoll`
- `TestGenerationTimer140ProductionClockEarlyFireAndTerminalDisarm`
- `TestMainAfterRunAnswerTasksHoldRun`
- `TestMainAfterRunTerminateSummary`
- `TestMainAfterRunStopAnswered`
- `TestMainAfterRunCloseInFinishReasks`
- `TestMainAfterRunMissingDefinitionStopOrphans`
- `TestMainAfterRunCrashFinishReasksDrainDoesNot`
- `TestMainAfterRunAnsweredFinishRestartLimit`
- `TestMainAfterRunChildRestartLimitCompletesGeneration`
- `TestMainBoundaryAtomicSuccessorPublication`
- `TestMainBoundaryEligibilityInheritedCancellationBeforePlacement`
- `TestMainBoundaryEligibilityAbsentAgentHostAllModes`
- `TestMainBoundaryEligibilityLocallyDecidedFailFast`
- `TestMainBoundaryFirstIncludesPassiveWrite`
- `TestMainBoundaryLifetimeBeforeModelResolution`
- `TestMainBoundaryLegacyRecoveryBusyControl`
- `TestMainBoundaryOwnedInvocationIdleChildAdmission`
- `TestMainBoundaryOwnedAfterResponseHeldAdmission`
- `TestMainBoundaryPassiveWriteRollback`
- `TestCanonicalInboxWritesAreBoundaryAuthority`
- `TestCanonicalInboxMissingWriteFailsAfterAdmissionCallback`
- `TestMainCutoffGenerationDuringPreparation`
- `TestMainCutoffSafeToolKeepsStoredOutput`
- `TestMainContinueBoundaryKeptInputsAndWrite`
- `TestMainContinueBoundaryLegacyFirstFallsBackToActualWrite`
- `TestMainContinueNothingToAnswerEmptyAndAnswered`
- `TestMainContinueToolHandoffOrderedInputs`
- `TestMainContinueBusyRejectAndRequestIDDedup`
- `TestMainContinueOwnedToolAfterModelError`
- `TestMainContinueOwnedSubmissionAbortBeforeProviderDerived`
- `TestMainCutoffHooksAtLimitNotPastLimit`
- `TestMainCutoffHooksNormalizeReplacement`
- `TestMainCutoffHooksThrowReportsAndRetainsSlot`
- `TestMainCutoffKeepsCommittedPartialAtLimit`
- `TestMainCutoffDropsPartialPastLimit`
- `TestMainCutoffDeferredSalvagePhasesDerived`
- `TestMainRunFailureOrdinaryAbortProjection`
- `TestMainRunFailureAbortHandlerCutoffProjectionDerived`
- `TestMainRunFailureAttachedInputsWithoutQueuedSuccessor`
- `TestMainRunFailureNoModelBeforeRequest`
- `TestMainRunFailureNoModelReplacesPreviousModelError`
- `TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement`
- `TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor`
- `TestMainRunFailureContinueQueueClearsFailureAfterAnswer`
- `TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor`
- `TestMainHarnessRecoveryCrashAfterConversion`
- `TestMainGenerationAgentEventsBeforeStorage`
- `TestM1bTerminalAndExplicitUnsupported`
- `TestRead120DefaultLargeImageReachesProvider`

The latest command exposes more afterRun, boundary, cutoff, report, queue and dependency failures; prior R1–R8 inventory was incomplete. No exclusions, isolated substitutions or passing older checkpoint establish integrated acceptance. Next: source reconciliation of these shared canonical record consumers, then another complete baseline.

## After-run and tool-recovery packet (uncommitted)

Canonical after-run tests now read live.run.taskId and capture the generation from LiveDoc, without the extra native Settlement.Task inference. The orphan probe directly inspects definition fit instead of polling. Source generation.abort ends the task aborted while preserving done inputs; the old supplemental completed assertion was corrected from generation.ts390–414. Source after-run478 task run/abort counts were added unchanged.

Before runtime fixes, the corrected TestMainAfterRun packet failed4.270s: terminate/handoff skipped hooks and tasks, and answered finish cutoff omitted task_interrupted. Native tool recovery independently failed1.080s: unsafe replay lost stored output and safe replay did not durably clear it before a second effect.

Runtime refactors:

- Terminate rounds enter finish before applying the final boundary when afterRun is selected. The committed summary uses the newest tool-result entry, or the handoff reset, as last; inputs settle done and remain busy while children run. Source finishToolRound/holdRun and harness-after-run221–248.
- Runtime.Commit reports terminal crash outcomes after its stored commit, retaining the pre-transition restart count. Failed outcomes carry their source reason; answered cutoffs use a private returned-state marker which is cleared from copies and never serialized. Source scheduler.ts2659–2715/3003–3020 and generation.ts974–988.
- Unsafe tool recovery builds output blocks from stored slot output, matching source fromSlot. Safe replay clears progress with immediate Runtime.Commit before effects, matching tool.ts execute; deferred progress cannot establish that recovery barrier.
- BeforeRequest recovery checks committed cutoff/context and unchanged original messages through public records. Its retired private Messages dereference caused the full-suite panic; no runtime change was needed. Source generation.ts request's context/beforeRequest ordering; supplemental clean-close/reopen probe.

Assertion scope:

| Pinned source | Native assertions |
| --- | --- |
| harness-after-run181–218 | TestMainAfterRunAnswerTasksHoldRun: done at answer, empty run inputs, one child, owner taskId/background false, pending idle, exact summary, final child entry, completed result entryId |
| harness-after-run221–248 | TestMainAfterRunTerminateSummary: finish/handoff last entry, exact terminate summary, one requested task run, one provider call, no active run |
| harness-after-run429/478/757/785/955/983/1019 | Crash-finish reasks/drain does not, Stop preserves answered input and task run/abort counts, answered/child restart limits, missing-definition orphaning and lowered-limit controls in harness_after_run_* tests |
| harness-deferred-progress341–394 | TestMainUnsafeToolCrashUsesStoredOutput and TestMainSafeToolReplayClearsOutputThroughSecondCrash: exact output/diagnostics, no unsafe rerun, durable safe clear through second crash and third execution |
| scheduler Runtime.commit/endedByCrashes | TestRuntimeCrashTerminationReportStoredBoundary: supplemental failed/completed report order, exact message/reason/task/restarts, no marker in public state and rollback suppression |

The original unchanged harness-after-run file passed29 tests, zero skips,0.746s; its90 supplemental TypeScript probes ran separately. Native21 TestMainAfterRun functions passed4.272s and three shuffled repetitions12.922s. The combined after-run/report/deferred packet passed17.459s; extended packet including tool and BeforeRequest recovery passed three shuffled repetitions19.667s. Whole-tree vet, Staticcheck v0.8.2, logging and diff check pass. Five compiled semantic faults were detected across terminate hook admission, answered-cutoff marker, report omission, lost recovery output and deferred safe clear. A fault script initially expected the wrong missing-report diagnostic; corrected assertions failed on report not delivered, never compilation or process timeout.

Full actual-tree attempt after after-run fixes: durable163.708s failed and panicked in the private BeforeRequest recovery fixture; tools9.088s failed. It reached71 top-level failure groups, beyond the previous cleanup hang. That BeforeRequest fixture now passes three shuffled runs0.446s; the two source tool-recovery failures now pass1.187s. No subsequent full-tree passing receipt exists. Remaining groups include canonical boundary/queue/withdrawal, progress/events, generation/tool cutoffs and metadata, HTTP context, compaction, hook/provider and tool-result envelopes. Source assertion mapping and later suite enumeration are incomplete. No commit/push or release acceptance.

Analysed full logs and mutation scripts were removed; pinned source/dependencies are preserved.

## Tool cutoff metadata and partial timer packet (uncommitted)

Source tool.ts endCutOff passes reason restart_limit through settle into outcome.error.detail; Runtime.commit reports that committed failure as task_interrupted. The native cutoff omitted the detail. The corrected code preserves the failure reason, exact message and pre-transition restart count. Source afterHarnessResult adds a truncation diagnostic after bounding a hook replacement; native settlement bounded the text but omitted that diagnostic. The existing cutoff/hook packet failed1.606s before fixes and passed3.269s afterwards with both demonstrated defects corrected.

TestMainCutoffSafeToolKeepsStoredOutput now checks the source diagnostics array instead of requiring the extra private receipt ErrorCode. Source harnessError/fromSlot use diagnostics; the failed task retains detail.reason. Other output, usage, task outcome, no-reexecution and report assertions remain. TestMainCutoffHooksAtLimitNotPastLimit maps unchanged harness-tool-hooks585; NormalizeReplacement/ThrowReportsAndRetainsSlot are labelled source-derived probes of afterHarnessResult. All check real reopen/cutoff paths.

TestGenerationTimer140ProductionClockEarlyFireAndTerminalDisarm is a supplemental timer probe beside source harness-generation partialIntervalMs coverage. It now observes live.run.taskId, canonical request checkpoint and actual conversation-watch memory frames. It retains early-fire deadline100ms, no premature partial, exact first prefix, terminal storage/partial timer cancellation and dropped late window assertions. The old private submission/checkpoint polling timed out; corrected fixture passes three shuffled runs0.445s without a runtime change.

Verification: original unchanged harness-tool-hooks file18 pass, zero skips,1.110s;90 supplemental TypeScript probes ran separately. Extended recovery/afterRun/cutoff/timer packet passes three shuffled repetitions27.870s. Two compiled mutations (omitted failure reason and omitted bounding diagnostic) fail behavioural assertions; restored packet and whole-tree vet/Staticcheck v0.8.2/logging/diff pass. Full source tool-hook equivalence is incomplete; bounded passes do not accept every assertion in that file.

Latest full actual-tree command, no exclusions: durable183.521s failed and panicked in TestM1bCloseReceivedTerminalCommitsNoReplay (provider closes entered twice after unexpected redispatch); tools8.799s failed on large-image provider input. It reached66 top-level failure groups. Prior private BeforeRequest panic no longer stops the suite. No complete passing baseline exists. The terminal-close/reopen runtime contract and its noncooperative fixture need source reconciliation before changing either behaviour or assertions. Boundary/queue/withdrawal, events, generation cutoff salvage, environment, provider metadata, tool receipt tests and later suite coverage are still open. No commit/push or release acceptance.

## Verified pushed packets

Tests are consolidated in `durable/builtin_tasks_test.go` and `registry_builtin_test.go`:

- Definition versions, initial checkpoints, generation identity migration and both original nested-tools1230–1250 tool-input migration cases.
- Imported Generation1/2 no-model records execute and persist the exact source terminal-state JSON.
- Actual canonical model answer and model→tool→model execution through Harness task creation, reservation and SQLite.
- Tool1/2/3 model calls migrate, execute once and append the model result entry.
- Unsafe stored intent does not execute; safe stored/current replay policy executes once.
- A throwing executor fails its task with `Tool thrower threw` and retains its model result reference.
- Canonical nested task kind/version/input/result, structured result and unavailable-tool settlement. Admission does not resolve the callee; abandonment follows the caller's replay policy, as tool.ts520–538 requires.
- Builtin definitions cannot be replaced through direct registration or extensions.

The former ordinary-suite builtin targets first failed on missing definitions and bounded import waits. Once execution worked, the no-model fixture's typed struct equality rejected identical JSON because of Go representation differences. It now compares the complete source JSON state without dropping fields or normalising aliases. Compile mistakes, that equality mistake, a missing reference package alias and a missing reference receipt callback receive no implementation-failure credit.

Further controls failed before their corrections: model executor throw0.114s, builtin replacement0.024s, nested private representation0.199s, and premature unavailable-callee rejection0.096s. Strict DTO checks caught typed diagnostic/usage values placed directly in JSON; these now cross the trusted encoder before persistence. The nested fixture declares the structured schema required by its result.

## Gates

The complete actual working tree passes26 tested packages: durable458.890s, tools21.525s, public consumers12.579s. No compiler overlay, excluded tests or weakened original assertions. An earlier complete run passed475.966s before the later nested/refusal corrections. Final ten shuffled canonical repetitions pass14.008s; forged-input controls pass three repetitions0.280s. Vet, Staticcheck v0.8.2, logging and parity register pass.

Six independently compiled semantic faults are detected: missing Tool1 discriminator migration, unsafe replay, wrong no-model state, invalid request phase, private nested definition reuse and missing builtin protection. Analysed scripts, logs and overlays were removed.

Selected original oracle:282 passing cases, zero skips (200 unchanged and82 independent probes). Original nested-tools plus the supplemental packet passes125 cases, zero skips. The latest reference probe checks actual Tool1/2/3 migration/execution and throwing-executor failure. These source executions do not establish native acceptance of all original assertions.

## Remaining work

**Full alignment is incomplete.** At the pushed checkpoint, normal Submit and legacy tool callers still create private builtin records. The uncommitted migration above replaces normal admission but has failing regression tests; its remaining cleanup and recovery paths need reconciliation. Custom-task nested calls still use the private nested kind. The passing tests above cover canonical task creation and imported execution, not those producers.

All source phase names have executable handlers, but their full assertion packets are not accepted. In particular, complete provider-payload hooks, turn limits, terminate/afterRun control combinations, deferred cancellation/cutoff reporting, malformed-stream/error timing, progress/summary fidelity, full nested replay/lifetime cases and fault/orphan run cleanup require further source reconciliation. Canonical queue/boundary helpers need the complete original submission/inbox packet through the migrated producers. Shared record identity, original SQLite format/API and complete public type/lifecycle/readonly acceptance are open. The complete oracle/image equality and live-provider credentials are unaccepted. Pre-release CPU, alloc_space and alloc_objects profiling/tuning are incomplete.

## User-requested incomplete checkpoint

The user explicitly requested commit and push after the red baseline above. This snapshot preserves the canonical admission/refactor work and bounded verification; it is a WIP checkpoint, not a verified full-tree candidate. Last fully verified pushed checkpoint remains `89c031ebcdc7374b28240ca09511bfcd7e9f8c22`. Earlier references to uncommitted work describe its state when those checks ran. Continue terminal-close/reopen source reconciliation and the remaining plan; do not release this snapshot as accepted parity.
