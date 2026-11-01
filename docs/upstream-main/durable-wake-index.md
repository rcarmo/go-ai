# Active native Wake index

Native Wake reads with an actual invocation running now visit a published sleeping-task index instead of all task records. Known activity also skips cancellation scans whose only result would be to set `Active` again. Exact native memory, journal and SQLite stores use this path. Wrapped Snapshot data and errors remain authoritative through the full read path.

The target remains `81f3cd412758ebdd92324c7d63b669a189408d0a`. Its `packages/durable/src/harness/scheduler.ts` derives activity from invocations, reconciliation and runnable candidates, then reads its sleeping set. The native fast path currently covers activity already established by an invocation. Without an invocation, native runnable/reconciliation discovery still scans all task records. Full source work-bound parity remains open.

## Publication and lifetime

Construction seeds the index from the native Session image before starting the scheduler loop. Normal publication updates it in the existing task-adoption write loop, using the final immutable adopted record for each changed task ID. Repeated writes use the final record; terminal transitions remove it. Entry-only publications advance its seq without rebuilding or cloning tasks.

A non-contiguous/private image adoption invalidates the seq witness; the next indexed read rebuilds from the Session's published image. Cold construction/rebuild is O(all tasks), normal maintenance is O(task writes), and active indexed reads visit O(sleepers). Due-sleep eligibility can still consult dependency/registry state. The index retains one task value per sleeper. Close clears it after real host joins. No queue, preparation overlap or group publisher is enabled.

A public future deadline and the internal alarm can share one fresh immutable scalar when their minima coincide. A due alarm keeps its separate earlier deadline. Public deadlines and callback values remain detached from persistent records and retained publisher state.

## Diagnostic measurement

Same host and fixture, linux/amd64, Go 1.27.3, `GOMAXPROCS=4`, CGO enabled. `BenchmarkMainWakeActiveHistory` holds a real invocation alongside 4,096 terminal records and one future sleeper; setup precedes the benchmark timer. Three two-second runs compare baseline `aefdd50` plus the benchmark with the implementation:

| Path | ns/op, three runs | Bytes/op | Allocs/op |
| --- | --- | --- | --- |
| Original scan | 315535, 285653, 297714 | 773, 776, 1749 | 11, 11, 19 |
| Index, separate deadline scalars | 143.4, 142.2, 143.7 | 16 | 2 |
| Index, shared coincident scalar | 146.6, 144.2, 137.0 | 8 | 1 |

Median latency falls from 297,714 to 144.2 ns/op: about 297,570 ns, or 99.95%, for this active-read workload. Baseline allocation measurements are noisy; the final three runs consistently report 8 B/one allocation. Sharing the scalar halves the indexed allocation cost; its 0.8 ns median latency difference is within the overlapping run ranges. No overall write-throughput or full-suite speedup is inferred.

Five-second CPU, `alloc_space` and `alloc_objects` profiles were inspected for all three stages. Initially `readWake` accounts for 75.85% cumulative CPU; nested fail-fast cancellation scans account for 64.21%. The index and known-active short circuit remove that repeated work. The intermediate allocation profile identifies `earlier` as the remaining read allocation source; sharing coincident minima removes one of its two scalars. Final CPU is spread over necessary admission/locking/map work (`readWake` 24.16% cumulative). The remaining fresh public deadline owns its lifetime and cannot safely alias persistent task state.

Allocation profiles also include fixture construction and benchmark calibration (`encodeBoundedOrder`, candidate tables and strict preparation). Different iteration/calibration counts make aggregate profile byte/object totals unsuitable for a before/after query comparison. Bulk candidate validation and publication-maintenance costs still need broader pre-release measurement; this diagnostic does not close that work. Raw captures and all matching binaries were deleted immediately after the current comparison. Only these conclusions remain.

## Checks

`wake_index_main_test.go` adds seven CGO/five non-CGO leaves: final repeated task writes, entry-only publication, terminal removal, private adoption rebuild, Close release, active wrapper data/errors, and distinct due-alarm/future-deadline minima. The publication test deliberately uses internal validated staging to isolate bookkeeping; public raw managed-task replacement remains forbidden. The wrapper fixture uses a real host. No invocation is fabricated.

New index checks plus Source Wake and existing Wake/sleep controls pass ten normal repetitions per CGO mode and shuffle seed 7/42, and five race repetitions per seed. Removing incremental maintenance fails three backend cases; forcing wrapper indexing fails the active adapter case; removing Close cleanup fails three backend cases. All overlays and logs are disposed.

Fresh full root normal/vet/staticcheck v0.8.2 pass with CGO disabled/enabled; module verification passes. The final full durable race suite passes in 609.812 seconds with 4,109 completed named test/subtest pass events, zero failures and zero unfinished tests (`-race -json -count=1 -timeout=1200s`). Raw events and empty owned homes are disposed. This replaces the earlier full-race checkpoint; different test workloads prevent a suite performance comparison.

No exported API, dependency, storage format, asset or release ref changed. Native read health, wrapper authority, cancellation, ownership, serial Session callbacks/commits and Close joins remain enforced. Inactive/runnable Wake discovery, incremental idle scopes, wrapped publication projection, keyed dependencies, group failure/continuation contracts and full semantic acceptance/pre-release profiling remain incomplete.
