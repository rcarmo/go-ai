# Restart cutoff checkpoint

Answered finish/drain recovery now completes the generation, clears its live run without creating `runFailure`, retains queued inputs and reports `task_interrupted/restart_limit`. Ordinary tasks fault before definition resolution at the limit. Generation, tool and compaction retain their own cutoff handlers. Full v1.5.0 convergence is unverified.

Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Runtime: `e5d73933dd1661ffa6f889de58848889095f08e6`. Base: `fe53bfc0ba75ef19c188630a4ba7fea40b806927`. The [unchanged recovery baseline](after-run-recovery-baseline.md) was not repeated.

## Source assertions

The five recovery functions are promoted, without changing their assertions, to `durable/harness_after_run_recovery_main_test.go`:

| Source `harness-after-run.test.ts` | Native suffix after `TestMainAfterRun` |
| --- | --- |
|429 crash portion|`CrashFinishReasksDrainDoesNot`|
|757|`AnsweredFinishRestartLimit`|
|955|`ChildRestartLimitCompletesGeneration`|
|983|`LoweredLimitNormalWorkerNoInterrupted`|
|1019|`LoweredLimitNormalFinishNoInterrupted`|

Three narrowly coupled controls were translated before changing dispatch, in `durable/harness_builtin_cutoffs_main_test.go`:

| Source `harness-restart-failure.test.ts` | Function suffix after `TestMainCutoff` | Checks |
| --- | --- | --- |
|330|`GenerationDuringPreparation`|One blocked preparation, no provider call, unanswered input, cutoff reason/detail, failed outcome, run-failure identities and first entry, attributed report/count.|
|367|`SafeToolKeepsStoredOutput`|Two real crashes, two executions, stored partial prefix, failed error receipt/diagnostic/outcome, parent answers, attributed report/count.|
|438|`CompactionPublishesFailure`|Two prior turns, interrupted summary, failed outcome, compaction failure, cleared active status, no redispatch, attributed report/count.|

Memory, journal and real modernc SQLite run all eight functions with CGO disabled. Host loss uses the existing failed-Session/cancel/join/reopen adapter; no task checkpoint or restart counter is fabricated in these integration fixtures. The tool fixture uses a real dependent-read barrier to confirm its output is stored before each crash. Its native tool is named `work`; model responses use the existing native faux helper. Native `task.pi.compaction`, live failure `reason`, report `Type`, receipt roles and typed outcome envelopes adapt source names/shapes. Native live state still includes its existing `failedRun` queue gate; source330's exact whole-document shape is not established. Complete hook/capability and public DTO fidelity need separate review.

The new controls initially failed all **nine backend leaves**, three top-level failures, in **36.789s** against unchanged production. Each exhausted its explicit wait and independently failed outcome/status/report checks. Draft fixture compilation errors receive no failure credit. Later fixture corrections used the real APIs and normalised expected numeric IDs; no runtime change was made to satisfy those adapter errors.

## Implementation and native controls

Scheduler1933 faults ordinary exhausted work before resolving its definition. The native implementation follows that ordering and keeps abort-handler cutoffs faulted. Explicit new abort marks reset the prior run-phase count, as source `withAbortMark` does. Retry or a raised limit can still act before resume. Startup no longer emits the obsolete `task_parked` report for exhausted work. Task inspection and graph blocking retain definition-fit reasons; wake includes an ordinary cutoff even when its definition is absent.

Generation entry checks the limit before re-asking after-run hooks. Answered cutoff uses a validated private `KeepQueued` receipt hold; deterministic cleanup also preserves it if owned work delays finalisation. Normal completion after the host lowers its limit does not report an interruption. Unanswered cutoff settles with `restart_limit`; source-specific partial/deferred salvage still needs its own complete phase assertions.

Safe-tool cutoff retains stored output and adds the source diagnostic and failed outcome detail. Validation requires matching error reason and receipt diagnostic. Compaction phase wrappers decide failure before another request and use the existing atomic compaction-status projection. Builtin interruption reports carry the recovered count; ordinary fault reports retain their source shape.

`restart_cutoff_150_test.go` adds a real host-loss/missing-definition wake/fault control across all three backends and native receipt-envelope rejection cases. Obsolete parking expectations in the existing restart-limit and task-graph tests were reconciled against source scheduler1933, `blockedReason`486 and `withAbortMark`3022. Graph coalescing now uses definition-fit changes; count-only edits do not change its public projection. The older seeded recovery helpers remain native controls, not translated crash-fixture evidence.

## Focused verification

All commands source `scripts/project-env.sh` and export `CGO_ENABLED=0 GOMAXPROCS=4 GOTOOLCHAIN=go1.27.3`. Compiler overlays exclude only the protected untracked continuation prototype, using both logical and physical source paths. Disposable paths stay below the project-owned run root.

* Eight recovery/cutoff functions: count3, **24 top-level passes**, no failures/skips,5.132s before the remaining native reconciliation.
* Final **27-function** selection: count3/shuffle42, **81 top-level passes**, no failures/skips, **17.042s**. It includes the eight oracles, reconciled host/graph controls, envelope/missing-definition controls, retry policy/poll/budget, tool crash/output recovery, conversion atomicity, abort-cutoff projection and both close/requeue-budget controls. Name/count checks reject empty or misspelled selections.
* After promotion to ordinary tests, the same27-function selection passes once more:27 passes/0fail/0skip,5.936s.
* Durable vet and Staticcheck **v0.8.2**, including ordinary tests, pass. Staticcheck uses an owned physical source copy excluding the prototype. Its first run found the startup-only `taskInstaller` helper unused after removing parked reports; that helper was removed.
* Seven effective logical+physical mutation overlays each fail all **three backend leaves**, with no compile failure, skip or global test timeout: generic builtin faulting, consuming the answered queue, discarding the tool prefix, omitting the report count, rerunning an ordinary child, hiding missing-definition wake, and reporting a normal finish after lowering the live limit. An earlier late-report mutation saw an already-reset count and passed; it receives no credit. Moving that fault to the post-hook/pre-progress point fails all three leaves.

The initial new missing-definition control used an older reopen helper that cannot open SQLite. It received no runtime-failure credit and now uses actual host loss and the production SQLite reopen path. Fixture errors, the ineffective mutation and static diagnostic were analysed before their disposable output was removed.

## Remaining work

Whole-file after-run and restart-failure dispositions stay `pending-review`. Source429's orderly close in drain, missing-definition orphaning outside the cutoff, history/compaction/failure inheritance, hook deselection, forks and foreign ownership remain open. Cutoff tool `afterTool`/limit hooks, at-versus-past-limit cleanup, generation partial/deferred salvage and all phase-specific contracts are not validated by these three new controls. At runtime e5d7393 the early tool-cutoff path still bypassed result hooks. The later [result-hook checkpoint](cutoff-result-hooks.md) ports the existing source584 scenario and fixes selected at/past-limit and normalization contracts. Native live/receipt/compaction DTO adapters need complete fidelity review.

No full suite, race detector, profiling, TypeScript/Vitest execution, hosted CI, dependency/guest update, release or asset publication ran. SQLite and QuickJS-in-wazero dependencies are unchanged. Native v1.4.0 still peels to `ee3076b3659697f1de01b87da964396e069a4389`. The prototype remains in place and content-compared; 28MB of temporary source extracts, compiler/static inputs, mutations and logs plus1.6GB of completed Go build cache were removed after analysis, with no active Go/compiler/static jobs. Full release acceptance needs Rui's broader-validation authority and the remaining source contracts.
