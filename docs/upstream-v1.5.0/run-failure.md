## Run failure projection -- scoped checkpoint

Runtime `6fb7c7577c7d4a7d0ae575cc14da3436fe29c768` implements this bounded failure-record slice against official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. The 29-function checks and subsequent 11-function no-model grouping delta passed on 2 November 2026. Their separate scopes are recorded below. Whole-file restart-failure parity and full v1.5.0 alignment are unverified.

## Failure identity and lifetime

Upstream [`live.ts` `endRun`][live] records `{taskId, inputs, reason, detail?, first?}` while settling every attached input and removing the ending run's presentation. `taskId` identifies the generation that owns the run **at its end**, including after a tool handoff. `first` is the source run/context boundary entry ID, never a timestamp and not universally the primary input's entry. An older run without `first` keeps that field absent; neither projection nor handoff should manufacture a boundary. A run that already answered and only waits for `afterRun` tasks records no new failure on termination; native equivalence for that case is unverified here.

A newly applied source boundary can place passive writes before selected users, so its `first` may identify a passive-write entry. Native [`startLiveRun`][projection] currently derives a new run's `first` from `cp.InputEntry`. The own-input assertions below cover their fixture shapes, not universal boundary equivalence: **passive-write-first parity remains open**. Post-tool steers joining an existing run preserve its original `first` (or its absence), rather than moving the boundary to a newly attached steer.

Native [`recordRunFailure` and `startLiveRun`][projection] use numeric IDs and `run.task`; the failure object uses source `taskId`. The native `failedRun: true` field is an extra queue gate for kept work. It is separate from failure history: `mainRequireRunFailure` compares the entire live value and explicitly includes the gate when expected.

Upstream [`endFailedRun`][end-failed] starts queued work with `keepFailure: true`; ordinary [`startRun`][start] clears the previous failure. The native implementation pins `FailureCut` to **one exact immediate successor**, excluding already attached inputs. If A fails, queued B retains A's failure while running; once B answers, queued C clears it even if C was queued before A failed. With no successor, later fresh admission clears the failure. Another failure replaces the record; reopen preserves it without redispatch.

A [post-tools reset][reset-source] settles the old run `unanswered/reset` without detail and retains its failure for its immediate successor independently of `afterFailedRun`. The tool-calling task can finish `completed` while its run ends unanswered. [`handoffToolGeneration`][handoff-code] transfers the run to its new generation owner while preserving all attached input IDs and the existing `first`, including its absence.

## Preparation, settlement and abort

Upstream [`failNoModel`][no-model-source] puts the missing-model message in `runFailure.detail` and the task error, but settles inputs `unanswered/no_model` **without settlement detail**. Native model resolution can fail before `prepareRequest` establishes `live.run`. [`finishGenerationNoModel`][no-model-code] places the actual user input when needed, establishes ownership from that entry and attached inputs, then ends the run without calling the provider or inventing an assistant answer.

[`cleanupGeneration`][cleanup] reads `tx.current()` after placement/steering, so settlement retains entry IDs staged in the same transaction. It gives attached inputs the same terminal status, reason, detail and answer while preserving each input's own placed entry. For `model_error`, settlement detail uses the selected task error message. The attached-input boundary test now checks the exact reason and detail for every input: `model_error` carries `attached failure`; reset detail is absent.

The queued no-model regression checks the actual terminal commit publication: the same decision appends the follow-up's user entry and retains that entry in its settlement. The final source-grouping correction removes `applyQueuedInputs` from [`finishGenerationNoModel`][no-model-code]: dying A ends with only its already attached inputs, before the failed-run boundary selects queued B/C. In `TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor`, A alone fails `no_model`, without settlement detail/answer; its exact terminal publication neither places nor settles B/C. Both remain queued and unplaced at the successor's resolver gate, and `FailureCut` reserves exactly B. In `all` mode, actual provider preparation groups B+C into one successor with distinct input entries and a shared successful answer. In `one-at-a-time` mode, B then C run through the actual provider with separate owners, entries and answers. Only B (the B+C run in `all` mode) retains A's failure; C's separate run clears it. There is no A+B failure retained during C. These assertions are inspected in the updated test; delta gate results are pending, not supplied by the earlier 29-function passes.

Ownership controls reject a foreign `live.run` during no-model completion and tool handoff without adopting partial writes. Their competing state is seeded through real Session commits with scheduling disabled.

Ordinary public abort settles the run `unanswered/aborted` without detail; an error-bearing partial does not supply abort detail. An abort-handler restart cutoff has different task and run outcomes: the task is `faulted` and reports `task_faulted` with `restart_limit`, while the run stays `aborted` and includes the cutoff diagnostic. The seeded cutoff control copies the actual committed partial into legacy `cp.Partial`, then requires no assistant conversion, unchanged `pi.usage` and no redispatch. That receipt's `Usage` is currently nil, so the unchanged-usage check does not exercise discarding non-zero legacy usage. The control does not reproduce upstream's two crashes during a hanging deferred cancellation.

## Native assertion map

These functions are in the earlier corrected selection below; the updated no-model grouping assertions await delta gate results. Source-derived lifecycle and seeded ownership controls are separate from translated assertions in `harness-restart-failure.test.ts`.

| File in `durable/` and function | Assertion scope |
| --- | --- |
| [`failed_run_140_test.go`][policies] -- `TestFailedRun140InboxPolicies` | Default/keep/withdraw/continue policy settlement, exact failure plus explicit queue gate, and fresh clearing. |
| [`harness_interruption_budget_main_test.go`][budget] -- `TestMainInterruptedBudgetAndCloseAccounting/exhaust` | Source204: exact interrupted failure, task outcome, both partials and no third request. |
| [`harness_interruption_poll_main_test.go`][poll] -- `TestMainInterruptedPollRetryEdges/disabled` | Source554: exact interrupted failure and one cancellation without refetch/resubmit. |
| [`harness_run_failure_main_test.go`][failure] -- `TestMainRunFailureContinuationAndReopen` | Retention during automatic continuation, replacement, detached snapshots, reopen without redispatch and fresh clearing. |
| [`harness_failure_queue_main_test.go`][queue] -- `TestMainRunFailureContinueQueueClearsFailureAfterAnswer` | A/B/C provider gates; only B retains A's failure; distinct persisted entry boundaries. |
| [`harness_failure_reset_main_test.go`][reset] -- `TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor` | Real tool/reset path, absent detail, completed task, reset provider context and B-only retention. |
| [`harness_failure_preparation_main_test.go`][preparation] -- `TestMainRunFailureNoModelBeforeRequest` | Unconfigured/unavailable model across keep/withdraw/continue; exact failure, absent settlement detail, zero provider calls and policy-dependent passive-write placement. |
| Same file -- `TestMainRunFailureNoModelReplacesPreviousModelError` | Fresh no-model failure replaces stale model-error identity and detail. |
| Same file -- `TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement` | Real successful run followed by an unplaced queued input; exact terminal publication appends its own entry and retains it in the no-model settlement. |
| Same file -- `TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor` | Updated source boundary: A alone fails no-model, with no B/C placement or settlement in its decision; exact B successor token. Actual provider groups B+C in `all`, or B then C separately in `one-at-a-time`; distinct input entries, successful settlements/answers and A's failure retained only during B's run. Delta gates pending. |
| [`harness_failure_ownership_main_test.go`][ownership] -- `TestMainRunFailureNoModelOwnershipFence` | Foreign placed/unplaced runs reject without Session/storage adoption; owned/absent-run positive controls finish their own input. |
| Same file -- `TestMainRunFailureToolHandoffOwnershipFence` | Foreign run rejects after staging successor and predecessor completion; no partial adoption. Owned positive control transfers run/first in one commit without duplicate receipts. |
| [`harness_failure_abort_main_test.go`][abort] -- `TestMainRunFailureOrdinaryAbortProjection` | Public abort with/without committed partial; absent failure detail. Error-bearing partial uses production `commitPartial`; stream-adapter error-text preservation is unverified. |
| Same file -- `TestMainRunFailureAbortHandlerCutoffProjectionDerived` | Real request ownership followed by seeded abort/restart state; faulted task/report versus aborted run, actual partial copied to legacy `cp.Partial`, no assistant conversion, unchanged usage and no redispatch; nil-usage limit above. |
| [`harness_failure_boundary_main_test.go`][boundary] -- `TestMainRunFailureAttachedInputsWithoutQueuedSuccessor` | Real tool handoff; ending owner, primary plus two steers, exact reason/detail for all inputs and exact failure IDs/first, no reserved successor and fresh in-progress clearing for model-error/reset. |
| [`generation_handoff_test.go`][handoff-test] -- `TestGenerationToolHandoffCommittedReopenNoToolReplayAndTerminalSubmissionTask` | Actual first-entry preservation, successor settlement and reopen without tool replay. No dedicated legacy-absent-first assertion was identified. |
| [`harness_retry_tool_wait_main_test.go`][tool-wait] -- `TestMainHarnessRetrySleepAndToolWaitDoNotChargeGenerationRestarts` | Source275: persisted provider-503 retry sleep, crash/reopen with zero restarts before Resume, real tool wait, second crash/reopen with zero restarts, original submission done. |

## Historical baselines

The inspected original `baseline.log` recorded **14 missing-record failures** in 1.230s: eight policy leaves (four policies times memory/journal), three budget-exhaustion leaves and three disabled-poll leaves (memory/journal/SQLite). The observed live values were empty or contained only `failedRun`; every expected value also required `runFailure`. These are historical pre-fix counts.

The original reported baseline cardinalities are preserved below:

| Baseline fixture | Backend leaves |
| --- | --- |
| Queue immediate-successor regression | 3 |
| Post-tools reset regression | 3 |
| No-model before request | 18: two model cases times three policies times three backends |
| No-model replacing model error | 3 |
| Seeded abort-handler cutoff | 2: memory/journal |
| Source275 unchanged-runtime control | 3 reported passing leaves |

The delegated per-leaf output was unavailable during the preliminary draft; the table preserves fixture cardinality without assigning additional execution outcomes. Source275's unchanged-runtime pass took 0.590s; the preliminary queue/no-model/reset/abort/continuation slice passed in 1.873s. Both results retain their original scope. `utf16Backends` adds SQLite only with CGO; the ordinary `backends` helper uses memory/journal.

## Earlier corrected focused validation -- 2 November 2026

Go 1.27.3, `GOMAXPROCS=4`, protected continuation prototype excluded. **All results in this table precede the final no-model grouping runtime/test delta and do not certify the latest source:**

| Gate | Result |
| --- | --- |
| Corrected 29 functions, normal `-count=3 -shuffle=42`, CGO1 | Pass, 34.404s |
| Same selection and flags, CGO0 | Pass, 23.850s |
| Same selection, race `-count=3 -shuffle=7`, CGO1 | Pass, 183.483s; no race-detector report |
| `go vet ./durable`, both CGO modes | Pass |
| Pinned Staticcheck v0.8.2, both CGO modes | Pass on an exact physical source copy including the new files, excluding the prototype |
| Module verification, scoped `gofmt` and diff checks | Pass |

### Final no-model grouping delta

The only later runtime change removes attachment of queued users to the dying no-model run; its regression now observes A alone ending before B/C start. The final **11 affected functions** passed normal `-count=3 -shuffle=42`: **10.532s CGO1 / 9.068s CGO0**. Race `-count=3 -shuffle=7` passed in **68.021s**, without race diagnostics. Durable vet and Staticcheck v0.8.2 passed both CGO modes on the final source; Staticcheck again used an exact physical copy excluding the prototype. Source-drift checks were clean. These delta results complete focused development verification for runtime `6fb7c7577c7d4a7d0ae575cc14da3436fe29c768`; they are not a full-suite or acceptance result.

The earlier corrected selection was verified against `/workspace/tmp/go-ai/runs/run-failure/20261102/selected.txt`; it contains 29 actual functions. The same names do not establish that their latest bodies were tested. An earlier smaller regex included six nonexistent names. Those names receive no execution credit, and that earlier run is superseded by the corrected scope above.

An earlier larger race/count3 run failed stored-prefix fixture preconditions without a race-detector report. `f.partial` returns after a memory watch, before the host may arm the persistence timer. A one-shot timer scan could therefore miss the actual arm and sample storage too early. The fixture now waits for the real mutex-protected `armComplete`, steps actual captured deadlines and drains the fired persistence work through foreground admission. The stored-prefix and failure assertions are unchanged; no arbitrary sleep was added. The isolated pre-fix race run did not reproduce the failure. The earlier corrected race result above includes the repaired fixture, but predates the final no-model grouping delta.

Earlier corrected selection (29 functions; not a latest-source gate receipt):

```text
TestFailedRun140InboxPolicies
TestFailedRun140BoundedWithdrawalRecovery
TestFailedRun140NoModelLeavesWritesQueued
TestFailedRun140SettingsValidation
TestFailedRun140WithdrawalJoinsOwnedHosts
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
TestMainInterruptedBudgetAndCloseAccounting
TestMainInterruptedPollRetryEdges
TestMainHarnessRetrySleepAndToolWaitDoNotChargeGenerationRestarts
TestMainRunFailureContinuationAndReopen
TestOnYieldContinuationUsesSameRunButQueuedUserWins
TestPostToolsRefreshesAgentLoadoutAndPromptWithoutDuplicatingInput
TestResetIdleAndPostToolsStartsFollowUpInNewContext
TestResetAtFinalBoundaryKeepsAnswerThenCutsContext
TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission
```

## Mutation checks

Paired memory-backend controls rejected eight distinct faults. The original five were rerun: omit the failure record; fabricate `first` from the task ID; always clear immediate-continuation history; never clear fresh-run history; and drop `first` during handoff. Three additional faults were rejected: skip no-model run establishment (both missing-record and stale-record checks), convert the legacy partial at abort-handler cutoff, and bypass the foreign-run no-model ownership guard. These remain eight distinct rejected faults across nine assertion rejections, not a fresh rerun after the final no-model grouping delta.

A ninth meaningful fault replaces the selected immediate-successor token with the maximum submission ID in the conversation, leaving the exact token comparator unchanged. The paired `TestMainRunFailureContinueQueueClearsFailureAfterAnswer/memory` baseline passed; the mutant executed that same single memory leaf and failed the B-in-progress retention assertion (`runFailure` was absent). This adds one assertion rejection: **nine distinct rejected faults, ten assertion rejections in total**. The startup/toolchain probe executed no tests and receives no credit. Activation credit rests on the isolated source change and paired behavioural contrast, not an unobserved compiler trace. The legacy-partial guard receipt was reused, not freshly rerun. Compilation/setup failures receive no credit; this narrow mutation check supplies no latest-source 29-function gate credit.

After the final grouping correction, skipped no-model establishment was rerun and rejected by seven memory leaves. Restoring the wrong attach-before-ending grouping was separately rejected by its source-grouping oracle. This adds one distinct rejected fault (**ten total**); it does not add whole-file parity credit.

Changing the successor comparison from `==` to `<=` survived. The current policy pins the actual immediate successor B, so the comparator-only change leaves the tested A/B/C sequence unchanged. It receives no rejection credit and supplies no proof of general equivalence or detection of an earlier-than-token input.

## Unverified contracts and owner

Whole-file restart-failure review remains pending. Explicit source [`continue`][continue-source] has no native port here: it places queued users plus the continue submission, creates no entry for the continue itself, and inherits the failed run's `first` when present. Automatic continuation starts its own entry boundary. Source settlement, failure publication and successor creation share one commit; final snapshots and provider gates have not verified that atomic end/start boundary natively. Existing partial-conversion/retry-charge atomicity tests cover another boundary.

Ordinary dispatch/attempt presentation still needs reconciliation with source logical attempts. The selected failure assertions do not establish all intermediate live sequences, callback order, clock/record equivalence or empty-error field presence; see [interrupted retries][retry] and the [18-declaration map][assertions].

Unported restart-limit bodies include source330 preparation cutoff, source367 two-crash tool cutoff/output/result, source438 compaction cutoff, source621 exact-limit salvage and source648 past-limit discard. Source671's actual hanging-cancellation/crash sequence, source712's exact `task_interrupted` report list/error text, and source737's warm-up followed by five measured rounds with average settings resolutions `resolutions / 5 <= 49` also need native verification. The seeded `task_faulted` control covers a different report path.

The poll fixture's 10ms value matches the source `deferredSetup(pollAfterMs = 10)` default for source523/554 and the explicit value in source574; it is not a pacing adaptation.

The project owner retains the remaining upstream alignment work and broader acceptance gates. No full suite, profiling, CI or release workflow was run for this checkpoint. Whole-file restart-failure parity and the full alignment goal are unverified.

[live]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/live.ts#L290-L318
[end-failed]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L628-L654
[start]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L1157-L1172
[reset-source]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L1126-L1132
[no-model-source]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L600-L614
[continue-source]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/submissions.ts#L264-L273
[projection]: ../../durable/run_failure.go
[cleanup]: ../../durable/harness.go
[no-model-code]: ../../durable/generation_no_model.go
[handoff-code]: ../../durable/generation_handoff.go
[policies]: ../../durable/failed_run_140_test.go
[budget]: ../../durable/harness_interruption_budget_main_test.go
[poll]: ../../durable/harness_interruption_poll_main_test.go
[failure]: ../../durable/harness_run_failure_main_test.go
[queue]: ../../durable/harness_failure_queue_main_test.go
[reset]: ../../durable/harness_failure_reset_main_test.go
[preparation]: ../../durable/harness_failure_preparation_main_test.go
[abort]: ../../durable/harness_failure_abort_main_test.go
[boundary]: ../../durable/harness_failure_boundary_main_test.go
[ownership]: ../../durable/harness_failure_ownership_main_test.go
[handoff-test]: ../../durable/generation_handoff_test.go
[tool-wait]: ../../durable/harness_retry_tool_wait_main_test.go
[assertions]: restart-failure-assertions.md
[retry]: interrupted-retry.md
