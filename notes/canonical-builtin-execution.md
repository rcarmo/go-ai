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
