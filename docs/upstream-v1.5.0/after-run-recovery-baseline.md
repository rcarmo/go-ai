# Answered-run recovery baseline

The unchanged after-run runtime at `ac5ac784c19fb0dd2666356f319f7cd5a9f3ca75` passes crash/no-reask and normal lowered-limit controls, but fails answered-finish and child-cutoff recovery. No runtime fix is included in this baseline.

The oracle is `durable/testdata/upstream-main/pending/harness_after_run_recovery_test.go`, derived from official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/test/harness-after-run.test.ts`:

| Source declaration | Native function suffix after `TestMainAfterRun` | Unchanged runtime, three backends |
|---|---|---|
|429 crash in finish, then crash in drain |`CrashFinishReasksDrainDoesNot`|3 passes/.260s|
|757 answered finish reaches restartLimit=1, queued input retained |`AnsweredFinishRestartLimit`|3 failures/9.190s|
|955 child reaches restartLimit=1, generation completes |`ChildRestartLimitCompletesGeneration`|3 failures/9.350s|
|983 normal worker completion after limit lowered |`LoweredLimitNormalWorkerNoInterrupted`|3 passes/.070s|
|1019 normal finish after limit lowered during hook |`LoweredLimitNormalFinishNoInterrupted`|3 passes/.190s|

Go1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4`, count1: **9 backend passes,6 failures**,3 top-level passes/2 failures, no skips,19.069s. This is a failing baseline. The pending file is excluded from ordinary test discovery. It was compiled and executed through an overlay, with the unrelated continuation prototype replaced only in the compiler input.

## Assertions and adapters

The fixture runs memory, journal and real pure-Go SQLite. Process loss uses the existing native adapter: fail the actual Session, observe failed closure, cancel/join the host, then reopen its real storage. It neither writes task checkpoints nor fabricates restart markers. This differs from the source storage proxy that hangs old calls after kill; the host join prevents stale Go invocations writing into reopened state.

Hooks/tasks use predetermined per-invocation channels. Source429 reasks before children exist, then repeats the same `run 2` task without another hook call or model request; there is exactly one completion entry and no unfinished task. Its additional orderly-close-in-drain portion is **not** translated here. The older native close-in-finish test does not cover that portion.

Source757 checks unchanged ask/model counts, cleared run/failure, retained queued item, completed generation and interruption report. Source955 checks completed generation, cleared run/failure, no child rerun and fault report. Native report `Type` adapts source `kind`. Source983 lowers settings from the worker through its native runtime's host; source1019 lowers them while the rerun hook is held. Both verify absence of interruption reports after normal completion.

## Demonstrated gaps and next action

Both failing cases exhaust their explicit3s idle waits on each backend; the overall75s test timeout is not reached. They also fail independent terminal-state/run-cleanup/report assertions. The answered finish remains pending with its retained answer; the child and its waiting run remain live. Native `task_parked` reports appear instead of the expected `task_interrupted` or child `task_faulted` reports. Queue retention and the no-rerun checks do not establish correct completion.

The native scheduler still parks un-aborted work at the limit. Source scheduler.ts1929-1943 faults ordinary kinds at the limit, while generation.ts365/386 and974-1004 let answered finish/drain end completed, retain queued items and explicitly mark crash-caused completion for reporting. Existing legacy parking tests must be reconciled against that v1.5.0 contract, not retained as compatibility requirements. Normal completion after a host lowers the limit must remain unreported.

The dispatch change must also respect the source `LIMITS_RESTARTS` set (`live.ts246`): run kinds, tool and compaction handle their own limits. `harness.ts251` passes that distinction to scheduler.ts1933; tool.ts151/206 routes its own cutoffs, and generation has separate answered/unanswered cleanup. Blindly removing native global parking would permit builtin crash loops without their source cutoff paths. Reconcile these narrowly coupled controls and the obsolete parking expectations before changing dispatch.

Next: implement the source dispositions and attribution without weakening ownership/cancellation or fabricating failure on an answered run, then rerun these oracles plus narrowly affected scheduler controls. Whole-file after-run review remains `pending-review`.

## Overlay recipe

Source `scripts/project-env.sh`, export `CGO_ENABLED=0 GOMAXPROCS=4 GOTOOLCHAIN=go1.27.3`, and create an owned run directory under `$PROJECT_TMP_ROOT/runs/after-run-recovery/`. Write an empty `package durable` file there. In an overlay's `Replace` map, supply **both logical and physical repository paths** for:

- `durable/continuation_prototype_main_test.go` → the empty compiler-only file;
- virtual `durable/after_run_recovery_oracle_main_test.go` → the pending oracle.

Run `go test -overlay <overlay.json> ./durable -run '^TestMainAfterRun(CrashFinishReasksDrainDoesNot|AnsweredFinishRestartLimit|ChildRestartLimitCompletesGeneration|LoweredLimitNormalFinishNoInterrupted|LoweredLimitNormalWorkerNoInterrupted)$' -count=1 -timeout=75s`. Do not repeat the recorded baseline wholesale while implementing the fix. No full suite, profiling, TypeScript/Vitest, CI, release/publication or full convergence is claimed.
