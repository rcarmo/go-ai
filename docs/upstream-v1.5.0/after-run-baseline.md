## After-run lifecycle baseline

Go has no `GenerationHooks.AfterRun` field or answer/finish/drain lifecycle. The focused baseline fails on memory, journal and real pure-Go SQLite: **six failing leaves, three passing no-hook controls, 0.549s**, using Go 1.27.3, `GOMAXPROCS=4` and `CGO_ENABLED=0`. Production stays at `b6b8d4185a7be3bc9dc1a84811af7925cf5c3791` (documentation head `cbb8c9de588df163b4d6b7d68597431b21e1e15b`).

The target is official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Its [after-run tests][tests] contain 29 declarations. This baseline translates selected assertions from two declarations; no complete body/file or TypeScript execution is credited.

### Existing native behaviour

`GenerationHooks` has before-request, after-response, after-tools/entries and on-yield hooks. It has no after-run hook. Existing `TestOnYieldContinuationUsesSameRunButQueuedUserWins`, `TestGenerationHeldHandoffReopensWithoutRerunningHooksOrRequests`, the response-owned boundary test and late-owned-work tests cover useful prerequisites, but none registers an after-run hook. They were reconciled without rerunning their completed gates.

`Harness.finish` creates a decided builtin outcome and calls `cleanupGeneration`. An ordinary owned task can keep the generation task `completing`, but cleanup settles inputs and ends the live run at the answer. Source after-run keeps the live run busy, with no decided generation outcome, through separate finish/drain phases. Native scheduler recovery also has a closed generation-phase whitelist which does not admit those phases. Reusing the generic held-outcome path alone cannot implement this lifecycle.

### Preserved oracles

The pending files are:

* `durable/testdata/upstream-main/pending/harness_after_run_test.go` -- three functions, no default skip.
* `durable/testdata/upstream-main/pending/after_run_declarations.go` -- declaration-only baseline adapter, excluded from ordinary discovery.

The overlay adds the proposed `RunSummary`, `RunEndTask`, `AfterRunResult` names and one `GenerationHooks.AfterRun` field so a hook can be registered in a test. It adds **no dispatch, selection or lifecycle implementation**. These names/shapes are test adapters, not an accepted public API or storage format.

| Native function | Source | Baseline |
| --- | --- | --- |
| `TestMainAfterRunAnswerTasksHoldRun` | 181–219: edit tool, final answer, settled input, one held ordinary owned child, empty live inputs, busy idle wait, exact first/answer/last summary, child result as last entry, cleared run/failure and completed generation retaining answer ID. Native extra checks require the hook's entry read to see its committed answer and exactly two requests. | Fails all three backends: the run becomes idle before a requested task starts. The subsequent child/summary/drain assertions are retained but are **unreached**, not passing. |
| `TestMainAfterRunNoHookImmediateEnd` | 251–260: task installed with no hook; the answer's commit leaves the live document empty. | Passes all three backends. |
| `TestMainAfterRunEmptyHookEnd` | 262–275: hook returns no requests; after idle it has been asked exactly once, no task ran and the live document is empty. | Fails all three backends: hook count zero instead of one. |

The second source declaration's setups are split into independent functions so the valid no-hook control remains visible. Go receipts/`message` entries plus roles replace source `pi.user`/`pi.assistant`; native compaction/task naming and opaque definition tokens differ. The task fixture uses the real public task runtime, commit, entry append and wait APIs. No task record, hook invocation or live state is fabricated. A cancellable actual idle wait exposes the missing hold without sleeping or synthesising a signal. The adapter's absent hook invocation is an explicit API/runtime gap; it establishes no passing after-run behaviour.

Draft `Extension.GenerationHooks`/numeric `EntryCursor` compile errors were corrected to native `Extension.Hooks`/`EntryCursor{}` before this baseline. They receive no behavioural credit. Pending-inclusive durable vet and Staticcheck v0.8.2 with tests pass; the static copy includes the adapter and oracle and physically excludes the protected prototype. Formatting/diff and prototype comparisons pass. No runtime or dependency changed.

### Execution recipe

From the repository root, this creates project-owned scratch and maps both logical and canonical source paths. The protected prototype remains physically in place.

```sh
. scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=4 GOTOOLCHAIN=go1.27.3
export AFTER_RUN_BASELINE_ROOT="$PROJECT_TMP_ROOT/runs/after-run/baseline"
mkdir -p "$AFTER_RUN_BASELINE_ROOT"
bun -e '
const r = process.env.AFTER_RUN_BASELINE_ROOT;
const logical = process.env.PWD;
const pending = logical + "/durable/testdata/upstream-main/pending/";
let hooks = await Bun.file("durable/hooks.go").text();
const anchor = "type GenerationHooks struct {";
if (hooks.split(anchor).length !== 2) throw Error("hook anchor changed");
hooks = hooks.replace(anchor, anchor +
  "\n AfterRun func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error)");
await Bun.write(r + "/hooks.go", hooks);
await Bun.write(r + "/excluded.go", "package durable\n");
const Replace = {};
for (const root of new Set([logical, process.cwd()])) {
  Replace[root + "/durable/hooks.go"] = r + "/hooks.go";
  Replace[root + "/durable/after_run_declarations.go"] = pending + "after_run_declarations.go";
  Replace[root + "/durable/harness_after_run_main_test.go"] = pending + "harness_after_run_test.go";
  Replace[root + "/durable/continuation_prototype_main_test.go"] = r + "/excluded.go";
}
await Bun.write(r + "/overlay.json", JSON.stringify({Replace}));
'
go test -overlay "$AFTER_RUN_BASELINE_ROOT/overlay.json" ./durable \
  -run '^TestMainAfterRun' -count=1 -timeout=90s
```

The final command is expected to fail on this baseline. Virtual-file discovery was checked before execution. Remove only completed scratch after analysis; keep installed dependencies, source and active-job files. The saved baseline was analysed once; no repeat run is needed to recover it.

### Source queue and implementation boundaries

The complete 29-declaration index below was extracted from the exact pinned file. Every row applies to native after-run work. Rows outside the selected 181/251 assertions have no new passing parity credit.

| Declaration start | Contract |
| ---: | --- |
| 181 | Inputs done at answer; owned tasks keep run busy. |
| 221 | Terminate summary's last entry is tool result or handoff reset. |
| 251 | No-hook immediate end and empty-hook completion. |
| 278 | Queued writes, steers, follow-ups and busy rejection through drain. |
| 353 | Reset queued during tasks is placed after them. |
| 372 | On-yield continuation suppresses after-run until the run actually ends. |
| 429 | Crash before creation asks again; created tasks prevent re-asking. |
| 478 | Stop during hook/task ends an answered run without failure. |
| 516 | Independently aborted child permits normal run end. |
| 538 | Broken hooks/requests are reported; valid requests survive. |
| 617 | Installed definition wins; initialisation runs once off the line. |
| 656 | Hook reads committed run history including tool results. |
| 694 | History survives compaction of the run's start. |
| 757 | Restart-limit exhaustion during finish completes answered run normally. |
| 785 | Unrunnable child holds busy state until Stop orphans it. |
| 818 | Task-admitted follow-up starts a fix run after drain. |
| 858 | Continue after failure inherits failed history. |
| 882 | Hook deselection before finish skips invocation. |
| 903 | Fork of a busy after-run conversation starts idle. |
| 923 | Missing recorded first falls back to answer. |
| 955 | Child restart-limit exhaustion permits normal run end. |
| 983 | Lowered child limit followed by normal end emits no interruption report. |
| 1019 | Lowered generation limit followed by normal finish emits no interruption report. |
| 1045 | Terminate wins over turn limit. |
| 1062 | Continue history begins at first tool round when no failure/input was placed. |
| 1094 | Queued summary is stale behind terminate handoff. |
| 1140 | Commit A stores held progress; orderly close in finish causes re-asking. |
| 1188 | Foreign run ownership is untouched; answered own run ends without failure. |
| 1200 | Failure projection retains first entry. |

Before implementation, translate 538/617 request-validation/installed-definition/off-line-initialisation assertions. Then add a real typed API and phase-local hook selection. Commit A must atomically append/store the answer and usage, settle run inputs, clear live generation/tool presentation, retain the run with `afterRun={}`, and persist a detached summary in a nonterminal finish checkpoint. It must not place queued boundaries or decide a held terminal outcome.

Finish reselects hooks, validates every request off the line against the installed definition, reports rejected requests without discarding valid siblings, and creates all valid ordinary owned children in one commit. Initialisation must run once; blindly calling current `Tx.CreateTask` after prevalidation would run it twice. The same commit records IDs in live state and a drain checkpoint. Drain waits for actual child terminal outcomes/host joins with all-settled semantics, ends the run retaining the original answer, then applies queued boundaries. No second answer append or usage charge is allowed. Recovery, Stop, hook deselection, ownership and restart-limit controls must be translated before accepting the feature.

After analysis, 27MB of owned source extracts, overlays, logs and static-source copies plus 500MB of completed Go build/test cache were removed with no active Go/compiler/staticcheck jobs. Source, installed dependencies/toolchains and the pinned mirror were preserved. The protected comparison copy is kept only through the push check.

Whole-file review stays `pending-review`. Full after-run, callback/clock, mixed-boundary, empty-error, restart/report/settings and release alignment are unverified. No full suite, race, profiling, CI or release publication was run.

[tests]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-after-run.test.ts
