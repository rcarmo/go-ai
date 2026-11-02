## Restart-failure assertions

Official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5` has 18 declarations in [`harness-restart-failure.test.ts`][restart]. The corrected 29-function native slice passed focused normal and race gates on 2 November 2026 **before the final no-model source-grouping correction**; those results do not validate the latest source. Runtime is `6fb7c7577c7d4a7d0ae575cc14da3436fe29c768`; the final 11-function grouping-delta results are recorded below. Whole-file review and full-body parity remain pending and unverified.

Source line numbers below use `/workspace/tmp/go-ai/runs/run-failure/20261102/source-restart.ts`, extracted from that release. Lifecycle references use the matching `source-live.ts`, `source-generation.ts` and `source-submissions.ts`. The existing [interrupted-retry checkpoint][retry-doc] and `upstream-review-f9a26365.json` retain their previous state until the main edits reconcile them.

## Native test names

The short names in the declaration table refer to these functions in `durable/`:

| Short name | File and function |
| --- | --- |
| Request | [`harness_interruption_retry_main_test.go`][request]: `TestMainHarnessInterruptedRequestRetryPolicy` |
| Budget | [`harness_interruption_budget_main_test.go`][budget]: `TestMainInterruptedBudgetAndCloseAccounting` |
| Poll | [`harness_interruption_poll_main_test.go`][poll]: `TestMainInterruptedPollRetryEdges` |
| Marker | [`harness_interruption_marker_main_test.go`][marker]: `TestMainInterruptedMarkerSurvivesCleanClose` |
| Tool wait | [`harness_retry_tool_wait_main_test.go`][tool-wait]: `TestMainHarnessRetrySleepAndToolWaitDoNotChargeGenerationRestarts` |

These fixtures use failed-Session/join/reopen boundaries in place of upstream `kill()`, controlled clocks and scripted native providers. Numeric native IDs replace source IDs; `Restarts == 0` represents absent source `restarts`. Poll uses the source 10ms initial delay in every mode: `deferredSetup(pollAfterMs = 10)` supplies source523/554, and source574 passes 10 explicitly. This value needs no pacing adaptation. Native dispatch ownership counters and source logical retry attempts require separate interpretation. Request and Marker retain their earlier [interrupted-retry results][retry-doc]; neither function is in this checkpoint's corrected 29-function selection.

## The 18 declarations

Each row identifies a declaration by its starting source line and the assertions mapped so far. A native control checks only the behaviour named in its row.

| Source line | Declaration contract | Native assertions and gaps |
| --- | --- | --- |
| 141 | Charge the attempt, keep the partial, back off and answer next time. | Budget/`charge-once` checks conversion, preserved text, one input and eventual answer; Request/`backoff` checks the retry diagnostic and deadline. The source request-role list `[["user"]]`, complete live-value sequence and final empty live assertion are not mapped by this combination. |
| 180 | Sleep the configured backoff before retrying. | Request/`backoff`: sleeping/retry checkpoint, deadline, cleared restart count, exact generation retry value and public `Wake`. |
| 204 | Exhaust retries, ending interrupted and keeping every partial. | Budget/`exhaust`: two crashes, exact settlement and failed task outcome, both aborted partials, one input and no third request. Now also calls the exact failure helper described below. |
| 242 | End immediately with retries disabled. | Request/`disabled`: `unanswered/interrupted` and no further provider request. |
| 254 | Resend after a clean close. | Request/`clean-close`: zero restart count before resume and successful resend with retries disabled. |
| 275 | Do not charge a generation sleeping in retry or waiting for tools. | New Tool wait fixture maps all four source assertions: **314** zero restarts before first resume; **319** canonical `waiting` state with checkpoint phase `tools`; **323** zero restarts before second resume/gate release; **326** original submission finishes `done`. Both crashes follow real persisted states: provider 503 retry sleep, then a gated replay-safe tool round. |
| 330 | End a generation cut off at `restartLimit` outside a request. | Not ported: preparation cutoff, exact settlement/failure detail and `task_interrupted` report. |
| 367 | End a tool call with an error result instead of running it again. | Not ported: two-crash execution count, retained partial output, error result/diagnostic, failed task and interruption report. |
| 438 | Fail a compaction into `compactionFailure`. | Not ported: crash-driven cutoff, exact task failure/live value, cleared compaction list and interruption report. |
| 523 | Charge an interrupted poll and fetch the same deferred response again. | Poll/`retry`: one request, two fetches, no cancellation, exact retry diagnostic, visible attempt 2 with deferred handle and final answer text. |
| 554 | Cancel the deferred response when an interrupted poll ends the run. | Poll/`disabled`: exact interrupted settlement, one cancellation without refetch/resubmit, plus the exact failure helper. |
| 574 | Charge an interrupted poll despite a backwards clock. | Poll/`backwards`: fetch starts at 1010, reopen at 1000 still settles interrupted and cancels. |
| 596 | Charge once across crash, pre-charge clean close and reopen. | Budget/`charge-once`: one backoff, answer and no duplicated input/partial. Marker separately preserves restart count and stored prefix across two pre-charge closes; it never resumes. The source retry-publication list `[1]` is not asserted. |
| 621 | End `restart_limit` when request charging keeps being cut off. | Not ported: exact-at-limit partial salvage, no new request and exact failure projection. |
| 648 | Drop the partial past the limit. | Not ported: seeded past-limit count, exact diagnostic, user-only transcript and absent generation. |
| 671 | Keep the run aborted when its abort handler reaches the limit. | New seeded native cutoff control below checks projection/outcome/report distinctions. The source deferred cancellation hanging through two actual crashes, cancellation counts and intermediate restart observations are not ported. |
| 712 | Report `task_interrupted` for work ended by a crash. | Not ported: exact generation report list and report error text. Native `task_faulted` controls cover a different outcome. |
| 737 | Avoid extra settings resolution on the normal path. | Not ported: warm-up followed by five measured tool rounds; `resolutions / rounds <= 49` bounds the average across all five, without a per-round maximum. |

Existing [`restart_limit_140_test.go`][restart-controls] scheduler/seeded cutoff tests and [`TestMainToolProgressDoesNotResetCrashRestarts`][tool-controls] supply native controls. They do not cover the missing generation/tool/compaction restart-limit declarations, reports or settings work counts.

## Failure projection and lifecycle

[`mainRequireRunFailure`][failure] now checks the entire live value for source204/554: `runFailure.{taskId,inputs,reason,detail,first}` plus the explicit native `failedRun: true` queue gate. It filters no arbitrary fields. In these source204/554 fixture shapes, `first` identifies the failed input's persisted user entry; it is not a timestamp or a universal own-input rule. This maps the failure object asserted by those declarations and [`live.ts:290-318`][live].

Source `first` is a run/context boundary entry ID: when a newly applied boundary places passive writes before selected users, the first entry can be a passive write. Native [`startLiveRun`](../../durable/run_failure.go) currently uses `cp.InputEntry` for a new run; passive-write-first boundary parity remains open. Post-tool steers joining an existing run preserve its original `first` (including absence), not the newly attached steer's entry.

The following are source-derived or native lifecycle controls, separate from the 18 upstream declarations. The updated no-model grouping assertions await delta gate results:

| Source contract | Native file/function and assertion scope |
| --- | --- |
| `generation.ts:642-654,887-888,1162-1172`: keep the failure only for the immediate successor. | [`harness_failure_queue_main_test.go`][queue], `TestMainRunFailureContinueQueueClearsFailureAfterAnswer`: A fails; queued B retains A's exact failure while running; after B answers, queued C clears it. Each run owns its own placed input/`first`. |
| `generation.ts:600-614` and `live.ts` `endRun`: no model before a request. | [`harness_failure_preparation_main_test.go`][preparation], `TestMainRunFailureNoModelBeforeRequest`: unconfigured/unavailable model across keep/withdraw/continue; exact failure, no provider call, settlement without detail and policy-dependent passive-write placement. `TestMainRunFailureNoModelReplacesPreviousModelError` checks fresh failure identity and replacement of stale metadata. |
| `generation.ts:1126-1132,1162-1172`: post-tools reset and successor. | [`harness_failure_reset_main_test.go`][reset], `TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor`: real tool/reset path, `unanswered/reset` without detail, retained failure during B, cleared during C, distinct task/entry ownership and reset provider context. |
| Ordinary abort and `live.ts` `endRun`. | [`harness_failure_abort_main_test.go`][abort], `TestMainRunFailureOrdinaryAbortProjection`: public abort with/without committed partial; exact aborted failure without detail. Error-bearing partials use native `commitPartial`; stream-adapter error-text preservation is unverified. |
| Source671 projection after abort-handler cutoff. | Same file, `TestMainRunFailureAbortHandlerCutoffProjectionDerived`: real run ownership followed by seeded abort/restart state; run settles `aborted` with cutoff detail while task/report are `faulted`/`task_faulted` with `restart_limit`. Copies the actual committed partial into legacy `cp.Partial`, then checks no assistant conversion, unchanged `pi.usage` and no redispatch. The receipt's `Usage` is nil; non-zero legacy-usage discard is unverified. |
| No-model placement in the deciding commit. | Same preparation file, `TestMainRunFailureQueuedFollowUpNoModelRetainsDecisionPlacement`: a successful run precedes a genuinely unplaced queued follow-up; its exact terminal commit publication appends and retains its own user entry without provider redispatch. |
| No-model failure before queued steers join a successor. | Same preparation file, `TestMainRunFailureNoModelAttachedSteersAndRemainingSuccessor`: A alone fails `no_model` without settlement detail/answer; its exact terminal publication neither places nor settles B/C. Both are still queued/unplaced at the successor resolver gate; the exact successor token is B. Actual provider preparation groups B+C in `all`, or starts B then C separately in `one-at-a-time`, checking distinct entries and successful settlements/answers. A's failure remains only during B's run (the shared B+C run in `all`), and clears for separate C. Delta gates pending. |
| Attached inputs with no queued successor. | [`harness_failure_boundary_main_test.go`][boundary], `TestMainRunFailureAttachedInputsWithoutQueuedSuccessor`: real tool handoff, exact ending owner/IDs/first and reason/detail for every input, no reserved successor and fresh in-progress clearing. Model-error detail is the selected task error (`attached failure`); reset detail is absent. |
| Foreign-run ownership fences. | [`harness_failure_ownership_main_test.go`][ownership], `TestMainRunFailureNoModelOwnershipFence` and `TestMainRunFailureToolHandoffOwnershipFence`: seeded competing runs through real Session commits, scheduling disabled; rejection without partial Session/storage adoption, plus owned/absent positive controls. The handoff rejection occurs after both task writes are staged. |
| Automatic continuation, replacement and reopen. | [`harness_run_failure_main_test.go`][failure], `TestMainRunFailureContinuationAndReopen`: retain old failure during automatic continuation, replace it on another failure, preserve it across reopen without redispatch/caller mutation, then clear it on fresh answered work. |
| Failed-run queue policies. | [`failed_run_140_test.go`][policies], `TestFailedRun140InboxPolicies`: existing keep/withdraw/continue controls now use the exact failure helper and explicit gate expectation. Bounded-withdrawal recovery and owned-host joins are existing native controls. |

The inspected [`finishGenerationNoModel`](../../durable/generation_no_model.go) no longer calls `applyQueuedInputs` on dying A: upstream `failNoModel`/`endFailedRun` ends A before the final boundary selects B/C. The updated test checks this source grouping through real resolver/provider gates, without seeded live/checkpoint data. It does not assert an A+B failure during C, and the earlier 29-function passes do not validate this changed body.

Explicit source `continue` is unported. [`submissions.ts:267-276`][submissions] places queued users plus the continue submission, creates no entry for the continue itself, and inherits `runFailure.first`; automatic continuation above starts its own entry boundary. Source atomic end/start behaviour also lacks a port: final snapshots and provider gates do not establish that settlement, failure publication and successor creation share one commit. Existing atomic partial-conversion/retry-charge tests exercise a separate boundary.

## Corrected focused results and limits

The following are **earlier results, before the final no-model grouping runtime/test delta**, not latest-source gate evidence. On 2 November 2026, Go 1.27.3 with `GOMAXPROCS=4` passed the corrected 29 actual functions: normal `-count=3 -shuffle=42` took **34.404s CGO1 / 23.850s CGO0**; race `-count=3 -shuffle=7` with CGO1 took **183.483s**, without a race-detector report. Durable vet and pinned Staticcheck v0.8.2 passed in both CGO modes. Staticcheck used an exact physical source copy including the new files and excluding the protected prototype. Module verification, scoped `gofmt` and diff checks passed. The [run-failure checkpoint][run-failure] retains the earlier verified list from `/workspace/tmp/go-ai/runs/run-failure/20261102/selected.txt`; unchanged function names do not certify their latest bodies. The final 11-function no-model delta passed normal/count3/shuffle42 in **10.532s CGO1 / 9.068s CGO0**, and race/count3/shuffle7 in **68.021s**, without race diagnostics. Durable vet and Staticcheck v0.8.2 passed both modes on final source. This verifies the changed slice of runtime `6fb7c7577c7d4a7d0ae575cc14da3436fe29c768`, not a repeated full 29-function run or acceptance gate.

An earlier smaller regex included six nonexistent names; none receives execution credit. An earlier larger race/count3 run failed stored-prefix fixture preconditions without a race-detector report. `f.partial` observed memory before the host's persistence timer arm. The repaired fixture waits for actual `armComplete` and foreground drain, preserving the prefix and failure assertions without arbitrary sleeps. The isolated pre-fix race run did not reproduce the failure; the earlier corrected race run passed, before the final no-model grouping delta.

Paired memory controls rejected eight distinct faults: the original five rerun (missing record, fabricated `first`, clearing immediate-successor history, keeping fresh-run history, lost handoff `first`), plus skipped no-model establishment, legacy-partial conversion at abort cutoff and bypassed foreign-run ownership. The no-model fault has two assertion checks, giving nine rejections for those eight distinct faults; they were not freshly rerun after the final no-model grouping delta.

A ninth meaningful fault pins continue to the maximum submission ID in the conversation instead of the selected successor, while leaving the exact comparator unchanged. The paired `TestMainRunFailureContinueQueueClearsFailureAfterAnswer/memory` baseline passed; the same single mutant memory leaf failed B-in-progress failure retention (`runFailure` absent). Combined credit is **nine distinct rejected faults and ten assertion rejections**. The zero-test startup/toolchain probe and compilation/setup failures receive no credit. Activation is supported by the isolated source delta and paired behavioural contrast, not an unobserved compiler trace. Reusing the legacy-partial guard receipt adds no fresh execution credit. This narrow mutation check is not a latest-source 29-function gate.

After the final source-grouping correction, skipped no-model establishment was rerun and rejected by seven memory leaves; restoring wrong attach-before-ending grouping was separately rejected. Ten distinct faults have rejection evidence in total. A comparator-only `==` to `<=` mutation survived: the current immediate-successor pin leaves this tested sequence unchanged. It receives no rejection credit; general equivalence is unverified.

Historical results keep their original scope: source275 passed its unchanged-runtime three-backend baseline in **0.590s**, and the preliminary lifecycle slice passed in **1.873s**. The historical 14-leaf missing-record baseline and other fixture cardinalities are preserved in [run-failure.md][run-failure]. Earlier interrupted-retry normal/race/static/mutation results are separate.

Whole-file restart-failure review remains pending. Explicit continue, atomic end/start, passive-write-first boundaries, empty-error field presence, the remaining restart-limit declarations, exact interruption reports, settings-resolution work counts, full declaration bodies and callback/record/clock equivalence are unverified. Broader regression gates, acceptance profiling and the full alignment goal also require verification.

[restart]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-restart-failure.test.ts
[live]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/live.ts#L290-L318
[submissions]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/submissions.ts#L267-L276
[retry-doc]: interrupted-retry.md
[run-failure]: run-failure.md
[request]: ../../durable/harness_interruption_retry_main_test.go
[budget]: ../../durable/harness_interruption_budget_main_test.go
[poll]: ../../durable/harness_interruption_poll_main_test.go
[marker]: ../../durable/harness_interruption_marker_main_test.go
[tool-wait]: ../../durable/harness_retry_tool_wait_main_test.go
[failure]: ../../durable/harness_run_failure_main_test.go
[queue]: ../../durable/harness_failure_queue_main_test.go
[preparation]: ../../durable/harness_failure_preparation_main_test.go
[reset]: ../../durable/harness_failure_reset_main_test.go
[abort]: ../../durable/harness_failure_abort_main_test.go
[boundary]: ../../durable/harness_failure_boundary_main_test.go
[ownership]: ../../durable/harness_failure_ownership_main_test.go
[policies]: ../../durable/failed_run_140_test.go
[restart-controls]: ../../durable/restart_limit_140_test.go
[tool-controls]: ../../durable/harness_tool_recovery_main_test.go
