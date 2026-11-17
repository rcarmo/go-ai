# Canonical builtin execution

Target: official pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

GenerationTask2 and ToolTask3 are now real definition tokens held by every Registry. They execute canonical records through the generic TaskRuntime. Generation1 migration preserves its input/checkpoint. Tool1 migration adds the model discriminator; Tool2 preserves its input. Registry replacement is refused. Source: harness/registry.ts10–26, generation.ts162–175 and tool.ts136–158.

The canonical handlers read source input/checkpoint fields and committed records directly. They do not reconstruct private generation/tool checkpoints. Canonical nested calls create pi.tool3 with `kind:nested`, and use the source `taskId/result` family document. The private nested schema guard is excluded only when the document's owner has the canonical tool execution representation; existing private forgery controls retain their assertions. Canonical task commits no longer invoke the private generation-status projector.

## Production-admission migration (known-red checkpoints)

The worktree after `89c031ebcdc7374b28240ca09511bfcd7e9f8c22` is **not regression-green**. Normal Submit now creates source Generation2 records and uses canonical Inbox/live `taskId` boundaries. The obsolete private explicit-continue producer has been removed. The migration is committed/pushed in365f5b6; fixture reconciliation6b637a3 and cutoff salvagec988681 follow. These are known-red checkpoints, not full integration acceptance.

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

## Clean-close and final-reset reconciliation after WIP checkpoint

User-requested incomplete checkpoint365f5b6 was committed and pushed;89c031e stays the last fully verified tree. Native compatibility does not constrain this port.

Source generation.ts classify first calls runtime.signal.throwIfAborted: a provider returning a terminal message after Close does not settle that attempt. The source generation-recovery75–125 and restart-failure254–273 resend cleanly at the same attempt, with no restart charge. The retired TestM1bCloseReceivedTerminalCommitsNoReplay expected a late terminal to be stored and then panicked in its provider on redispatch. TestGenerationCloseBeforeTerminalClassificationResendsRequest now asserts the source contract: Close joins noncooperative effects; late result is absent from storage; reopened task remains pending/request/attempt1/restarts0; input remains placed, Open dispatches nothing; explicit wait resends once and appends only one final assistant, with no duplicate input. The companion noncooperative-close test checks the canonical pending request before dispatch, replacing its impossible terminal checkpoint attempt assertion and removing its sleep. Production code was already correct for these cases. Three shuffled repetitions pass1.292s.

The next full actual-tree command (all packages, no exclusions) failed durable213.651s/tools9.611s, reaching75 top-level failure groups before a reset fixture indexed block Content on a canonical string-content message. The final-reset fixture now preserves the string union and checks exact user/assistant/reset entry order and reset head:self, retaining done settlement and exact new context text. Source harness-inbox272–291. Corrected close/reset packet passes three shuffled repetitions1.759s. Other reset tests and later suite enumeration are still incomplete; no full-tree acceptance.

Original unchanged generation-recovery9 pass, zero skipped,0.816s; separate inbox37 pass, zero skipped,0.781s. Each command also ran90 independent TypeScript probes; those are not original/native assertion credit. Whole-tree vet/Staticcheck v0.8.2/logging passed after close reconciliation; rerun after final-reset correction below. No runtime edits were required by this packet.

Latest full-baseline observed groups (runtime versus fixture cause still requires pinned-source reconciliation):

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
- `TestMainContinueBoundaryKeptInputsAndWrite`
- `TestMainContinueBoundaryLegacyFirstFallsBackToActualWrite`
- `TestMainContinueNothingToAnswerEmptyAndAnswered`
- `TestMainContinueToolHandoffOrderedInputs`
- `TestMainContinueBusyRejectAndRequestIDDedup`
- `TestMainContinueOwnedToolAfterModelError`
- `TestMainContinueOwnedSubmissionAbortBeforeProviderDerived`
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
- `TestM1bLargeLocalModelAuthorizationHTTP`
- `TestMainInterruptedBudgetAndCloseAccounting`
- `TestMainInterruptedPollRetryEdges`
- `TestMainPollTerminalOnlyBoundaryDerived`
- `TestMainAbortedPollCancelHandlerCutoff`
- `TestMainPollCutoffSkipsRepeatedCancellationDerived`
- `TestMainToolProgressDeferredMemoryAndPersistence`
- `TestMainToolProgressMemoryEventsNotStorageReplay`
- `TestM1cActualHTTPToolResultAwareAnswer`
- `TestM1cSchemaSubsetAndRepairBeforeIntent`
- `TestM1cNotOfferedAndInvalidArgumentsZeroEffects`
- `TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI`
- `TestM1cCloseNoncooperativeToolKeepsOwnershipAndReturnedOutcome`
- `TestM1cRegistryPhasePinOutputCapAndAtomicAppFailure`
- `TestM1cCommittedPartialAndQueuedWithdrawal`
- `TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay`
- `TestM1cAbortAtOfferedCallBoundaryStartsNoTool`
- `TestBeforeToolBlockOrThrowPreventsEffectAndRevalidatesReplacement`
- `TestIdentity140DurableDefaultHostAndPinnedOverrides`
- `TestInputMetadata120QueuedReopenBeforePlacement`
- `TestM1bPinnedModelRecoveryIgnoresRegistryBehaviorChange`
- `TestAssistantProtocolFieldsSignaturesPresenceContextAndReopen`
- `TestNestedTool120ModelParentCallAttribution`
- `TestOverflowFailedOrDeclinedCompactionEndsWithoutAnotherRequestOrReceipt`
- `TestGeneration104ConfiguredPartialIntervalProductionPath`
- `TestReaderUTF16MainCommittedEventsAndSnapshot`
- `TestReport140ModelHostFailuresReportAfterFallbackAdoption`
- `TestReport140StructuredOutputUsesCapturedProducer`
- `TestResetIdleAndPostToolsStartsFollowUpInNewContext`
- `TestResetAtFinalBoundaryKeepsAnswerThenCutsContext`
- `TestRead120DefaultLargeImageReachesProvider`

## Generation cutoff salvage (after6b637a3; incomplete integration)

Pinned harness-restart-failure621–674 and generation.ts endCutOff preserve a committed partial at the restart limit and drop it past the limit. Deferred poll/retry handles are cancelled only at the salvageable boundary. Native fixtures now capture live.run.taskId and canonical request state, preserve partials in LiveDoc, and compare the exact source runFailure shape without native failedRun. Supplemental seeded poll/retry checkpoints use canonical model/handle/cutoff/attempt fields; they are not original crash scenarios.

Translated tests on unchanged runtime failed0.951s: at-limit request/poll/retry omitted the aborted assistant, and poll/retry cancellation count was0 instead of1. Past-limit control already passed. Production cutoff now calls best-effort canonical cancellation before committing, and converts the partial in the same failure transaction before endRun. Past-limit cleanup stays disabled. Abort and cutoff share the source deferredOf/cancelDeferred helper; no private checkpoint reconstruction. Focused correction1.382s passes.

Three semantic faults were detected: omitted partial conversion, cleanup past the limit and omitted deferred cancellation. Restored broad cutoff/afterRun/report/deferred packet passes three shuffled runs25.281s. Original unchanged harness-restart-failure18 pass, zero skipped,2.720s;90 supplemental TypeScript probes run separately. Other assertions in that source file are still unaccepted natively.

Full actual-tree attempt, no exclusions: durable1200.042s/tools9.383s failed;75 top-level failure groups before timeout in TestSequentialAdmissionAtTerminalDrainAndEmptyWorker. The storage gate searched retired phase:terminal and waited without a deadline; the fixture now gates canonical status:terminal and uses public WaitForIdle instead of the removed conversation-worker map. Admission during the held terminal receipt and after idle both settle; extended three-shuffle packet28.352s passes. No runtime change was needed for that fixture. Full regression still red and later enumeration incomplete. The timeout dump also contains many generationProgress actors from earlier failing paths; exception-safe progress cleanup needs a source-derived regression and runtime audit.

Whole-tree vet, Staticcheck v0.8.2, logging and diff pass after the final fixture correction. Checkpoint authority does not establish full parity;89c031e stays the last fully verified tree. Analysed logs and temporary scripts removed; source/dependencies retained.

## Progress, reset and tool receipt packet

Current focused work after c988681 passes; integration remains red. No complete plan workstream is accepted.

| Pinned source | Native assertion/scope |
| --- | --- |
| generation.ts690–754 streamResponse finally | TestGenerationProgressProviderPanicRetiresActor: public idle/Close joins before goroutine stack comparison; acquisition panic leaked one actor before deferred stop |
| source Inbox/admission IDs | TestFailedRun140InboxPolicies: canonical runFailure/Inbox queue checks; minted Go IDs now survive same-transaction trace conversion |
| harness-inbox.test.ts272–306 | TestIdleResetCanonicalStringContentAndClock: exact string content and host timestamp7; final/owned boundary tests use taskId/Generation2, not private run fields |
| scheduler terminal ownership cleanup; live.ts settleSchedulerOutcome | TestToolSchedulerFault*: exact orphan/result semantics, canonical decision faults and held owned host; supplemental native tool-end event waits for terminal adoption |
| tool.ts94–96,817–858 | Byte flush/reopen, strict scalar/array/null details and thrown diagnostic tests resolve public Task result.entryId to public Entry.Model, not private Result checkpoints |
| harness-tools.test.ts424–440; tool.ts674–736,1106–1118 | Large-image error/reconnect supplemental probes preserve non-text payload exactly; canonical settlement now persists conversation-owned image chunks before entry settlement |
| same source output union/boundContent | Final-head and output-policy fixtures supply a single ordered block array, replacing native-only simultaneous Content+Blocks. Exact text/image/truncation assertions retained; public receipt diagnostic replaces absent terminal private policy/counters |

Before production fixes, actor count rose0→1; same-transaction IDs rejected with submission-not-found; idle reset returned block content/timestamp0; canonical tool-end appeared while completing; image tests failed0.628s with missing provider/reconnect payloads. The earlier packet contains the individual red/green test transcripts in history. Progress actor, ID, string reset, clock reset, terminal tool-end and omitted image-chunk mutations all fail their corresponding assertions, then restore. Six semantic controls, no compilation-only credit.

Combined progress/reset/owned/fault/image/receipt tests pass three shuffled repeats26.019s; restored image/receipt packet8.680s; source-correct output union fixtures1.934s. Whole-tree vet, Staticcheck v0.8.2, logging and diff checks pass before the final test-only output/progress fixture edits. Unchanged original inbox37 and tools42 pass with zero skipped;90 independent TypeScript probes per command counted separately. Whole original files are not full native assertion acceptance.

Actual-tree durable runs, no exclusions: baseline8=237.135s/92 top-level failed groups, baseline9=235.407s/92, baseline10=372.996s/94, baseline11=377.177s/93. Each stops at an obsolete private-state fixture panic, not a complete baseline. Earlier summary counts74 for8/9 were incorrect; these are exact anchored FAIL-line counts. Fault/byte-output/policy panic fixtures are now source-correct; the next full run remains required.

Historical baseline11 failures before its panic:

- `TestAutomaticCompactionRetryDecisionFollowsCurrentSettingsAfterResponse`
- `TestOverflowCompactionOneRecoveryAndNoOrdinaryRetry`
- `TestEntryContextActualHTTPSystemToolsEditedAncestorReopen`
- `TestEnvironmentFactoryAbortAndFailureFenceEffects`
- `TestAgentEventsActualPartialsAndToolEndAdjacentToResult`
- `TestAgentEventsCommittedToolLifecycleAndReentrantListener`
- `TestFailedRun140BoundedWithdrawalRecovery`
- `TestFailedRun140WithdrawalJoinsOwnedHosts`
- `TestGeneration140PacingAlarmAloneResumesRetryAndPoll`
- `TestMainBoundaryAtomicSuccessorPublication`
- `TestMainBoundaryEligibilityInheritedCancellationBeforePlacement`
- `TestMainBoundaryEligibilityAbsentAgentHostAllModes`
- `TestMainBoundaryEligibilityLocallyDecidedFailFast`
- `TestMainBoundaryFirstIncludesPassiveWrite`
- `TestMainBoundaryLifetimeBeforeModelResolution`
- `TestMainBoundaryLegacyRecoveryBusyControl`
- `TestMainBoundaryPassiveWriteRollback`
- `TestMainBoundaryWritesFirstAndSequentialHeads`
- `TestMainContinueBoundaryKeptInputsAndWrite`
- `TestMainContinueBoundaryLegacyFirstFallsBackToActualWrite`
- `TestMainContinueNothingToAnswerEmptyAndAnswered`
- `TestMainContinueToolHandoffOrderedInputs`
- `TestMainContinueBusyRejectAndRequestIDDedup`
- `TestMainContinueOwnedToolAfterModelError`
- `TestMainContinueOwnedSubmissionAbortBeforeProviderDerived`
- `TestMainRunFailureOrdinaryAbortProjection`
- `TestMainRunFailureAbortHandlerCutoffProjectionDerived`
- `TestMainRunFailureAttachedInputsWithoutQueuedSuccessor`
- `TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement`
- `TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor`
- `TestMainRunFailureContinueQueueClearsFailureAfterAnswer`
- `TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor`
- `TestMainHarnessRecoveryCrashAfterConversion`
- `TestMainGenerationAgentEventsBeforeStorage`
- `TestM1bTerminalAndExplicitUnsupported`
- `TestM1bLargeLocalModelAuthorizationHTTP`
- `TestMainInterruptedBudgetAndCloseAccounting`
- `TestMainInterruptedPollRetryEdges`
- `TestMainPollTerminalOnlyBoundaryDerived`
- `TestMainAbortedPollCancelHandlerCutoff`
- `TestMainPollCutoffSkipsRepeatedCancellationDerived`
- `TestMainToolProgressDeferredMemoryAndPersistence`
- `TestMainToolProgressMemoryEventsNotStorageReplay`
- `TestM1cActualHTTPToolResultAwareAnswer`
- `TestM1cSchemaSubsetAndRepairBeforeIntent`
- `TestM1cNotOfferedAndInvalidArgumentsZeroEffects`
- `TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI`
- `TestM1cCloseNoncooperativeToolKeepsOwnershipAndReturnedOutcome`
- `TestM1cRegistryPhasePinOutputCapAndAtomicAppFailure`
- `TestM1cCommittedPartialAndQueuedWithdrawal`
- `TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay`
- `TestM1cAbortAtOfferedCallBoundaryStartsNoTool`
- `TestBeforeToolBlockOrThrowPreventsEffectAndRevalidatesReplacement`
- `TestIdentity140DurableDefaultHostAndPinnedOverrides`
- `TestInputMetadata120QueuedReopenBeforePlacement`
- `TestM1bPinnedModelRecoveryIgnoresRegistryBehaviorChange`
- `TestAssistantProtocolFieldsSignaturesPresenceContextAndReopen`
- `TestNestedTool120ModelParentCallAttribution`
- `TestOverflowFailedOrDeclinedCompactionEndsWithoutAnotherRequestOrReceipt`
- `TestGeneration104ConfiguredPartialIntervalProductionPath`
- `TestReaderUTF16MainCommittedEventsAndSnapshot`
- `TestReport140ModelHostFailuresReportAfterFallbackAdoption`
- `TestReport140StructuredOutputUsesCapturedProducer`
- `TestResetIdleAndPostToolsStartsFollowUpInNewContext`
- `TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission`
- `TestConversationPartialRetryDisabledKeepsDefaultsAtActualDispatch`
- `TestStopBackgroundSuppressesReportAndChildStaysUsable`
- `TestTaskRecoveryBuiltinHoldFinalFailureAndReopen`
- `TestTaskRecoveryToolOutcomePanicRollbackFallbackAndReopen`
- `TestTaskRecoveryGenerationPreparationFallbackWakeSequence`
- `TestTaskRecoveryOfflineGenerationPreparationFallbackWakeSequence`
- `TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen`
- `TestTaskRecoveryQueuedWithdrawalOwnedChildDefersSettlement`
- `TestTaskRecoveryGenerationDecidingFinalFailureMatrix`
- `TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts`
- `TestTaskRecoveryHostPanicDecidingStoragePrefixAndReopen`
- `TestTaskRecoveryToolValidatorHostFailureBeforeIntentAndDecidingReopen`
- `TestTaskRecoverySuccessfulValidatorIntentStoragePrefixAndReopen`
- `TestTaskRecoveryOrderedToolContinuationAdmissionCloseAndLateOwnedWork`
- `TestTaskSchedulerCapacityToolChildHoldOne`
- `TestMainTaskTraceQueuedFirstInvocation`
- `TestTelemetry140AdoptedEntryAndSettlementEvents`
- `TestTool120GrammarConstraintDurableHTTPAndIntent`
- `TestToolControlsUnavailableOrThrowingCallCannotTerminateRound`
- `TestToolControlsPinnedUnanimousTerminationLastHandoffAndAddTools`
- `TestToolDiagnosticsCommittedDetachedAndRenderedOutsideDetails`
- `TestToolDiagnosticsInvalidReturnRejectsAppMutation`
- `TestToolDuration110ExecutedErrorPanicAndUnexecuted`
- `TestToolExecutionParallelAndSequentialRounds`
- `TestToolDefaultOutputLimitsTruncateAndReturnedContentReplacesProgress`
- `TestToolOutput104SkippedTailCountersAndWindow`
- `TestToolOutputFinalHeadBoundsCombinedTextOnce`
- `TestToolOutputPolicyPinsCopiesFinalTextAndNonTextBlocks`

Canonical progress follow-up: TestToolProgressDetailsWindowAndSealedWrites reproduced missing details/null/window events0.639s after moving its final receipt assertion to public result.entryId. Source LiveDoc uses run.taskId and model call identity from ToolTask input/slot, not private run.task or an empty terminal checkpoint. The presentation/event consumer now uses those canonical identities. Removing a settled slot is not reported as a new empty tool update. Exact detail12/null/retained, trimStart3/appendXYZ, call/task attribution, sealed writes and final receipt assertions pass. Three additional compiled semantic mutations (old owner key, lost call/name attribution, spurious final clear) fail unchanged assertions and restore. Total current packet controls:9. The final expanded three-shuffle packet passes32.984s, followed by whole-tree vet, Staticcheck v0.8.2, logging and diff checks. Original tools42 passed earlier with zero skipped, separately from90 independent probes. The deferred-memory timer/dependent fixture remains red on retired native operation paths; it is not included in the passing packet or accepted as parity.

## Tool timing, retained output and complete baseline

| Pinned source | Native translation |
| --- | --- |
| harness-tools.test.ts459–497; tool.ts554–582,817–858 | TestToolDuration110ExecutedErrorPanicAndUnexecuted:30ms execute,100ms before hook and75ms after hook; stored/public provider duration25≤ms<100; throw/panic flags, blocked field absence, exact model/entry duration equality and detached protocol pointer |
| harness-tools.test.ts1058–1076; tool.ts674–736 | TestToolDefaultOutputLimitsTruncateAndReturnedContentReplacesProgress: byte/line counts and full diagnostic equality, explicit result override; supplemental kept/replaced/empty/equal-text-distinct-array hooks distinguish output identity rather than value equality |
| harness-tools tail-window result assertion; finalResult retained diagnostic | TestToolOutput104SkippedTailCountersAndWindow: tail/head window/skip policy plus exact retained text and3 lines/28 bytes dropped diagnostic; terminal private counters replaced by source receipt assertions |
| harness-tools-recovery.test.ts430+; tool.ts execute recovery | TestSafeToolReplayClearsInterruptedOutputAndDetails: public live slot has no stale fields before replay effect, public execute state, committed clear event, fresh receipt/duration equal to actual provider projection; no terminal private checkpoint |
| source progress coalescing/memo/finalResult | TestToolProgressCoalescesFirehosePreservesMemoAndTerminalFlush: supplemental deterministic MemoCandidate completion channel replaces polling; public Task memo, full final receipt diagnostics/details, bounded publication count |

Source timing translation first failed1.894s because blocked calls stored durationMs0. It now uses an optional measured duration: nil for rejected/abort/cutoff/recovery and environment-failed non-executions; present (including0) only for execute attempts. Hook time is excluded. Original terminal checkpoint timing was not source state; provider/entry equality checks replace it.

Source-correct output fixtures exposed missing diagnostics: default/skipped red1.951s; firehose red0.292s after correcting its TaskValue memo fixture. Canonical execution now records omitted-output retention, carries it only when hooks keep that arm and appends exact dropped-byte/line diagnostics after hooks. Explicit replacement/empty/new array output discards retained truncation. The broader public ToolResult API/identity audit remains open.

Five compiled semantic faults are detected and restored: blocked duration0, omitted measured duration, timing inflated by100ms, missing truncation and truncation incorrectly kept after output replacement. Restored expanded three-shuffle30.237s; whole-tree vet, Staticcheck v0.8.2, logging/diff pass. Original unchanged harness-tools42 and tools-recovery10 pass zero skipped, with90 independent TS probes counted separately per command. Other source assertions remain unaccepted natively.

A targeted tool inventory first panicked15.116s in the old Builtin.Memos fixture; after porting it finishes24.598s with10 failed groups and no panic. The complete actual-tree run then finishes: durable421.579s/95 failed groups;25 other tested packages pass, tools9.322s/public10.955s. No overlays/exclusions. See [the full failure inventory](canonical-integration-baseline.md). Plan step1 baseline establishment is complete; integration remains red and step2 continues. Analysed scratch/logs removed; pinned source/dependencies preserved.

## Continue checkpoint and user-requested pause

User requested commit and pause for today; no further implementation is authorised until a new instruction.

Pinned harness-continue.test.ts185–201 and generation.ts616–625 distinguish the task's explanatory error from the entryless unanswered settlement/runFailure, which have reason:nothing_to_answer and no detail. The strengthened public-path native test failed0.202s on both empty/answered continues and exact LiveDoc shape. canonicalFailedRun now omits that detail while retaining the exact task error/message/reason object. No-model and model-error behaviour stay distinct.

Fixture reconciliation (no runtime shims): Continue boundary and owned-handle tests use live.run.taskId/turns and exact runFailure, without native failedRun. Handoff asserts the old generation's public completed result.entryId, new owner from live.run.taskId, empty Generation2 input/request attempt1, ordered transferred inputs and original entry placements. It no longer invents private successor/submission/steered checkpoint fields. The busy/dedup test now expects the exact source cross-type error: Request resume-1 already identifies a submission of type continue. Text/user/tool-result context, answer identity, trace, owned abort, lifetime, queued write, first fallback and eight handoff combinations remain tested through public production paths.

All TestMainContinue targets pass three shuffled repeats21.642s; unchanged original harness-continue13 pass zero skipped, with90 independent TypeScript probes counted separately. Whole-tree vet, Staticcheck v0.8.2, logging and diff pass. No new semantic mutation was run for this last pause packet; the full actual tree was not rerun after it. Complete baseline95 failures/25 passing tested packages belongs to the preceding timing/output tree, not a newly accepted current-tree result. Last fully verified tree remains89c031e.

Resume from plan step2, not release acceptance: reconcile the remaining canonical boundary/AbortSubmission/event/provider/compaction consumers against pinned assertions, then rerun full actual tree. Public Storage/readOnly/schema/AI/codemode/identity/profiling acceptance remains open. No job is intentionally left running.

## Resumed abort, boundary and event consumers

User explicitly resumed work from826f49e. Step2 remains active.

| Pinned source | Native assertion and scope |
| --- | --- |
| generation.ts abort390–416; live.ts endRun290+ | TestMainRunFailureOrdinaryAbortProjection now uses a real provider partial and stored-publication signal, source run.taskId and public Generation2 request. It verifies unanswered/aborted without detail/answer, exact LiveDoc, zero/one committed aborted assistant with original diagnostic/content and actual task attribution. Private commitPartial helper retired; synthetic Settlement.Message is not a source answer |
| harness-restart-failure.test.ts671–711; scheduler cutoff cleanup | TestMainRunFailureAbortHandlerCutoffProjectionDerived keeps its seeded abort/restart control label. Real request/partial retained, only abort mark/restart count seeded after host join; no private checkpoint/legacy partial fabrication. Faulted/restart_limit task/report, aborted run with exact cutoff detail, no salvage/usage/redispatch all retained. It does not port the original two real crashes/cancelDeferred assertions |
| inbox.ts77–119; generation.ts startRun/endRun | TestMainBoundaryFirstIncludesPassiveWrite reads source run.taskId/turns and public request input/attempt. W remains first, B retains its own placement, exact provider A,W,B order and both literal failure records persist |
| inbox.ts77–119; submissions.ts admitSubmission | TestMainBoundaryWritesFirstAndSequentialHeads uses canonical reset write drafts and real public SubmitIn boundary admission; exact first/head/stale/entry positions, reset settlement and string/block model union. Scheduler is sealed before admission: supplemental single-commit inspection, no host-execution acceptance |
| live.ts139–178; generation.ts streamResponse and tool.ts94–96/call intent | Native WatchEvents supplemental tests now project canonical run.taskId, execute intent/arguments and result.entryId. Real partial changes, turn/tool starts, call attribution, adjacent result/end, detached messages and no duplicate streamed assistant start are tested. WatchEvents is an extra native surface, not a claim to port an upstream event API |
| Harness settings getter/retry policy, harness-generation disabled-retry case | TestHarnessPartialRetryDisabledKeepsDefaultsAtActualDispatch configures the host; removed unsupported pi.agent retry override from this dispatch fixture. Separate native agent-settings presence test remains supplemental and outside full public-API acceptance |

Initial event tests failed3.482s: partial gate timed out, turn/tool starts and updates absent. Source-correct progress fixture now awaits Details, a logical publication barrier; fire-and-forget output may legitimately coalesce away before a returned replacement. Runtime reads canonical ownership, tool execute input/call identity and public result entry. Strengthened streaming test then failed0.921s on3 assistant starts instead of2: terminal arrangement now consults canonical live partials and does not start the same streamed message twice. Scope/holding/fault semantics remain in regression controls.

Fixture-only corrections exposed no further abort/first runtime defect: strengthened error-partial conversion reads the committed assistant entry, while unanswered submission Answer remains0. A temporary test-only decoder dependency was caught by production vet and replaced with the existing production detached-entry decoder. Final production vet passes.

Five compiled semantic faults detected/restored: obsolete generation owner key, omitted canonical tool start, missing result-entry resolution, duplicate streamed start, and the previously unmutated nothing-to-answer detail fix. Restored combined three-shuffle39.057s; whole-tree vet, Staticcheck v0.8.2, logging/diff pass after retiring the unused private partial helper. Original unchanged generation26/inbox37/restart-failure18 all pass zero skipped;90 independent TS probes per command counted separately. Source file passes do not establish native coverage of every assertion.

Complete actual tree, no overlays/exclusions: durable417.799s fails78 top-level groups;25 other tested packages pass, tools9.332s/public11.215s. No panic/timeout. [Updated failure inventory](canonical-integration-baseline.md). Previous95-group baseline is historical; the reduction includes the earlier Continue packet and fixture translations, not seventeen new runtime fixes. Integration and release acceptance remain red.

## Live settings after rendering and pacing signals

Pinned generation.ts186–267 awaits section rendering before thresholdCompaction/compactionPolicy and reads settings.stream in the request-preparation commit. The settings object has live getters. New source-derived TestGenerationThresholdReadsCurrentHostSettingsAfterRendering gates a real extension renderer, changes host policy, then observes actual provider options and task admission. Disabled→enabled and enabled→disabled both preserve the requested post-render policy, exactly one answer, expected zero/one summary and zero/one compaction task. The request also observes temperature0.75 after an initial0.25. Inputs and history use public canonical entry/admission APIs. This is a focused source regression, not a complete original test-body translation.

Before runtime fixes: disabling during rendering still admitted a compaction0.189s; after the policy correction, the sampling assertion exposed a cached0.25 request0.432s. Threshold planning now reads fresh host settings after rendering; stream options are read inside the actual preparation commit. No agent-snapshot settings fallback is used at those decisions.

Compaction fixtures now configure HarnessSettings explicitly, matching original harness-compaction.test.ts setup149–150. Blocking/background thresholds, model trace, overflow, retry and error/decline assertions retain their production paths. The resolved four-field helper only expresses explicit host-policy pointers. The old automatic retry fixture expected a provider answer after failed blocking compaction; pinned generation.ts196–203/compactionFailure471+ and harness-compaction's blocking-failure test require the exact model_error/Compaction failed: Summarization failed: 503 overloaded outcome, with one summary and zero answer requests. That fixture now checks the source outcome. Agent-settings presence/overrides outside these runtime fixtures remain a separate unaccepted API surface.

TestGeneration140PacingAlarmAloneResumesRetryAndPoll now supplies its1000ms retry deadline on the host, preserving early-fire/rearm/exact sleeping deadline/provider-count assertions. A terminal publication signal replaces its1ms polling loop; neither Wait nor Resume can mask a lost alarm. Both retry and poll cases pass.

Four compiled semantic faults detected/restored: pre-render cached threshold settings, forced-disabled policy, forced-enabled policy and pre-render cached stream options. Focused three-shuffle11.788s; restored broad Compaction/Threshold/pacing/Settings/StreamOptions selection10.456s. Whole-tree vet, Staticcheck v0.8.2, logging and diff pass. Unchanged original harness-compaction110 passes zero skipped, separately from90 independent TS probes. Original source execution does not establish full native assertion acceptance.

No complete ./... run after this settings packet. The78-group full-tree receipt belongs to8bd3d4f; the failure table is not arithmetically reduced from selected green tests. Step2 continues; full Storage/readOnly/schema/AI/codemode/identity/profiling and release acceptance remain open. Scratch fault/translation scripts were removed; pinned source/dependencies retained.

## Diagnostic JSON and source tool controls

Pinned tool.ts473–476 copies diagnostics as strict JSON; finalResult681–736, nestedResult882+ and appendToolResult1068–1099 preserve/render those values. The TypeScript severity union does not add a runtime enum validator. A new independent original-runtime probe supplies invalid/empty/uppercase/Unicode/error severity strings through both api.diagnostic and the returned result. It checks committed progress, a real api.commit effect, exact data.diagnostics, rendered model content and isError:false. The first probe omitted the mandatory details context and was corrected before reference acceptance; that fixture error has no semantic-failure credit.

The matching native public-path test first failed1.438s on the four nonstandard labels. validateToolDiagnostics now checks JSON string/resource and internal argument-witness boundaries without imposing a severity enum. Source data, order, original case and explicit result error flag survive. Ordinary JSON/resource/argument guards have separate negative tests. The canonical parent→nested ToolTask3 control also retains an unknown severity and structured output; the generic-call fidelity test is supplemental while its broader API migration remains open. Unsupported native ToolResult.Commit expectations were replaced by actual ToolAPI.Commit execution effects in these fixtures. The extra ToolResult.Commit API elsewhere is still unaccepted against source.

| Pinned source | Native assertion scope |
| --- | --- |
| tool.ts diagnostic/finalResult/appendToolResult; independent original diagnostic JSON probe | TestToolDiagnosticJSONSeveritiesPreserveProgressAndResult: five labels, stored progress, one api.commit effect, exact source entry data/diagnostic ordering, provider rendering, completed task and independent isError flag |
| tool.ts nestedResult; canonical nested task execution | TestSourceCanonicalToolNestedUsesSameDefinition additionally retains the same JSON label/structured output; generic TestNestedTool120DiagnosticJSONSeverityPreservesApplicationCommit remains supplemental |
| generation.ts finishToolRound1068–1140; harness-inbox.test.ts352–376,789+; context.ts leadWithSystem | Handoff alone makes the next request; unanimous terminate ends. Added combined handoff+terminate control. Exact last reset/string content, source rebased system/tool baseline, next answer, call count and reopen no-replay retained |
| harness-tools unoffered-call cases; generation/tool input unions | Two calls produce two canonical tool tasks, one flagged unoffered. Exact source diagnostic code/message/rendering and empty private errorCode, source result entries/hook order and no unanimous termination retained |
| tool.ts reported progress/Details barrier and final result | TestToolDiagnosticsCommittedDetachedAndRenderedOutsideDetails joins a real progress flush, sees exactly the reported diagnostic update, then checks both ordered committed result diagnostics and no mutation alias. Returned final diagnostics are observed in the receipt, not a fabricated final progress update |

Three compiled semantic faults detected/restored: enum restriction, severity lowercasing and deriving isError from diagnostic severity. Expanded three-shuffle28.152s and final deferred/exact-data subset5.769s pass; vet, Staticcheck v0.8.2, logging/diff pass. Original unchanged harness-tools42 passes zero skipped; supplemental reference probes now91 (new diagnostic probe plus previous90), counted separately. A legacy generic fixture's worker-local Fatal caused repeated retry diagnostics; it now checks the source result without Goexit. No runtime suppression/exclusion was added.

Complete ./...: durable416.189s fails72 groups,25 other tested packages pass (tools9.459s/public11.201s), no panic/timeout or exclusions. The two new names were stale deferred stream configuration on pi.agent, reproduced0.388s; moving it to HarnessSettings fixes both without changing runtime. The crash fixture preserves its explicit zero partial interval/infinite persistence window. Final targeted checks pass after that adjustment; no subsequent whole-tree run. [Observed failure inventory](canonical-integration-baseline.md). Integration remains red, step2 active. Analysed full log and scratch scripts removed.

## Canonical terminal tool consumers

Four existing fixtures now use public source records. No runtime code changed. Pinned tool.ts674–736/1068–1099 and harness-tools.test.ts retained-output assertions put diagnostics in the result entry; terminal tasks return its entryId. The truncation fixture now checks the exact ordered diagnostic objects, code/message/severity, identity, error flag and one-tool/two-request cardinality through Harness.Task and Harness.Entry. Provider content and hook replacement/preservation assertions remain unchanged.

| Pinned source | Native assertion scope |
| --- | --- |
| tool.ts finalResult/appendToolResult; harness-tools retained/explicit output cases | TestToolTruncationRemarksPinnedFallbackExplicitAndHookReplacement: four output/hook variants; full source result data replaces a private checkpoint diagnostic-count assertion |
| tool.ts899–953 startNestedCall | TestNestedTool120ModelParentCallAttribution: full model-call-120/child call ID, separate child key, caller/parent/task identity, exact canonical input and task ownership/version. Generic parent-call forgery fixture remains supplemental |
| tool-search.test.ts64+; persisted modelTools controls | TestToolSearch120LoadsPersistsAndDoesNotExecuteDiscoveredTool: one successful search-call receipt identifies tool_search; all existing HTTP offer, compaction/fork/reopen assertions retained. This scenario does not translate every assertion in the original search file |
| codemode-describe.test.ts91–98; generation.ts prepare; pi-ai api/openai-completions.ts | TestTool120GrammarConstraintDurableHTTPAndIntent: exact constrainedSampling in the canonical pi.system toolsAdded declaration, custom HTTP tool name/type, lark syntax/definition and cardinality. Source does not persist private Offered metadata in a terminal generation checkpoint. General grammar tool fixture supplements the original codemode declaration case |

Initial four-fixture run passes2.119s without runtime edits. The added exact nested-input check initially used the Go ID alias as an expected JSON number; corrected to the source numeric value (float64), without changing the actual record. This fixture error receives no runtime-fault credit. Expanded diagnostics/output/nested/search/grammar three-shuffle20.043s; restored focused three-shuffle5.491s and final single2.010s pass. Whole-tree vet, Staticcheck v0.8.2, logging/diff pass. Unchanged original tool-search6, codemode-describe5 and harness-tools42 pass zero skipped;91 independent probes per command remain separately counted.

Three compiled semantic faults detected/restored: remove nested parent-prefix, change persisted truncation code while retaining identical rendered text, and strip grammar declarations. The first grammar mutation selector hit an unrelated nil-dereference assertion; a second selector was incorrectly expanded by make and ran no tests. Neither is credited. The corrected exact-prefix selector fails the real grammar HTTP assertion. No production mutation remains.

Complete actual-tree ./... (baseline15): durable390.511s,67 top-level failing groups,25 other tested packages pass (tools9.086s/public11.809s); no panic/timeout/exclusions. Six previously failing names disappear: these four fixtures and the two previously corrected deferred fixtures. TestM1bRejectedAndUncertainIntentNoEffect newly fails at Submit with uncertain storage outcome; its ordering/cause remains unverified. The observed count is67, not an inferred subtraction. [Current full failure inventory](canonical-integration-baseline.md). Canonical round/abort/private boundary consumers, Storage/readOnly/schema/AI/codemode/identity/profiling and full acceptance remain open;89c031e remains the last fully verified tree.

## Live tool-round mode and canonical round consumers

Pinned generation.ts1010–1061 reads runtime.settings.toolExecution as the model response starts its tool round. The tools checkpoint then keeps the pending calls; later host changes do not replan that round. New source-derived TestToolRoundReadsLiveHostModeAndKeepsRoundDecision changes parallel→sequential and sequential→parallel inside the actual provider callback, then switches back inside the first real tool. Deterministic entry/release signals prove non-overlap or overlap, preparation ordering, task cardinality and final completion. Before the one-line policy fix, both directions fail3.433s: early second preparation/two precreated tasks and a missing parallel entry signal. The runtime now reads current host settings at that decision.

| Pinned source | Native assertion scope |
| --- | --- |
| harness-tools.test.ts266–292; generation.ts1021–1030 | TestToolRoundReadsLiveHostModeAndKeepsRoundDecision: both live mode changes and a mid-execution change which must not retarget pending calls. Source uses a timing trace; native uses deterministic entered/returned/release signals |
| harness-tools.test.ts294–343; GenerationTask2 migration162–173 and postTools handover | TestToolExecutionParallelAndSequentialRounds: default parallel, host sequential and per-tool sequential; preparation/effect order, one or two initial tasks, ordered model context, two completed conversation-owned generations, same generation owning both tools, canonical request attempt1/input{}, spent assistant via outcome.result.entryId, adjacent turn end/start sequence, one run start/end |
| GenerationTask2.migrate identity cast | v1-import variant is a supplemental canonical import control. Scheduling is sealed before actual Submit; only that real undispatched record's version changes to1 via the internal canonical setter. Its production input/checkpoint are untouched. Dispatch migrates to2; all public request/result/event assertions are identical. This replaces the obsolete fabricated private Successor checkpoint variant and is not counted as an original translated test |
| harness-structured.test.ts1263–1293; generation.ts390–416 | TestSequentialRoundAbortHasNoLaterTasksAndOrderedUnstartedResults expands to three calls: initial slots [started,pending,pending], public generation AbortTask, unanswered/aborted settlement, three ordered flagged results, exact diagnostic data/text for both unstarted calls, one tool task, aborted generation and no effect replay after reopen. Context cancellation supplies a deterministic mark signal; the former snapshot polling loop is gone. The separate full view-history assertions at1785+ remain unaccepted by this fixture |
| Original runtime host settings | Existing public-progression, builtin tool-trace and sequential-child scheduler controls now configure HarnessSettings.ToolExecution. Their behavioural assertions are unchanged; per-agent legacy settings cannot configure this source host policy |

The translated static round/abort fixtures pass2.129s against the pre-fix runtime, independently of the live-mode failure. After the fix: initial live three-shuffle2.122s; round/abort three-shuffle6.976s; expanded canonical/trace/progression/receipt controls23.105s; restored final round/trace/progression/scheduler three-shuffle13.470s. Whole-tree vet, Staticcheck v0.8.2, logging/diff pass. Unchanged original harness-tools42 and harness-structured58 pass zero skipped;91 independent probes per command remain separately counted.

Four compiled semantic faults detected/restored: cached host mode, ignored per-tool sequential requirement, replanning pending calls from changed host settings, and reversed unstarted abort results. All fail behavioural assertions; no production mutation remains. Two transient translation-script match failures occurred before writing files and receive no fault credit.

Full baseline16 exceeded the outer900-second command deadline without a completed durable result or diagnostic; its processes ended. It does not establish a native deadlock or completed baseline. A verbose unchanged-tree repeat (baseline17), with the same1200-second Go timeout and no exclusions, completes: durable853.852s fails64 groups,25 other tested packages pass (tools15.265s/public13.233s), no panic/Go timeout. The two canonical round/abort names now pass. The previously intermittent uncertain-intent name also passes this run; its cause remains unverified. No performance improvement or regression is inferred from these different runs. [Exact observed inventory](canonical-integration-baseline.md). Full integration/Storage/readOnly/dependencies/identity/profiling acceptance remains red;89c031e remains last fully verified tree.

## Canonical atomic boundaries and live queue modes

Pinned inbox.ts77–119 places writes before selected users; generation.ts startRun1162–1173 records taskId/inputs/turns/optional first, and answer/endFailedRun transfers control in that same commit. The atomic fixture now reads real public ObserveCommits records alongside the unchanged native stored/local delivery taps. No snapshots are combined into a publication. Exact public submission objects, A's terminal outcome, B's empty input/pending prepare-attempt1 state, ordered entry IDs, canonical live/inbox values and deciding sequence are checked; native sequence stamping remains supplemental because Source EntryRecord has no seq member. The public assertion requires that private field to be absent.

| Pinned source | Native assertion scope |
| --- | --- |
| inbox.ts applyBoundary; generation.ts answer/endFailedRun/startRun | TestMainBoundaryAtomicSuccessorPublication: answer/all, answer/one, model-error/one and no-model/one. Own A placement and settlement; answer/error then W then selected B/C; W is first, B owns its own placed entry; same-commit fresh conversation generation; retained failure; exact remaining queue; actual next provider context. Private checkpoint Submission/InputEntry/Successor assumptions are removed |
| submissions.ts admitSubmission; GenerationTask.prepare/abort; LiveRun.first optional | TestMainBoundaryLifetimeBeforeModelResolution: public Generation2 input{}, prepare/attempt1 checkpoint, taskId/turns0 run and actual placed input before model resolution. Conversation/task abort retains the actual callback until return, fences effects, dispatches no provider and settles its original input. Optional-first control preserves absence through request cutoff and final failure. Retained synchronous Go resolver/invocation checks are native supplemental host fences, not a complete upstream body/API port. Cancellation signals replace polling/Gosched |
| harness-inbox.test.ts final queue-mode cases157–212; generation.ts final and postTools boundary commits | TestMainBoundaryReadsLiveQueueModesAfterHooks: both boundaries and both one→all/all→one changes while onYield/afterTools awaits; actual next request users, queued/placed statuses and exact dispatch count. This source-derived control is narrower than the original case's separate occupied Session-line commit; that ordering assertion remains unaccepted |
| Source runtime host steering/followUp settings | TestFollowUpQueueModeOneAndAllShareOnlySelectedAnswers and TestBackgroundReportersCoalescedSteersReportSharedAnswerOnce configure the host. The one shorthand and native reporter API scenario remain supplemental. Existing shared-answer/dispatch/report assertions are unchanged; three real child-submission publications replace reporter admission polling |

The translated atomic fixture initially exposed an obsolete attribution assumption: A also appends the boundary's W/B entries, so byTaskId=A does not make them assistant receipts. Kind and actual assistant attribution now distinguish them. An added seq assertion was a fixture error: types.ts458 EntryRecord has no such member. Both errors were corrected from source before acceptance and receive no runtime-fault credit. Atomic/lifetime public translations pass2.804s without changing runtime for those expectations.

The new live queue-mode tests then fail2.105s in all four cases: final/postTools reused cached agent settings after the hook await. Both now read host modes inside the deciding commit. Combined three-shuffle29.235s; restored expanded Continue/round/no-model regression selection80.323s; final boundary/queue/reporter three-shuffle31.650s pass. Whole-tree vet, Staticcheck v0.8.2, logging/diff pass. Original unchanged inbox37/submit-in-commit23/structured58 pass zero skipped;91 independent probes per command are separate, not new original cases. No full original-file native-assertion acceptance is claimed.

Five compiled semantic faults detected/restored: cached final modes, cached postTools modes, successor first moved from W to B, discarded automatic-successor failure and prepare attempt0. An earlier first-entry mutation inspected only committed entries, never activated on the staged W and passed; it receives no fault credit. The corrected mutation produces a valid wrong first reference and fails the same deciding-batch assertions. No production mutation remains.

Complete verbose ./... baseline18, without overlays/exclusions: durable940.620s fails64 groups,25 other tested packages pass (tools15.619s/public13.077s), no panic/timeout. The atomic/lifetime names pass; the two queue/reporter fixtures newly fail because of agent-scoped configuration. Those fixtures were corrected afterwards and pass the final targeted three-shuffle run. No subsequent full tree: keep observed64, do not subtract two. [Full inventory](canonical-integration-baseline.md). The remaining old private queued-generation recovery/eligibility/withdrawal consumers, full public Storage/readOnly/API/dependency/identity/corpus/profile acceptance remain open.89c031e remains last fully verified tree.

## Canonical submission abort and positional inbox removal

Pinned submissions.ts148–163 reads the submission record, settles queued input/write unanswered/aborted and calls inbox.ts160–164 removeInboxItem. Placement/settlement determine the disposition; no generation checkpoint or owned-task scan participates. AbortSubmission now uses the existing internal canonical submission reader and Tx.SettleSubmission, then emits the exact one-position splice. The private queued-generation rewrite/termination branch and stringified ID filtering are removed from this path. Public Tx visibility is unchanged; its source interface does not expose the internal Transaction.submission reader.

A new source-derived native test first fails0.370s because the old loop removed both matching positions. An independent original-runtime probe confirms only the first position is removed, the other record stays queued, the duplicate remains, repeated abort reports settled and the operation is exactly [["p",["items"],0,1,[]]]. The first probe incorrectly treated SubmissionHandle.status as a property; corrected to await status(context) before acceptance, with no fault credit. Separate probes now92 (68+24); this new case is supplemental to the unchanged original submissions11.

| Pinned source | Native assertion scope |
| --- | --- |
| submissions.ts abort; inbox.ts findIndex/splice; independent original-runtime probe | TestSubmissionAbortRemovesOnlyFirstMatchingInboxItem: actual public created records, real inbox with two matching positions, exact deciding canonical value and positional operation |
| Source record-only abort authority | TestSubmissionAbortOwnsOnlyItsCanonicalRecordAndInboxItem: queued input and write, preserved explicit empty requestId/trace, exact public settlement and sole inbox change, untouched other submission and independent canonical generation, no task/entry mutation or scheduling, repeated settled disposition. Before runtime refactor the nonduplicate controls already pass0.850s with the existing disposition fixture |
| harness-submissions.test.ts109–131; raw queued and queued-write extensions | Existing TestSubmissionReferenceStatusAndAbortDispositionScopeQueuedWrites retains placed/settled/not_found/wrong-scope/no-write/detached-status/one-provider assertions. Existing Continue and invocation-bound abort/seal controls remain unchanged and pass |
| generation progress plus queued submission abort | TestM1cCommittedPartialAndQueuedAbort uses Abort/aborted and already_placed, canonical live.generation.message and unanswered reason, while preserving the physical stored-partial barrier and one provider. Explicit zero progress persistence, bounded signal waits and cancellation-aware fake stream prevent cleanup hangs. Retired private checkpoint/withdrawn-envelope assertions are replaced by source records |

The unchanged original source submissions11 passes zero skipped with92 separate probes. Native abort/Continue/invocation three-shuffle13.939s; adding the streamed partial fixture15.080s; final expanded boundary/queue/reporter/Continue selection37.265s. Whole-tree vet, Staticcheck v0.8.2, logging/diff pass.

Four compiled semantic controls detected/restored: filter all duplicate IDs, omit inbox deletion, strip request/trace identity and return settled for a placed record. The identity mutation is rejected by the existing submission-identity guard; it is not credited as a new output-comparison defect. All controls compile and fail behavioural/identity assertions, without a panic; no mutation remains.

Complete unchanged-tree verbose ./... baseline19: durable835.670s fails61 top-level groups,25 other tested packages pass (tools15.780s/public11.737s), no panic/timeout/exclusions. The previous two host-queue fixture corrections and the streamed partial/queued abort name now pass; no new failing names. [Observed inventory](canonical-integration-baseline.md).

The extra native SubmissionHandle.Withdraw method and four remaining legacy callers are still unaccepted under D13. Their fixtures bind obsolete queued-generation ownership/retirement and origin-lock semantics; this packet does not preserve those as a source contract or claim their reconciliation. Private eligibility/recovery consumers, provider recovery and all wider Storage/readOnly/API/identity/dependency/corpus/profile acceptance remain open.89c031e remains the last fully verified tree.

## Model metadata and request-reference consumers

Two fixtures now match pinned generation.ts245–290: preparation commits model:{provider,modelId}, streamOptions and cutoff; request resolves that reference through runtime.models.getModel. A clean-close resend retains the committed request and uses current process-local model metadata. No runtime code changed in this packet.

| Pinned source | Native assertion scope |
| --- | --- |
| generation.ts request and harness-generation-recovery.test.ts75–125 | TestGenerationRecoveryUsesCurrentModelMetadataAndCommittedStreamOptions replaces the obsolete full-model pinning expectation. Public View/Task records prove request phase/attempt1/reference/timeout/cutoff survive Close/Open, Open does not dispatch and restarts stay0. Changing the API, model maxTokens256→999 and top_p0.5→0.99 selects the current provider; changing host timeout1234→999 cannot alter the saved1234. One original and one replacement dispatch, same user context and recovered answer are required. This metadata-change supplement does not translate every assertion of the original section-rendering recovery case |
| ModelRef/request wire and process-local transport; real pi-ai OpenAI HTTP path | TestM1bLargeLocalModelAuthorizationHTTP retains the600KiB Authorization header, exact model/temperature/max_tokens/top_p body, successful single dispatch, database absence of header/key/URL sentinels and reopened terminal deduplication. Captured public request metadata contains only the compact source reference/options; the terminal has no checkpoint. Model maxTokens supplies the HTTP budget, not an obsolete native agent-setting pin. The separate oversized private clone assertion remains supplemental |

The initial metadata fixture translation incorrectly used host.stream.maxTokens, which ConversationStreamOptions does not export and native generationStreamOptions does not retain. Its0.293s failure is a fixture error, not an implementation defect; it was corrected to the original timeoutMs1234/999 scenario before acceptance. Process-local numeric metadata is compared as exact JSON number values, not an invented json.Number-only contract. Old-provider redispatch reports a test failure without double-closing its signal.

Corrected metadata three-shuffle1.513s; combined metadata/header/close/intent4.928s; expanded HTTP/process-crash/render-settings/queue-mode/abort regression selection29.804s. Whole-tree vet, Staticcheck v0.8.2, logging/diff pass. Unchanged original generation-recovery9 and generation26 pass zero skipped;92 independent probes per command remain separate.

Three compiled semantic controls detected/restored: reread stream options from the replacement host, discard process-local model sampling metadata and add an extra API field to the stored model reference. The last is rejected by the existing canonical JSON schema gate before transport; no new output-comparison defect is claimed. No production mutation remains.

Complete unchanged-tree verbose ./... baseline20: durable862.069s fails60 top-level groups,25 other tested packages pass (tools15.027s/public12.419s), no panic/timeout/exclusions. The two old model fixture names disappear; the already observed uncertain-acknowledgement fixture fails again during Close. Its intermittent cause remains unverified; the observed count is60 rather than an inferred59. [Full inventory](canonical-integration-baseline.md). Private provider protocol/recovery/eligibility consumers, extra Withdraw and all broader API/Storage/readOnly/dependency/identity/corpus/profile acceptance remain open.89c031e remains last fully verified tree.

## Failure placement and guarded constructor reservations

Pinned source: packages/durable/src/session/session.ts guarded Storage mintId path and #call failure latch; test/session-documents.test.ts, test/harness-inbox.test.ts and test/harness-restart-failure.test.ts. Source constructor mutations fail the Session even when the callback catches the originating error. Source failure scenarios use canonical task input/prepare1, current task/result.entryId and deferred submission settlement, not retired private successor checkpoints.

Native mapping:

- harness_failure_queue_main_test.go retains failure lifecycle, own placement and continued-answer clearing using taskId and Generation2/input{} requests.
- harness_failure_reset_main_test.go identifies completed tools by actual result.entryId; retains immediate-successor history/placement and turns.
- harness_failure_boundary_main_test.go uses the actual public outcome assistant entry for attached-input handoff; retains failure/reset outcome and turn assertions.
- harness_failure_preparation_main_test.go captures actual public commit records by sequence with a deterministic waiter; exact input/prepare1, grouped source task and settled source records remain asserted. Native storage taps are supplemental.
- harness_boundary_rollback_main_test.go constructs a real no-model run and queued input, then uses public Continue placement. It retains rejection identity, no adopted leakage, deferred settlement, stale-head and append ordering. No private queued-generation producer remains.
- session_failure_120_test.go adds five public constructor cases, caught and uncaught: submission, entry, conversation, task and document. They retain originating error identity, the callback health fence, failed notification/Closed reason and no published effect. Source-first tests fail all five with an unlatched Session; the same production paths pass after sharing mintIDEntered. Fork conversation/document allocations also share this helper; no separate fork-allocation failure coverage is claimed.

The recurring uncertain-acknowledgement Close failure was real. Direct constructor store.mintID calls poisoned the backend without immediately latching Session failure; Harness requeue could then fail before Close. Every production reservation now passes through the existing guarded mutation logic without re-entering the already-held Tx guard. Temporary diagnostic instrumentation was restored.

Verification: six failure/rollback groups three-shuffle16.660s; expanded three-shuffle41.303s; allocator/acknowledgement30-repeat41.326s (before adding caught constructor variants), then restored five-repeat caught/uncaught selection. Five independent allocator-bypass controls compile and fail for the expected unlatched-failure assertion; all restored. Vet, Staticcheck v0.8.2, logging and diff checks pass.

Independent original-runtime probe adds ten constructor failure/catch scenarios as one Vitest test; probes now93 (69+24). Unchanged originals run separately: session-documents27, harness-inbox37 and harness-restart-failure18, all zero skipped. Original executions and supplemental probes do not establish full native assertion acceptance.

Complete actual-tree baseline21: durable835.764s with53 failed top-level groups;25 other tested packages pass (tools14.923s/public12.659s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

## Single rendered system sections and property order

Pinned source: pi-ai src/utils/text.ts getSystemMessageText/contentText and src/utils/transcript.ts createInitialSystemMessage/getCurrentSystemMessage/getCurrentSystemPrompt; durable src/harness/generation.ts preparation/request, context.ts and prompt.ts. Section values are already rendered: initial projection joins them without adding tags. Preparation reconciles historical sections/tools with the current loadout; a request carries the resulting raw transcript once. JavaScript Object.entries order puts array-index keys first; other property names retain insertion order, with deletion/re-addition moving the name to the end.

Source-first evidence and public mapping:

- entry_context_test.go replaces this HTTP fixture's historical private entry adapter with public AppendTypedEntry drafts. Both configured and unconfigured current loadouts retain the original edited ancestor, stop-reason exclusions, pending text, tools, owned input/reopen and credential non-persistence checks. Public ContextView/Context additionally prove historical sections/tool parameters survive caller mutation before generation. Exact HTTP system equality then exposed the duplicate rendered text, rather than accepting a substring. Native red1.296s.
- transcript_sections_v150_test.go checks plain/pretagged/empty sections, deletion/re-addition, text separators/whitespace, decoded property order across JSON roundtrip, no extra metadata wire property, and numeric-key enumeration within/across patches. Utility red0.027s; order red0.025s; numeric red0.012s. The same values run independently against pinned pi-ai source in the reference probe.
- canonical_generation.go no longer copies replayed sections into the legacy SystemPrompt alongside the same raw system messages. Source request hooks and provider projection see the actual transcript, not a duplicate prefix.
- transcript.go joins rendered values, preserves whitespace and empty text separators, and provides mapped GetSystemMessageText. Message.SectionOrder is representation metadata (json:"-"); message_json.go captures/serializes property order without adding a new source JSON key. Durable message projection carries its existing order into pi-ai messages. Existing durable MessageReceipt.sectionOrder remains a separate unaccepted wire/API obligation; this packet does not close it. Host-built Go maps need explicit SectionOrder for non-lexical string order; decoded source JSON retains it.
- shared response projection uses the core initial/update renderers. Codex removes the obsolete whitespace compensation and duplicate folding implementation; SSE/WebSocket tests retain all old body equality/immutability checks and add ordered sections/re-addition. Existing whitespace tests first caught the compensation double-inserting the now-correctly retained prompt.
- supplemental fake-provider prompt/codemode/instruction/hook/agent-order consumers inspect the production ResolveContext projection, retaining positive, negative and exact prompt checks. Raw HTTP equality prevents this provider projection from hiding a duplicated or incompatible transcript. Two older native auto-tag expectations now compare the pinned raw values exactly. These are not claimed as full upstream test-body ports.

One numeric-key source probe initially omitted required timestamp; the resulting undefined source message was a fixture error, corrected by supplying timestamp0 before acceptance. One initial fault failed to compile and was not counted; its corrected compiled version is included below.

Verification: final expanded durable three-shuffle99.877s; root/Codex/shared three-shuffle pass; all inference/public consumers pass; vet, Staticcheck v0.8.2, logging and diff checks pass. Eleven compiled semantic controls detected/restored: duplicate prefix, implicit tags, lost empty separators, trimmed whitespace, ignored desired sections, missing configured tools, ignored ancestor replacement, lost JSON order on decode, lost order on encode, lost durable-to-pi-ai order, and omitted numeric property enumeration. No mutation remains.

Unchanged originals: harness-prompt9, harness-generation26, conversation-transcript16, zero skipped. Separate probes now94 (70+24), including the single new source section/value/order test. These source executions and supplemental probes do not establish complete native pi-ai/durable assertion/export acceptance. Baseline22 observed56 groups before four additional flat-prompt consumers were corrected; no failures were excluded.

Complete actual-tree baseline23: durable842.839s with52 failed top-level groups;25 other tested packages pass (tools15.114s/public13.570s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

## Environment errors and public metadata/reset consumers

Pinned source: harness-tools.test.ts974–990 (throwing env), harness-input-entries.test.ts (kind/data ownership, queued inbox entries, reserved kinds), harness-inbox.test.ts242–270 (post-tools reset), generation.ts requested thinking-level assignment, harness.ts buildEnv and scheduler.ts runtime.env.

Production correction: environment.go returns the original callback error; preparation alone reports it once and renders without env. Tool execution converts that error into its tool_error diagnostic. An error-valued Go panic retains the same error identity. The old generic environment-callback replacement and unconditional helper report contradicted the source: native red1.799s showed wrong result text and five reports instead of two. Source original tools42 passed before runtime editing. Ordinary public callback errors are source diagnostics, not automatically redacted native host payloads; no real credentials are used by these fixtures.

Public consumer mapping:

- environment_test.go retains zero-effect and late-abort fences, now at a real tool environment await rather than an incidental preparation await. Error/panic lanes assert exact source tool text/data, one result, two model calls and two reports with original cause identity. Close joins report delivery; no polling establishes the count. The native-only secret-redaction assertion was replaced by the incompatible source's explicit public error diagnostic, not counted as a source assertion.
- report_140_test.go preserves host attribution/error identity through explicit public ReportFrom. A caller-handled runtime.Environment error does not automatically report in the source; the fixture checks no duplicate after Close.
- message_protocol_test.go retains all response/signature/empty-presence assertions through live successor context, reopen and the separate codec probe. Generation records requested thinkingLevel (off), while raw contribution conversion retains provider low. Both default/explicit empty registries retain the unoffered call's own flagged public task. Async callback assertions no longer use Fatal/Goexit.
- input_metadata_reopen_120_test.go reads the actual shared inbox with new mapped InboxDoc() token/SnapshotDefinition; verifies exact queued kind/data, caller mutation isolation, no early placement, reserved-kind rejection with unchanged readonly identity/history, and one placed entry after reopen. A private queued-generation metadata/forged-checkpoint assumption is removed: the source stores queued metadata in InboxDoc, not a placeholder task. Exact JSON numbers use a safe source-number representation for the expected ID; no incompatible envelope is normalised.
- reset_test.go uses public submission records, ordered Entries and h.Task. It retains reset settlement, fresh context, exact non-system transcript order, reset self-heads and one old assistant, and checks the canonical completed Generation2 result.entryId against its actual tool-call assistant. Four private BuiltinHold mutation assertions have no source counterpart and are not counted as translated source assertions. Reset handoff user messages are not synthetic assistants.

The independent protocol probe initially supplied unsupported fields to fauxAssistantMessage's narrow options. It was corrected by setting the actual returned AssistantMessage fields before queueing, without changing expectations. It confirms requested-level overwrite, response metadata/signature preservation and a flagged unoffered task against original runtime. Separate probes now95 (71+24).

Verification: three reconciled consumer groups3-shuffle7.206s; environment+consumers12.270s; final expanded environment/task/nested/report/prompt/metadata/reset/canonical/acknowledgement/HTTP three-shuffle84.733s; vet, Staticcheck v0.8.2, logging/diff pass. Seven compiled controls detected/restored: omitted requested-level stamp, omitted queued data, accepted reserved kind, wrong reset result.entryId, replaced environment error text, duplicate helper report, and lost panic cause identity.

Unchanged source runs, zero skipped: tools42, input-entries11, generation26 and inbox37. Their original counts remain separate from95 supplemental probes per command and native assertions. Broad API/Storage/readOnly/schema/recovery/continuation/export acceptance remains open; InboxDoc is one mapped token, not closure of the export ledger.

Complete actual-tree baseline24: durable854.904s with48 failed top-level groups;25 other tested packages pass (tools14.876s/public12.266s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.

## Scoped Tool3 UTF-16 checkpoints and publications

Pinned source: tool.ts call/execute checkpoint, readCall and beforeTool/afterTool paths; source harness-tools42, harness-tools-recovery10 and session-tables12. The independent actual-tool probe supplies isolated UTF-16 keys/values, prepares and rewrites arguments, reads the real execute checkpoint, and verifies afterTool still receives the original model call.

Demonstrated native defect: preparation, both validations and beforeTool completed, but no effect began. The canonical execute checkpoint used an ordinary JSON bag, rejecting the isolated scalar; adding the existing scoped argument domain then exposed the publication's strict decoder rejecting its valid escaped source bytes. A temporary stack trace located preparePublication's typed Write decode. All diagnostic code was restored. This is a runtime defect, not only a private-checkpoint consumer mismatch.

Production mapping:

- canonical_tool.go writes the already-existing sourceToolArguments domain for a streamed model call's validated execute arguments, without a new JSON carrier property. task_checkpoint.go recognises that object when resuming a safe execution.
- canonical_tool_arguments.go qualifies only typed pi.tool version3 records with execute checkpoints, selects the arguments subtree and restores it losslessly from the original bytes before validation/freezing. streaming_arguments_ranges.go grants that exact scanner range; streaming_arguments_json.go restores it through typed table/storage containers. Dynamic ordinary JSON bags gain no permission. TaskRecord.UnmarshalJSON also restores the public record's qualified subtree directly.
- events.go reads the scoped checkpoint arguments instead of dropping the map wrapper, preserving tool_execution_start arguments and listener isolation.

Native assertions: ToolUTF16MainExecutionReopenHooks retains prepare/validate/rewrite/execution counts, close/reopen safe replay and transcript-original ownership. Its afterTool expectation changes from rewritten d801 to original d800, as source readCall/tool.ts requires; beforeTool still sees prepared dfff and Execute sees rewritten d801. ReaderUTF16MainCommittedEventsAndSnapshot retains actual readonly publication identity/ownership, snapshot/listener isolation, exact escaped source JSON and ordinary-JSON rejection. The obsolete raw private Task.Checkpoint read is replaced by the actual public execute record. It additionally checks public json.Unmarshal/decodeStrict roundtrips and negative kind/version/phase/field edits of an actual produced record, never a fabricated executable checkpoint.

Reference probe's first state assertion mistakenly used phase instead of source status; it was corrected before acceptance without normalising the returned record. Separate probes now96 (72+24). The probe is source-derived and supplemental, not an extra unchanged upstream test. Native Tool1/2 isolated-surrogate migration assets and broader generic-string/API/schema authority remain unaccepted; no version-exclusion approval is claimed.

Verification: UTF-16/streaming/public-record/canonical selection three-shuffle18.752s; final expanded canonical/tool/state/environment/metadata/reset/acknowledgement selection53.350s; vet, Staticcheck v0.8.2, logging/diff pass. Six compiled semantic controls detected/restored without timeout/panic acceptance: drop direct public-record restoration, drop typed event arguments, lose scoped replay arguments, and overgrant kind/version/phase permission.

Unchanged original tools42, tools-recovery10, session-tables12 pass zero skipped;96 separate probes per command remain distinct. No complete native source/API/Storage/schema/release acceptance is claimed.

Complete actual-tree baseline25: durable838.361s with46 failed top-level groups;25 other tested packages pass (tools15.606s/public12.785s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree.
