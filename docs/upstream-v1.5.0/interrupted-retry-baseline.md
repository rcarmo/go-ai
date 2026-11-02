## Crash-interrupted generation retries

Native recovery resends an interrupted request without charging `settings.retry`. Two focused source-derived scenarios fail on `e7e5f0706824a763c19420438435e9b1af7a4582` (production `decbd9d4419d0580c6b84e6832b07d7893573444`). The clean-close control passes. Production has no changes in this checkpoint.

Official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`, defines the interruption path in `packages/durable/src/harness/generation.ts:497-543`: a running request or poll cut off by host loss consumes an attempt, preserves a committed partial as an aborted entry and follows the current retry policy. Exhaustion ends the run `unanswered/interrupted`; a permitted retry sleeps before the next attempt. The source uses `attempt <= maxRetries` with a one-based logical attempt. That expression alone is not an off-by-one difference from a zero-based retry count.

### Existing native tests

These tests were read without rerunning their completed gates:

| Native anchor in `durable/` | Existing contract | Missing interruption condition |
| --- | --- | --- |
| `TestGenerationDurableRetryUsageAndReopenDeadline` | Retryable provider error stores a failed receipt, `RetryCount=1`, deadline2000 and usage; reopen respects that deadline. | A crash supplies no provider error receipt. |
| `TestGenerationRetryRepreparesCommittedAgentToolsPromptAfterReopen` | A provider-error retry uses the current model, prompt, tools and temperature without duplicating the input. | Crash recovery currently resends pinned intent directly. |
| `TestGenerationRetryDecisionReadsCurrentSettingsAfterResponse` | Retry decision after a response reads current settings. | No response exists when the host stops. |
| `TestGeneration140PacingAlarmAloneResumesRetryAndPoll` | Existing retry/poll sleeps respect their pacing alarm. | It does not create or charge a crash retry. |
| `TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission` | Generic restart-limit exhaustion and abort settle the task/submission. | Generic restart limits and model retry budgets are separate controls. |
| Deferred-Harness eleven-scenario map and nine slow-fixture guards | Stored/held recovery outcomes and literal retry overrides. | The nominal paths do not assert charging, sleeping or retry exhaustion. |

The relevant native paths are `runGenerationInvocation`, `sessionCommitInterruptedPartial` and the provider-response retry decision in `durable/generation.go`. The response branch increments `RetryCount`. Recovery converts the partial, then increments `Attempt` and resends. The incoming restart marker is not used there to enter a retry or refuse a request. `Attempt` also increments on a clean resend, so a fix must reconcile logical attempt numbering with dispatch/ownership fences before using that counter as a retry budget.

### Three selected source scenarios

The source file is `packages/durable/test/harness-restart-failure.test.ts`. Its other15 declarations have not been reconciled here.

| Source lines | Assertions retained by `TestMainHarnessInterruptedRequestRetryPolicy` | Baseline |
| --- | --- | --- |
|180-202|Retry enabled/max2/base60000ms. At reopened clock1000000: task sleeps until1060000 in phase`retry`; restarts reset; exact generation `{attempt:1,retry:{at:1060000,error:"The host stopped while the model was answering, on attempt 1"}}`; wake inactive/at1060000. Extra native control: no second provider call.|Fails: native dispatches a second request immediately, completes the task, removes live generation and has no future wake.|
|242-252|After a streaming crash, current retries disabled: canonical submission `unanswered`, reason`interrupted`. Extra native control: no second provider call.|Fails: native resends and settles `done`.|
|254-273|Retries disabled before clean close and reopen: no restart marker; resend succeeds and submission is`done`.|Passes; two total provider calls.|

The pending file is `durable/testdata/upstream-main/pending/harness_interruption_retry_test.go`. It uses real memory/journal/SQLite storage, a stored64-character `z` prefix, failed-Session shutdown/join/reopen for a crash and ordinary Close for the control. It checks the actual reopened restart marker before resuming. No task state, publication, retry or settlement is fabricated. Controlled clock steps, a scripted provider and native document/checkpoint envelopes adapt the source's slow stream, process killing and SQLite representation. The source backoff/disabled assertions have not been relaxed.

The initial fixture sampled storage before its zero-window timer had fired and failed all nine storage preconditions. Those failures are excluded. The corrected fixture fires the captured due timer and enters the real foreground gate before requiring the stored prefix. The final baseline is Go1.27.3, `GOMAXPROCS=4`, count1, protected prototype excluded:

* CGO1: six failing crash-policy backend leaves, three passing clean-close leaves,0.544s.
* CGO0: four failing crash-policy backend leaves, two passing clean-close leaves,0.341s.
* Race/CGO1: the same six contract failures and three control passes,2.891s; no race report. This is a failing race baseline.
* Pending-inclusive durable vet and Staticcheck v0.8.2 pass in both CGO modes. Staticcheck uses a physical source copy with the pending oracle included and the protected prototype omitted. Module verification, formatting, diff and prototype-content checks pass.

### Explicit execution

Default Go discovery excludes `testdata`. Run this known-failing oracle explicitly; there is no default skip in the fixture. Use a distinct project-owned run component for concurrent work.

```bash
cd /workspace/projects/go-ai
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4
export RUN="$PROJECT_TMP_ROOT/runs/retry-charging/manual"
mkdir -p "$RUN"
bun -e '
import { writeFileSync, realpathSync } from "node:fs";
const blank = process.env.RUN + "/excluded-prototype.go", Replace = {};
writeFileSync(blank, "package durable\n");
for (const root of new Set([process.env.PWD, realpathSync(".")])) {
  Replace[root + "/durable/continuation_prototype_main_test.go"] = blank;
  Replace[root + "/durable/zz_main_interruption_retry_test.go"] =
    realpathSync("durable/testdata/upstream-main/pending/harness_interruption_retry_test.go");
}
writeFileSync(process.env.RUN + "/overlay.json", JSON.stringify({ Replace }));
'
CGO_ENABLED=1 go test -overlay "$RUN/overlay.json" ./durable \
  -run '^TestMainHarnessInterruptedRequestRetryPolicy$' -count=1 -timeout=90s
```

Exit1 is expected until the crash-policy gap is fixed. CGO0 uses the two available backends; add `-race` with CGO1 for the failing race baseline. Analyse results, then remove the owned run scratch only after its jobs finish. Preserve the in-place prototype, installed dependencies/toolchains and durable assets.

### Runtime work still required

Before changing production, reconcile the source's exhausted-budget case204, already-sleeping/tools case275, interrupted poll523, backwards-clock poll574 and crash-then-clean-close case596 with native controls. Charge a crash once, persist conversion and retry/failure together, retain deferred handles through a permitted poll retry, and cancel the provider job on exhaustion. Preserve clean-close pinned resends, public immediate Commit, cancellation, restart-limit accounting and ownership fences. The nine slow fixtures will need explicit advancement of the new1ms backoff rather than an assumption of immediate resend.

The current source review keeps `pending-review` for the whole restart-failure file and records this selected failing baseline. This is not TypeScript execution, a runtime fix, whole-file parity or full v1.5.0 alignment. Broader validation/profiling needs Rui's authorisation.
