# Interrupted generation retries

Official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5` charges a running request or deferred fetch interrupted by host loss against the current retry policy (`generation.ts:497-543`). Native recovery now makes that decision before a poll deadline, including when the clock moved backwards. The [historical baseline](interrupted-retry-baseline.md) records the original failures; they were not repeated as unchanged baselines.

## Runtime change

Native implementation: `46710ff8921dce0187eaa4df855e8fc5d62834b8`. This is an incremental runtime checkpoint, not a release or full-alignment acceptance.

`durable/generation_interruption.go` commits partial conversion, the retry charge and the next task state together. A permitted retry sleeps until its backoff deadline. Exhaustion settles `unanswered/interrupted` with the source diagnostic and failed task outcome. A stored partial becomes one aborted assistant entry; its usage is recorded once.

The decision reads current resolved agent/host retry settings. `RetryCount` tracks logical failures; `Attempt` remains the dispatch/presentation ownership fence and can advance on a clean resend. A clean close does not charge a retry. Retry presentation uses the logical count rather than the inflated dispatch count.

An interrupted poll retains the model and provider handle. After backoff it fetches the same job, without submitting another request. Exhaustion uses the existing best-effort provider cancellation outside the Session line. Abort, Close and runtime-lifetime checks still fence the deciding commit.

Only interrupted intent/poll checkpoints with a persisted model and positive attempt witness enter this path. Legacy pacing-only records without those witnesses retain their deadline migration before model access. Tools and already-sleeping retries do not enter the interruption branch. No new checkpoint field, retry default, public Commit policy, dependency or storage format was introduced.

## Focused assertions

At the pinned checkpoint, the four passing fixtures were promoted from `testdata` into ordinary Go tests without changing their assertions. The table and validation below retain that checkpoint's scope; later mappings are linked below. The whole upstream restart-failure file remains `pending-review`.

| Source / control | Native test and scope |
| --- | --- |
|180-202,242-273|`TestMainHarnessInterruptedRequestRetryPolicy`: current-policy backoff, disabled termination and clean-close resend.|
|204-240|`TestMainInterruptedBudgetAndCloseAccounting/exhaust`: two interrupted attempts, exact failed outcome including reason detail, no third request, both stored partials and one input. The source live `runFailure` object is not mapped.|
|523-594|`TestMainInterruptedPollRetryEdges`: retry/reuse, disabled cancellation and backwards-clock termination; exact retry diagnostic, next deferred attempt and answer. Uses a 10ms initial poll for all modes (literal source574; pacing adapter for523/554). Source554's live `runFailure` object is not mapped.|
|596-619|Budget `charge-once` exercises crash/clean-close/reopen before charging, one backoff and no duplicated input/partial. `TestMainInterruptedMarkerSurvivesCleanClose` separately checks marker and stored-prefix survival through two pre-charge clean closes; it does not itself resume or assert charging.|
|Native phase/budget controls|Budget `sleeping` crashes an already-charged sleeping retry and preserves its deadline/budget. `clean-resend` advances the dispatch counter through a clean resend, then proves its first actual crash still has the one-retry budget. These are not complete source275/provider-error/tool-wait translations.|

Existing provider-response retry/repreparation/current-settings, pacing, restart-limit, legacy ownership and affected deferred recovery tests were included because production changed. The real append-fault recovery test now also requires conversion and the sleeping retry checkpoint in the same stored batch. Its next append fails before the next request intent, and reopen preserves the converted prefix without charging it twice.

Controlled provider clocks advance only actual future wake deadlines. The request/poll fixture samples public `Wake` quiescence, not native `WaitForIdle`: a sleeping, unanswered submission is not native idle. An initial backwards-clock timeout from the unsuitable idle sampler is excluded; the corrected unchanged-runtime poll baseline failed nine backend leaves in 0.711s. The pre-charge marker control passed. An initial runtime draft omitted host setting resolution; it was corrected before credited passing gates.

## Validation

Go1.27.3, `GOMAXPROCS=4`, protected prototype excluded throughout:

* Affected focused set: normal count3/shuffle42 CGO1/0, 18.358/11.854s; race count3/shuffle7, 108.008s.
* Same runtime, stronger atomic-batch assertion: only that function rerun, normal3 CGO1/0 0.767/0.460s; race3 5.464s.
* Four promoted fixtures, including stronger exact exhaustion outcome: normal3 CGO1/0 6.830/3.882s; race3 39.511s.
* Durable vet and pinned Staticcheck v0.8.2 pass in both CGO modes; physical static-check copy excludes the prototype and includes these fixtures. Initial copy omitted `images`; that setup error was corrected, not counted as a runtime failure. Module verification, formatting and diff checks pass.
* Six active faults are rejected: omit crash charging (12 backend leaves), lose the deferred handle (3), charge clean close (3), omit partial conversion (12), use dispatch attempts as the budget (3), and split conversion from charging (3 explicit atomicity assertions).

The legacy pacing regression was found before the final gates: synthetic model-less poll-running records were incorrectly charged. The witness guard fixes it without changing the original assertions. No TypeScript/Vitest, full-suite, profiling, hosted CI, release or full-alignment result is claimed.

## Superseding checkpoint

Runtime `6fb7c7577c7d4a7d0ae575cc14da3436fe29c768` implements the selected source204/554 live `runFailure` assertions and joined source275 retry-sleep/tool-wait oracle; see the [run-failure checkpoint](run-failure.md) and [18-declaration assertion map](restart-failure-assertions.md). The corrected 29-function selection passed normal CGO1/0 in 34.404/23.850s and race CGO1 in 183.483s. After correcting no-model grouping to end A before selecting queued B/C, its 11 affected functions passed normal/count3/shuffle42 in **10.532/9.068s**, race/count3/shuffle7 in **68.021s**, and vet/Staticcheck v0.8.2 in both CGO modes. Earlier receipts retain their original pins and scope.

[Boundary-first](boundary-first.md) subsequently closes the source-derived passive-write-first boundary while preserving each input's own `cp.InputEntry`, and selected answer/model-error/no-model atomic end/start cases. The [current pure-Go validation](run-failure.md#superseding-boundary-checkpoint) supersedes the earlier 44-function/race gates for the latest fail-fast overlay; earlier receipts above keep their original scope.

## Still open

Whole-file restart-failure review remains `pending-review`. Explicit source `continue`, upstream `afterRun`, full mixed-mode/reset coverage, full source test bodies, remaining restart-limit declarations, exact interruption reports, settings-resolution work counts and empty-error field presence remain open, as does full callback/record/clock equivalence. Ordinary request/deferred presentation still uses native dispatch ownership counters; source attempt equivalence is unverified.

Wider Session/Harness, schema, provider, catalogue and package/source correspondence work remains open. Acceptance profiling and broader gates still require Rui's authorisation.
