## Deferred polling and cancellation cutoffs

The pinned v1.5.0 poll API returns one terminal message. `generation.ts:332` awaits `models.fetchDeferred` with a signal and telemetry; request-phase streaming and partial commits use a separate path. Provider-channel deltas in Go must not become harness progress. The new checks pass without changing runtime behaviour.

Target: `f9a26365568127461c47a07a37dcb42c140a3ac5`. Tested runtime baseline: `3620c50a8bdc499fabc2c89642dea4342bcbe72a` (SQLite-only runtime `f284e25`). Test revisions: `ddf546c3c5a6f65fc21a5854e37c241eb7676618`, tightened at `d98f0c2798d6250cc6233841c1de3db6615d517a`. Tests live in `durable/harness_poll_boundary_main_test.go`.

### Three selected contracts

`TestMainAbortedPollCancelHandlerCutoff` ports the actual `harness-restart-failure.test.ts:671` scenario: abort a sleeping deferred job, lose the host twice inside a hanging cancellation callback, then fault its abort handler at restart limit 2. It checks real persisted restart counts, unanswered/aborted settlement, the exact error/detail, task ownership, fault outcome and `task_faulted/restart_limit` reporting. There is one request, no fetch and two cancellation calls.

`TestMainPollTerminalOnlyBoundaryDerived` is source-derived, with terminal and interrupted-fetch cases. The provider sends a text delta and a second event; receipt of the second witnesses consumption of the delta. Both harness and storage snapshots must lack a partial message. A terminal event produces only the terminal answer. A host loss followed by recovery with retries disabled produces interrupted settlement, one cancellation and no assistant entry. Neither case repeats the request or fetch. This covers Go's native channel adapter; f9 has no equivalent streamed-poll fixture.

`TestMainPollCutoffSkipsRepeatedCancellationDerived` is source-derived from `endCutOff` and its salvage boundary. A host loss during a real fetch reaches limit 1; cancellation then hangs and another host loss reaches count 2. Recovery skips further cancellation and settles the poll with restart-limit detail. Totals: one request, one fetch, one cancellation, no partial transcript.

The Go adapter uses a virtual clock, poisons the active Session and joins/closes the failed host before reopening the same SQLite file. Blocking Go callbacks return on context cancellation so they can join. These checks do not kill an OS process or establish universal power-loss behaviour. The native `failedRun` compatibility field is checked separately: policy failures retain it; explicit aborts clear it. Upstream run-failure reason, detail and ownership checks are preserved.

### Focused results

Commands source `scripts/project-env.sh`, with Go 1.27.3, `CGO_ENABLED=0`, `GOTOOLCHAIN=go1.27.3`, `GOMAXPROCS=4` and owned `runs/poll-recovery/20261103` scratch. Compiler overlays exclude the protected continuation prototype without editing it.

* Final three-function gate, `-count=3 -shuffle=42`: **9 top-level passes, 12 SQLite leaf passes, zero failures/skips, 1.034s**. Declared names and executed counts are checked. The preceding hardened gate passed in 1.089s; promotion passed all three functions in 0.355s.
* Four effective overlay faults reject five SQLite leaves: returning a delta as the terminal result (two cases), skipping cancellation exactly at the limit, repeating cancellation past it, and granting an extra abort-handler restart. The cancellation faults hit bounded wait/count/settlement assertions; no build failure or test-runner panic receives fault credit. Two incorrect scheduler mutation anchors were rejected before execution.
* Vet and Staticcheck v0.8.2 with tests pass for `./durable`. All 1,457 Go files match the source copy used by Staticcheck; the protected prototype is excluded. Formatting, diff and prototype-content comparisons pass.

The initial compile probe needed two bounded fixture delay casts. One fixture expectation incorrectly required native `failedRun` after explicit abort; inspection of the native settlement policy corrected it. Neither issue is a demonstrated runtime mismatch. The original next-step assumption about persisted polling-stream progress is withdrawn: the pinned poll API exposes no such progress to the harness.

No full suite, profiling, race/CGO, TypeScript/Vitest, hosted CI, release or asset publication ran. Dependencies, QuickJS in wazero, SQLite format, existing data and published assets are unchanged. One paired restart-failure crosswalk/review evidence entry changes; whole-file dispositions, flags, the 362/210 test corpora and the 428-path inventory do not change. Full v1.5.0 convergence is unverified.

Disposable source extracts, overlays, matching static-copy files and logs (`du -sh`: 14M), plus Go build cache (`du -sh`: 1.1G), are removed after analysis with no active owning job. The upstream source mirror, installed dependencies/toolchains, prototype, durable data and intentional assets are preserved.

Next: the actual after-run missing-definition/Stop-orphaning scenario at `harness-after-run.test.ts:785`; broader after-run inheritance, hooks, forks and foreign ownership still need reconciliation.
