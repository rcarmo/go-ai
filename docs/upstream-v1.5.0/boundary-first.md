## Boundary-first and atomic successor -- final bounded receipt

The final read-only task-selection overlay passed the **45-function focused selection with `CGO_ENABLED=0`**, each function run three times with shuffle seed 42: **31.898s, 135 top-level passes, zero failures and zero skips**. Scoped CGO=0 vet passed in **0.164s** and Staticcheck 0.8.2 in **16.585s**. Rui's corrected validation instruction is **pure Go, CGO=0 only**. The overlay and new fail-fast regression were added after the earlier 44-function gates; those earlier CGO=1/race results apply only to their historical source. No CGO=1 or race checks were rerun after the correction, and no race exception was authorised.

This bounded development receipt leaves acceptance and full v1.5.0 alignment open. The original source-derived first-entry regression failed in **six backend leaves**; the atomic-publication regression failed in **12**. The tested runtime is `2072637c915ee4205247f8357d04ec174c782891`, based on `27323f780e64d8a6d5e5e83811c36e9d03643f8c`.

The target remains official upstream v1.5.0 **`f9a26365568127461c47a07a37dcb42c140a3ac5`**. The tests below are source-derived native regressions/invariants, not complete translations of upstream TypeScript test bodies. The earlier [run-failure checkpoint][failure] retains its original scope. No intentional semantic divergence is accepted by this receipt, and an unverified contract is not an accepted divergence.

## Source contracts and current native implementation

The inspected pinned extracts are `source-inbox.ts`, `source-generation.ts`, `source-live.ts` and `source-submissions.ts` under `/workspace/tmp/go-ai/runs/boundary-first/20261102/`. Permanent links below identify the release objects; working extracts and logs are disposable, not permanent acceptance artifacts.

| Release code | Contract | Native mapping and checked scope |
| --- | --- | --- |
| [`prepareBoundary` and `applyBoundary`][inbox] | Read head, inbox and queue modes before the first table write. Place writes first, then selected users; each group follows submission order. Return the first entry actually appended. | [`run_boundary.go`][native-boundary] carries boundary `first` separately from primary input ownership. W-before-B is checked through real generation; ordered reset/stale/numeric-head writes also have native Tx controls. |
| [`startRun`][start] | Set the owner, selected user inputs and optional boundary `first`. A passive write contributes history without joining `run.inputs`. | Source `run.taskId` maps to numeric native `run.task`. B keeps `cp.InputEntry == B.entry` and its own settlement `Entry`, while `live.run.first == W.entry` or the actual reset entry when it landed first. W settles `done` at W, without an answer. |
| [`endRun`][end] | Settle only ending-run inputs and copy its existing `first` into `runFailure`; preserve absence in older runs. Remove ending presentation. | [`recordRunFailure`][native-failure] copies the run boundary, not a task ID or guessed historical entry. The lifetime control removes `first` from an actual run and verifies preparation/abort do not backfill it. |
| [`answer`][answer] | Without a selected continuation or `afterRun` hold, append the answer, apply the final boundary, end A and start the selected successor in one commit. | [`cleanupGeneration`][native-harness] stages successor placement/ownership in the ending transaction. Stored publication and Session-local delivery must each independently contain the complete boundary; later preparation cannot repair it. |
| [`endFailedRun` and `failNoModel`][failed] | End A before applying the automatic continuation boundary; start selected users with `keepFailure: true` in that commit. Missing-model settlement omits detail although the task error and failure record contain it. | A alone fails; queued B/C cannot become A inputs. The no-model case sends no A provider request and invents no A assistant receipt. Successor ownership/placement is checked before its resolver returns. |
| [`admitSubmission`][admit] | Idle admission with queued items runs a final boundary. A fresh input queues behind existing users; an older B may start while fresh C stays queued. An idle input otherwise starts its run at admission. | [`submission.go`][native-submission] places the input and publishes its run before model resolution. Fresh C releases a kept queue but remains unplaced under one-at-a-time selection. Bound admission preserves caller attribution. |

`first` is the first **actual append of this boundary**, not the newest head, first currently visible context entry, or a historical scan result. Native `cp.InputEntry` remains each input's own entry. Queued resets share the ordered write lane and append a real `pi.reset` self-head marker; a later reset can cut an earlier W out of context without changing that boundary's `first`. Each write sees the head changes of preceding writes. A stale numeric-head write settles `unanswered/stale`, appends nothing and cannot supply `first`; a stale-only write boundary returns no `first`, even with an older reset in history. An immediate display-only write outside the boundary likewise supplies no boundary entry. The new tests do not establish every display-only/reset admission combination.

Follow-on blockers exposed while exercising rollback, bound attribution and pre-model abort were corrected, without inferring placement from history:

* Passive-write classification/placement detaches `Submission.Value` before adding `placedEntry` or `errorCode`. A rejected candidate must not leak either field into Session/storage state; only real ID reservations may survive rejection.
* Bound admission supplies transaction task/conversation attribution. The bound child's actual user entry retains the caller's `ByTask`; its later answer belongs to the distinct generation.
* Both queued-user selectors, `startQueuedRunBoundary` and [`applyQueuedInputs`][native-inbox], guard inherited cancellation and native fail-fast selection as well as direct marks, decided outcomes and other existing eligibility fences. [`boundarySelection`][native-boundary] supplies a private, read-only task-table overlay containing A's locally decided outcome before the caller stages its authoritative task write. Both selectors use that view for cancellation eligibility; the candidate/adopted tables, write budget, task timestamps, restart counters and replacement validation stay unchanged.
* Queue-mode resolution uses host settings when optional `pi.agent` is absent, rather than treating an empty checkpoint agent as an override. The explicit absent-agent control checks host `all/all` and does not create that optional document.
* Busy detection still includes actual legacy reserved/running preparation with no `live.run`, even when its checkpoint phase is `queued`. An already answered, decided predecessor held by children does not by itself make admission busy.
* A checkpoint can still say `queued` after its input is **placed**. Scoped abort withdrawal requires an unplaced submission and `cp.InputEntry == 0`; an admitted run is marked/joined and settled by run cleanup, not withdrawn as queued. The blocked valid model resolver must return before abort joins, and cannot resurrect intent or dispatch afterward.

Failure history belongs to the failed run and its immediate automatic successor. `keepFailure: true` preserves A failure while B runs in the ending commit; B answering and starting C clears it. A later fresh admission also clears it, including when it releases B from a kept queue. The earlier [failure lifetime controls][failure] retain their longer A/B/C scope.

## Exact new boundary-test function map

These are the **11 actual `TestMainBoundary*` functions**. They are source-derived native tests; the count does not measure upstream test-body ports. File links are under `durable/`.

| Function | Actual scenarios/assertions and fixture limits |
| --- | --- |
| [`TestMainBoundaryFirstIncludesPassiveWrite`][first-test] | `answerA`, `keptfailureA`: queue B before model-contributing W; require actual provider users `A,W,B`, persisted `(Seq,Position)` order W before B, B-only ownership, B checkpoint/settlement at B and active/failure `first` at W. W remains done after B fails. Fresh C releases the kept queue but stays queued/unplaced; A history clears at that later admission. |
| [`TestMainBoundaryAtomicSuccessorPublication`][atomic-test] | `answer/all`, `answer/one-at-a-time`, `model_error/one-at-a-time`, `no_model/one-at-a-time`: queue B, C, W while A is gated. In A-ending stored **and** local delivery require A settlement, answer/error receipt when applicable, W placement/settlement, selected B/C placement, successor task/checkpoint/run, failure history and inbox removal. All groups B+C; one-at-a-time leaves C untouched/unplaced. No-model sends only B's provider request and no synthetic A assistant. |
| [`TestMainBoundaryLifetimeBeforeModelResolution`][lifetime-test] | `conversation-abort`, `task-abort`, `legacy-first-absent`: real idle admission already owns its placed input/run before pinned model, intent, attempt presentation or provider dispatch. A deliberately blocked valid resolver witnesses mark/join, sealed old authority, no early public abort completion, unanswered/aborted settlement at the original entry and no synthetic transcript/usage. The legacy lane explicitly removes only `first` from that actual run and checks continued absence. |
| [`TestMainBoundaryLegacyRecoveryBusyControl`][lifetime-test] | Seed a valid native queued generation/inbox on the real Session before Resume (not a fabricated model completion). Gate actual reserved/running preparation with no live run: reject-mode admission is busy; default B queues without stealing A's reservation. Release A; preparation appends its real first input, then A and B complete independently without `invalid_preparation`. |
| [`TestMainBoundaryOwnedInvocationIdleChildAdmission`][owned-test] | Real task-owned child conversation and bound Submit/Wait through a gated HTTP provider. The admitted input is immediately placed and attributed to the caller; the child has its own generation/run, whose answer attribution must not overwrite the caller's input attribution. |
| [`TestMainBoundaryOwnedAfterResponseHeldAdmission`][owned-test] | `reject`, `input`, `passive-write`, `reset`: A has answered and ended its live run but remains completing behind a real unfinished child created by `AfterResponse`. Fresh admission is not busy: B gets a distinct placed run, or W/reset lands without a provider run. Late A finalization must not repeat the boundary, mutate the later run or change A's answer. This is **not** upstream `afterRun` hook equivalence. |
| [`TestMainBoundaryPassiveWriteRollback`][rollback-test] | `append`, `stale-head`: public Submit queues W; scheduling stays disabled. Explicitly release the actual live run, then reject a candidate **after** real placement or stale classification. Both Session and storage retain the pre-candidate tables/documents/inbox/submission; only allocator high-water may advance. Positive retry adopts exactly once. Native Tx rollback control, not automatic generation completion/publication. |
| [`TestMainBoundaryEligibilityInheritedCancellationBeforePlacement`][eligibility-test] | `start`, `initial-follow-up`, `post-tools-steer`, each with inherited-abort and unmarked-positive controls. Real Open/Submit creates inputs/checkpoints/inbox; scheduling is sealed before invocation. Explicitly clear A's run and optionally mark its real conversation owner. Probe/reject each selector candidate: cancellation leaves B unplaced/unconsumed; the positive control places B and preserves checkpoint/run ownership as applicable. Does not test automatic abort cascade/withdrawal or scheduler timing. |
| [`TestMainBoundaryEligibilityLocallyDecidedFailFast`][failfast-test] | `final/failFast`, `held/failFast` and matching `allSettled` controls: a real native waiter waits on exactly `[A,C]`, with B outside its ownership. A fails at the real provider while B/C are genuinely queued under `AfterFailedRun: "continue"`, `FollowUpMode: "all"`. In A's actual deciding commit, B starts; fail-fast leaves C untouched/unplaced in its inbox slot, while all-settled attaches/places C in B's run. Check the same-commit entries, tasks, submissions, run/checkpoint and actual successor request (`A,B` versus `A,B,C`). Only `held` adds a real child blocked on A's cancellation. Fixture adaptation assigns the real legacy A/C task rows to the waiter before the native wait; it seeds no outcome, abort, execution envelope, live document or checkpoint. |
| [`TestMainBoundaryEligibilityAbsentAgentHostAllModes`][eligibility-test] | Plain-Session root without `pi.agent`, host `all/all`, genuinely queued B/C follow-ups and S/T steers. A rejected early-boundary candidate must place all four in order, keep distinct input ownership, install exactly B's grouped run, empty the inbox and still omit `pi.agent`. Explicit native invariant fixture, not a model-driven mixed-mode end-to-end port. |
| [`TestMainBoundaryWritesFirstAndSequentialHeads`][writes-test] | `write-reset-stale-write`, `stale-only`, `numeric-head-progression`: real minted entries/submissions and native inbox shapes, with scheduling disabled. Check exact append order/count, first actual append, reset self-head, stale settlement without entry, sequential numeric-head validity, final active head/context and empty inbox in one adoption. Queue data is explicitly seeded; this does not establish model-completion timing. |

The first/atomic functions use real Harness admission, generation and provider paths; their snapshot helpers do not seed live documents, checkpoints, inboxes or settlements. The final CGO=0 checks use memory/journal through `utf16Backends`; SQLite coverage belongs to earlier CGO=1 checks. The two owned-task functions use `backends` (memory/journal). The table identifies explicit native invariant/legacy fixtures and the fail-fast ownership adaptation.

The atomic function uses [`mainPreparationTerminalPublication`][publication] and `SubscribeCommits` to capture the actual terminal publication, rejecting resnapshots. `mainAtomicLocalDecision` installs a white-box, read-only tap in the actual Session-local delivery list because `followSessionCommits` rejects scheduler-backed sessions. Neither callback edits durable state or attaches later writes. Each delivery must independently contain the whole boundary; a later snapshot supplies entry identities/values only. This does not establish a public scheduler-backed listener API or general callback timing/re-entrancy equivalence.

### Existing assertions adapted to the earlier boundary, not weakened

* [`TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement`][publication] now checks B's placed entry/run in **A's actual deciding publication**, before B's resolver returns. B's later no-model settlement must reuse that entry/owner and append no duplicate input.
* [`TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor`][publication] still checks both queue modes, exact A failure without absorbing B/C, successor inputs/entries, retained immediate failure, later clearing and uniform settlements. `mainRequirePreparationStart` asserts the exact grouped run, task/checkpoint and unique placements in the earlier deciding publication plus gated snapshot; `mainRequirePreparationInputSettlement` checks terminal reuse. Assertions formerly tied to later no-model preparation are relocated to the atomic start, not dropped to permit an empty-run gap.
* [`TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor`][reset-test] now requires B's `run.first` to name the **actual reset entry**, in the same sequence and before B's own input position; B's checkpoint/settlement still name B's entry. A's reset failure remains exact, including absent detail, and C still clears it. `AfterFailedRun: "keep"` remains intentional: reset succession must not depend on failure-policy continue.

## Pre-fix baseline and development probes -- 2 November 2026

The supplied original baseline has six first-entry failures and 12 atomic failures. Original raw per-leaf logs were unavailable in the inspected run directory; later `probe*.log` files are separate evidence. Sequence numbers below are fixture-local observations.

| Unchanged-runtime selection | Observed failure |
| --- | --- |
| First-entry function: **six failing leaves**, two scenarios times three backends | `answerA` already placed W before B but used the wrong active/failure `first`. `keptfailureA` sent provider users `A,B,W`, persisted W after B by `(Seq,Position)`, and used B as `first`. |
| Atomic function: **12 failing leaves**, four scenarios times three backends | A-ending publication contained no live successor. Answer/model-error fixtures ended A at sequence **9**, then placed B at **11**; no-model fixtures used **7** and **9**. A later B state cannot repair that publication. |

These demonstrate two contracts: placement order/history boundary and atomic publication. A correct later provider request alone proves neither same-commit publication nor the complete upstream test corpus.

| Inspected working log | Result and limit |
| --- | --- |
| `probe.log` | Admission setup failure, `durable: rejected: submission unavailable`, 0.307s; no behavioural/fault-rejection credit. |
| `probe2.log` | Intermediate production change, 1.440s: all 12 atomic leaves now had a successor in stored/local A-ending delivery but `first` was B rather than W; all six first-entry leaves still failed `first`. Not the unchanged-runtime baseline. |
| `probe3.log` | Initial focused pass, **1.373s**. No command/flags/toolchain/revision/per-leaf list in that log; not final-source validation. |
| `probe4.log` | Lifetime fixture rejected the actual native user-entry shape: the user receipt is in `Value`, not necessarily an `Entry.Model` array. The assertion was corrected to decode the real receipt, retaining placement/ownership checks; this probe is not a new runtime/fault rejection. |
| `probe5.log`, `affected-probe.log` | Development passes, **1.733s** and **11.150s** respectively; bare package summaries do not replace the exact current selection/results below. |

The original deliberate-fault receipt records **five paired mutants, rejected in all 10 memory leaves**. Missing per-mutant logs or identities have not been reconstructed. These results cover memory leaves only.

### Locally decided fail-fast regression and sixth fault

The [new reproducer][failfast-test] exercises both final and held A outcomes through real provider failure, native waiter admission and automatic boundary publication. Before A decides, B/C have real queued submissions, unplaced checkpoints and inbox slots, with no failure or abort mark. The waiter's actual `failFast` policy selects `[A,C]`; B can start while C must stay unprocessed in A's deciding batch. Matching `allSettled` controls must attach C, so suppressing the entire successor boundary cannot pass.

An initial attempt staged A's outcome early. It caused a terminal-task replacement rejection and a context deadline; those failures receive no regression or fault-rejection credit. That attempt was reverted. The final fix passes the locally decided task only through the read-only selection overlay, leaving the caller's single authoritative task write in place.

The paired memory baseline passed **all four leaves** (`final`/`held` times `failFast`/`allSettled`). A sixth deliberate fault changed `boundarySelection` to return the original `state`. **Both fail-fast leaves failed; both all-settled controls passed.** The rejected mutant processed C in the exact same A-deciding commit, before reconciliation could hide the placement. This adds **two rejections** to the original five-fault/10-leaf result. The test uses no synthetic failure metadata or weakened assertions.

## Final CGO=0 validation results

The exact selection is **45 top-level functions: the 11 mapped above plus these 34 retained controls**. The final normal run uses `-count=3 -shuffle=42` and records **135 top-level passes, zero failures and zero skips**. The earlier 44-function selection predates the final selection overlay and new regression.

```text
TestFailedRun140InboxPolicies
TestFailedRun140NoModelLeavesWritesQueued
TestGenerationAbortConvertsOnlyCommittedPartialAndNoSyntheticAssistant
TestGenerationToolHandoffCommittedReopenNoToolReplayAndTerminalSubmissionTask
TestGenerationModelFailurePreservesProviderTextReasonAndReceipt
TestUnconfiguredConversationNoModelFailsWithoutSyntheticAssistantAndCanConfigure
TestMainRunFailureOrdinaryAbortProjection
TestMainRunFailureAbortHandlerCutoffProjectionDerived
TestMainRunFailureAttachedInputsWithoutQueuedSuccessor
TestMainRunFailureNoModelOwnershipFence
TestMainRunFailureToolHandoffOwnershipFence
TestMainRunFailureNoModelBeforeRequest
TestMainRunFailureNoModelReplacesPreviousModelError
TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement
TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor
TestMainRunFailureContinueQueueClearsFailureAfterAnswer
TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor
TestMainRunFailureContinuationAndReopen
TestOnYieldContinuationUsesSameRunButQueuedUserWins
TestBusyPassiveWriteQueuesUntilAnswerBoundaryBeforeFollowUp
TestForegroundSubagentOwnedConversationAndAnswer
TestResetIdleAndPostToolsStartsFollowUpInNewContext
TestResetAtFinalBoundaryKeepsAnswerThenCutsContext
TestBackgroundSubagentIdleBoundaryAndReopenDedup
TestSubmissionIdleAdmissionPlacesEntryAtomicallyAndPublicLifecycle
TestSubmissionReferenceStatusAndAbortDispositionScopeQueuedWrites
TestM1bRequestDedupQueueAndWrite
TestTaskOwnershipPinnedHeldFailureAndSchedulerFaultDrainBeforeParent
TestTaskRecoveryGenerationHeldReceiptFinalCleanup
TestTaskRecoveryGenerationPreparationFallbackWakeSequence
TestTaskRecoveryOfflineGenerationPreparationFallbackWakeSequence
TestTaskRecoveryBuiltinPredispatchAncestorCancellation
TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts
TestTaskSchedulerBoundWriteAttributionAndEscapedSettlementSeal
```

Validation uses Go **1.27.3 linux/amd64**, `GOMAXPROCS=4`, `GOTOOLCHAIN=go1.27.3` for the focused test gate; static checks used the installed Go1.27.3 binary, with offline dependency resolution and the project environment. **Runtime commit: `2072637c915ee4205247f8357d04ec174c782891`.** The final 45-function pure-Go gate covers the read-only selection change and fail-fast test. Earlier 44-function and split-race results retain their pre-delta scope; they do not prove a race pass for this final source.

The recorded normal test command uses `/workspace/tmp/go-ai/runs/boundary-first/20261102/overlay.json`. This compiler overlay replaces only the unrelated `durable/continuation_prototype_main_test.go` with `package durable`, for both workspace aliases; it is separate from the runtime's read-only task-selection overlay. Final Staticcheck/vet use an exact physical copy scoped to `durable`, excluding that prototype and including the changed source, new test assets and imports. The test selection and static checks cover this bounded scope. Recorded normal command shape (`RUN` is the directory above; `go` is the stated toolchain):

```sh
CGO_ENABLED=0 GOMAXPROCS=4 go test -mod=readonly -vet=off -overlay="$RUN/overlay.json" -json -count=3 -shuffle=42 -timeout=240s -run="$(head -n 1 "$RUN/focus.txt")" ./durable
```

The results below were supplied for this correction; this documentation task executed none of these checks.

| Gate | Actual result and scope |
| --- | --- |
| Normal, CGO=0 | **PASS, 31.898s**; exact 45-function selection, `-count=3 -shuffle=42`, **135 top-level passes, zero failures and zero skips**. Memory/journal only. |
| Vet, CGO=0 | **PASS, 0.164s**; exact physical-copy scope described above, prototype excluded. |
| Staticcheck 0.8.2, CGO=0 | **PASS, 16.585s**; same exact physical-copy scope, prototype excluded. |
| Formatting | **All 17 changed Go files are gofmt-clean.** Four unrelated baseline formatting findings remain unedited. |
| Module verification | **Earlier PASS retained**; module manifests are unchanged. No new module verification run. |
| Original deliberate faults | **Five paired faults rejected in all 10 memory leaves**. |
| Sixth deliberate fault | `boundarySelection` returns `state`: **two fail-fast leaves rejected, two all-settled controls passed**, against the passing four-leaf memory baseline. |

### Earlier 44-function gates

Before the final read-only selection overlay and fail-fast test, the 44-function normal selection passed with `-count=3 -shuffle=42`: **CGO=1, 49.301s; CGO=0, 38.819s** (132 top-level passes for CGO=0). Disjoint 22-function race halves passed in **143.477s** and **97.276s**, each with `-count=3 -shuffle=7` and 66 top-level passes; their union covered those earlier 44 functions. An earlier full-selection race attempt timed out after 113 top-level passes, with no completed package and no pass credit. Scoped Staticcheck/vet also passed on that earlier source after unused `placeQueuedResets` was removed.

Those checks retain their original source scope. The final overlay and 45-function selection have only the CGO=0 checks above. Rui's correction authorised no CGO=1 rerun or exception for the CGO-dependent race detector.

## Contracts outside this receipt

* Explicit source `continue` places queued users and includes the continue submission among run inputs, while appending no entry for the continue itself. It uses `live.runFailure?.first ?? first`; ordinary `startRun` clears failure. Automatic continuation starts at its newly placed boundary. The mapped boundary functions do not establish explicit-continue inherited-history parity; included existing continuation controls retain only their actual asserted scope.
* Source [`holdRun` and `endAnsweredRun`][after] split an answered run across `afterRun` preparation/drain. Commit A settles inputs `done`, retains `live.run` with empty inputs and leaves the queue untouched; commit B applies the final boundary and starts the successor. `RunSummary.first` uses the stored boundary, falling back to the answer for older runs. The new atomic answer fixture selects no `afterRun` hook. Native `AfterResponse` held-child admission is not proof of this split, summary range or full hook equivalence.
* Source mixed selection places every non-stale write first, then selected steers and final-boundary follow-ups together in submission order, using each mode independently. Absent-agent `all/all` and ordered write/reset/stale Tx controls now cover narrow additional invariants, not every mixed mode/reset permutation through automatic generation. General post-tools reset promotion, tool handoff/termination combinations, callback-time arrivals and source `onYield` continuation priority still need their own complete crosswalk/controls. Source `onYield` preserves the run through handover and is suppressed by queued users or a reset; selected existing controls are not whole-contract proof.
* Inherited-cancellation controls use deliberately rejected, sealed-scheduler candidates. The new locally decided fail-fast regression checks actual automatic A-deciding publication in final/held cases; general abort-cascade timing and recovery still need separate tests. Rollback uses an explicit boundary and allows only nontransactional ID reservations to survive; other storage-fault recovery paths are unverified. Legacy fixtures seed native compatibility state and do not port upstream persisted-state migration tests.

Complete TypeScript-body ports, whole-file restart-failure parity, full-suite and repository-wide static/race checks, hosted CI, pre-release profiling and full v1.5.0 convergence are outside these results. Release acceptance is open. None of the listed limitations has Rui's acceptance as an intentional divergence.

## Receipt scope and disposal

The final CGO=0 results cover the 45-function selection and exclusions above at runtime `2072637c915ee4205247f8357d04ec174c782891`. Broader acceptance requires the [project gates][agents] under Rui's current validation instruction and CPU, `alloc_space` and `alloc_objects` analysis/tuning at pre-release. These focused checks include no profiling.

Used raw logs, captures, matching binaries and temporary fixtures must be disposed of after analysis at safe completed-job boundaries, retaining only concise conclusions. This documentation task neither verifies disposal nor removes active or completed artifacts.

Only this document was edited for this correction. No tests, static checks, cache/cleanup commands, Git operations, release actions or agent contact were performed by the documentation task.

[failure]: run-failure.md
[inbox]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/inbox.ts#L34-L125
[start]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L1157-L1175
[end]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/live.ts#L285-L318
[answer]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L851-L892
[failed]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L598-L654
[admit]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/submissions.ts#L264-L321
[after]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L899-L969
[native-boundary]: ../../durable/run_boundary.go
[native-failure]: ../../durable/run_failure.go
[native-harness]: ../../durable/harness.go
[native-submission]: ../../durable/submission.go
[native-inbox]: ../../durable/inbox.go
[first-test]: ../../durable/harness_boundary_first_main_test.go
[atomic-test]: ../../durable/harness_boundary_atomic_main_test.go
[lifetime-test]: ../../durable/harness_boundary_lifetime_main_test.go
[owned-test]: ../../durable/harness_boundary_owned_main_test.go
[rollback-test]: ../../durable/harness_boundary_rollback_main_test.go
[eligibility-test]: ../../durable/harness_boundary_eligibility_main_test.go
[failfast-test]: ../../durable/harness_boundary_eligibility_main_test.go#L372
[writes-test]: ../../durable/harness_boundary_writes_main_test.go
[publication]: ../../durable/harness_failure_preparation_main_test.go
[reset-test]: ../../durable/harness_failure_reset_main_test.go
[agents]: ../../AGENTS.md
