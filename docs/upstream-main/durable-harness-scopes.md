## Harness scopes and context retention

The three remaining new harness files at `81f3cd412758ebdd92324c7d63b669a189408d0a` have been reviewed through their final assertions. Four context-retention cases produced twelve native failures across memory, journal and SQLite; those failures are fixed. Scheduler overlap, ordered expiry and the off-line context-read protocol still need implementation or validation. Published membership and target enumeration use the [idle forest](idle-index.md).

The lexical declarations are one in `harness-context-range.test.ts`, seventeen in `harness-group-commit.test.ts`, and fourteen direct declarations plus two tables in `harness-idle-scopes.test.ts`. Tables and internal loops expand work/cases; these counts are not executed source test counts.

### Off-line context reads

The 47-line context-range test pauses an entry scan after it captured a page. An intervening commit makes that range non-current. Advancing it with a later publication must leave it unchanged; the next read extends from storage and includes the missed entry. Native `Session.ContextView` takes Session admission and derives from an immutable snapshot; it has no equivalent off-line `readContextFrom` surface. A future asynchronous reader must retain a version witness and invalidate a scan raced by publication. The source row is reviewed-open.

### Scheduler regions and admission

The harness group file requires these boundaries before Session overlap:

- Completion decisions wait for admitted creations below owned conversations and ended/pending-end ancestors. Membership reservations reach the live owner and global roots even when an ancestor currently appears live or is ending. Chain-load nodes stay pinned through a sweep until the unpublished creation settles; publication releases those pins.
- An unpublished owner abort mark stops the child's next phase. Opposite waits admitted in order refuse the second as a cycle. Reservations publish before invocation starts; successful and failing migrations run/report once after any pending write they depend on.
- Cascaded abort and fail-fast marks copy the current record after a pending checkpoint change, preserving the newer checkpoint. A prepared copy cannot overwrite that change.
- Three hundred child outcomes remain in memory even beyond the record-cache count. Native owned-image reads avoid bulk copying, but do not establish the source point-read cache/wait-pin contract.
- Pending submissions with the same request ID deduplicate. A task-wait continuation runs before the next part publishes. Close signals the invocation while storage is held, waits for its call, and reopens the stopped task as pending with its entry retained.

Native's serial Session line does not provide source disjoint preparation or the keyed scheduler regions. Existing ownership/idle/submission/close tests are related controls, not a completed translation of these seventeen cases. The source row is reviewed-open. Continue under the [group publication contract](durable-group-contract.md), including the unresolved Go continuation boundary.

### Idle scopes and expiry

Source scope membership follows owned task/conversation chains through ended tasks, excludes background work at its boundary and treats unloaded chains conservatively until reconciliation. Unrelated conversation waiters settle independently. The context-retention state uses that same scoped idleness.

Native `contextBusy` previously considered only direct nonterminal foreground tasks. A pending task in a conversation owned by an ended root task therefore counted in root idle work but not in root context retention. It could discard the busy root context at zero retention. `idleTaskInScope` now supplies one predicate for idle targets and context busy checks, retaining failed-input exceptions, actual invocations and background boundaries.

Expiry now checks an existing idle deadline before a transition to busy. Every successful Session publication settles context retention after releasing the core line. This also expires a range at an entry-only publication without a timer callback. Strictly entry-only batches reuse busy witnesses; all other writes refresh them, including live-document changes that can affect held-generation idleness. Public values remain detached and clock arithmetic/bounds are unchanged.

`context_scope_main_test.go` covers four cases on all three backends: ownership-scoped busy membership, zero retention with a foreground descendant, an expired idle range becoming busy, and an entry-only publication expiring a range. Fixtures use validated legacy records, an atomic clock and accepted virtual timers that do not fire. The baseline at `0c61a30` fails all twelve leaves. No fake host invocation or real project state is mutated.

### Remaining idle work

Source also checks no-op reconcile expiry, oldest-idle ordering with backward clocks, dynamic shorter/longer/zero retention, unchanged idle expiry on reread, immediate removal after a non-extendible head change, and a 200-step seeded comparison against a full idle recomputation. Native still scans ranges/task metadata and has no equivalent incremental-idle mismatch/full-check counters. The source's zero full-check assertion is unfinished work.

The Wake return-value crosswalk is now complete for the seven table rows and the running-invocation case described below. [Active native Wake indexing](durable-wake-index.md) now visits published sleepers when a real invocation already establishes activity. Without an invocation, runnable/reconciliation discovery still scans all tasks; wrapped Snapshot paths remain authoritative. Full sleeping-only work-bound parity remains open; these behavioural assertions alone do not establish it. The idle-scopes source row remains development-verified-partial for the four retention cases and these Wake values.

### Wake assertion crosswalk

At `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/durable/test/harness-idle-scopes.test.ts:514–590` checks seven Wake rows and a running invocation. `durable/wake_source_main_test.go` preserves their values at clock 1000:

| Source case | Native expectation |
| --- | --- |
| Sleeping until 5000 | Inactive, at 5000 |
| Another sleep at 3000 is abort-marked | Active, at 5000; abort work is not a future sleep |
| Sleep at 500 is due | Active, no future deadline |
| Task waits on the live sleeper | Inactive, at 5000 |
| Completing owner holds a sleeping child | Inactive, at 5000 |
| A pending task lacks a definition | Inactive, at 5000 |
| Pending work has a definition | Active, at 5000 |
| Real invocation runs, then returns | Active then inactive, at 5000 in both states |

The native implementation already passed these values; no production fix was needed. Minted IDs, the native execution envelope and phase name `work` replace the source fixture's IDs/state shape. The completing owner uses a validated native completing outcome. Raw records are validated before opening Harness; public creation/final-owner admission fences are unchanged. Paused table cases also check that Wake neither invokes a host nor advances storage seq.

The running case uses a real blocked task host. A fixture probe incorrectly used `WaitForIdle` after its outcome: native idle waiting also waits for the future sleeper, so the probe timed out. The corrected test waits for the observed inactive Wake transition after `WaitForTask`, preserving the distinction between outcome publication and host retirement. It does not advance the clock, invent an invocation or weaken idle waiting. The failed probe's remaining journal fixture was removed after confirming no durable test process was active.

All twenty-four CGO/sixteen non-CGO leaves pass twenty normal repetitions per mode and shuffle seed 7/42. The new crosswalk plus existing Wake/sleep controls pass five race repetitions per seed. Removing the future deadline or treating the abort-marked sleep as a future deadline fails three backend leaves each; overlays/logs were deleted after analysis. Fresh full root normal/vet/staticcheck v0.8.2 pass in both CGO modes, and module verification passes. Empty test homes are disposed. The full durable race result at `112f451` predates this test-only increment. No API, production, dependency, format, asset or release changes; acceptance profiling and indexed Wake/idle work remain incomplete.

### Idle expiry during reconciliation

`context_idle_main_test.go` checks the expiry values in `harness-idle-scopes.test.ts:297-371` at the pinned revision: a no-write reconcile pass expires retained ranges, clock rollback preserves each range's original idle timestamp, zero retention removes even ranges idle after the current clock, and rereading a range does not extend its expiry. Two native controls shorten and lengthen retention through `SetSettings`. Virtual timers never fire. These checks cover retained values; ordered-index work counters and the 200-step model still need validation. Captured-host [timer checks](#context-expiry-timers) cover scheduling separately.

A genuine admitted reconcile pass now settles context retention before selecting task work. Previously a pass with no writes returned before publication's settlement hook, leaving expired ranges cached. Restoring that scheduler code fails twelve backend leaves across no-op, rollback, longer-retention and reread cases. Shorter/zero-retention controls already passed because settings updates settle retention directly.

Context membership uses the published image and actual hosts under the Session line. Its extra raw-core wait has been removed: a later unpublished writer cannot change those records. A held real append test fails on all three backends with the old wait restored. Public read admission, storage health, ownership, wrapper Snapshot data/errors and serial publication are unchanged; focused native-health, wrapper and Session failure controls pass under race detection.

The new tests have twenty-one CGO/fourteen non-CGO backend leaves. Focused normal checks pass ten repetitions per mode (shuffle 7); context/cache/Wake race checks pass five repetitions (shuffle 42), and context/health/wrapper/failure race checks pass five repetitions (shuffle 7). Full root normal tests, vet, staticcheck v0.8.2 and module verification pass in both modes with Go 1.27.3/GOMAXPROCS=4. Baseline overlays, logs and empty homes were removed after analysis. The full durable race run passes in 626.962 seconds: 4,138 completed named passes, zero failures and zero unfinished tests. Seven opt-in Codemode process tests skip without the exact worker installation; that integration gate is still open. Its events and empty homes were removed after analysis. This replaces the earlier development race checkpoint; workloads differ. No performance comparison or acceptance profiling was performed. Overlap stays disabled; indexed expiry, off-line reads and broader semantic convergence are incomplete.

### Context expiry timers

Context expiry now follows `scheduler.ts:991-1005`: settlement retains an accepted alarm for the same absolute deadline, even when the oldest idle range and retention components change. If a timer fires after clock rollback, the next delay includes the gap back to that range's idle timestamp. A deadline overflow bit prevents distinct signed-int64 sums from aliasing; delays saturate at Go's maximum duration before addition or multiplication can wrap.

An optional background timer declined by the host is retried on a later settlement. An admitted arm still returning from its host hook is retained, including reentrant context reads. Completion and cancellation state are read under the timer's short mutex; host hooks run outside it and the Session line. A fired callback checks its exact timer identity on the Session line before retiring it and recomputing expiry. A stale admitted callback cannot discard a replacement. Existing one-time cancellation and Close draining remain intact.

`context_timer_main_test.go` has eight captured-host scenarios (twenty-four CGO/sixteen non-CGO backend leaves), plus 1,005 arithmetic vectors checked against independent arbitrary-precision subtraction and duration saturation. These include unchanged alarms, rollback/early fire, equal deadlines with changed components, settings/fire/late callbacks, declined retry, positive overflow and capped rearming, stale admitted callbacks, reentrant arms and Close. Restoring old cache scheduling or removing gap arithmetic, declined retry, pending-arm retention or callback identity is rejected by five mutation controls; overlays/logs were removed after analysis.

Focused normal tests pass ten repetitions per CGO mode (shuffle 7). Context/timer/native-health/wrapper races pass five repetitions per shuffle seed 7/42. Full root normal tests, vet, staticcheck v0.8.2 and module verification pass in both CGO modes with Go 1.27.3/GOMAXPROCS=4; isolated homes/logs are removed. The full durable race run passes in 628.220 seconds with 4,172 named passes, zero failures, zero unfinished tests and seven opt-in Codemode process skips. Its events and homes were removed after accounting. The preceding 626.962-second run has fewer tests; these times are not a performance comparison. No acceptance profiling was performed. Ordered-index work bounds and broader convergence are incomplete; the nested-work workload has separate functional coverage below.

### Nested work model

`idle_model_main_test.go` ports the 200-operation workload in `harness-idle-scopes.test.ts:436-511` with seed 42. Its generator preserves JavaScript Number rounding, checked against eight states from the pinned generator; an integer LCG would select different operations. Conversation and live-task collections retain source insertion order.

The test uses registered, gated task hosts and public creation/completion APIs, including background work, task-owned children, owned conversations and work admitted through ended owner chains. Typed user-message entries keep native context ranges populated; each named gate carries its immutable requested context read. Host-start and return barriers replace the source's event-loop quiet helper. A Session-line observation compares every conversation and the global idle target set with an independent downward ownership-graph traversal, then checks cached idle markers without running a repair/expiry pass. Production traverses owners upward.

The initial run on all three backends checked every step: 24 ownerless conversations, 74 direct work tasks, 22 owned-work creations and 80 gate releases, with no skipped-owner iterations. It retained 29 context ranges with mixed busy/idle state. Source assertions require more than five ranges and both states; the test preserves those inequalities. Four mutations fail all three backends: background leakage at loop index 6, omitted owned-conversation traversal at 11, dropping ended-owner connectors at 18 and retaining a stale idle marker at 4. Overlays and logs were removed after analysis.

The workload passes three normal repetitions per CGO mode and shuffle seed 7/42, plus three race repetitions per seed. Full root normal tests, vet, staticcheck v0.8.2 and module verification pass in both CGO modes. The full durable race run passes in 657.832 seconds: 4,177 named passes, zero failures, zero unfinished tests and seven opt-in Codemode process skips. Events, status and empty homes were removed after accounting. The prior 628.220-second run lacks these model tests; no suite-speed comparison was made.

The [published idle index](idle-index.md) now supplies constant-time conversation/global busy queries. The model checks every indexed value, one initial build and zero node visits during warm queries. Native idle waits and bound-wait refresh use active-branch target enumeration with an explicit published-read witness. Wrapped and private candidate views retain the original `idleTargets` scan. Internal task-root pin/lifetime equivalence, ordered expiry, off-line reads and the complete Source `fullIdleChecks == 0` contract need separate work. Query-focused CPU/allocation comparisons are recorded with the index; full release acceptance profiling is incomplete.

### Earlier verification

Go 1.27.3/GOMAXPROCS=4, owned empty homes and project-namespaced scratch: full root normal tests pass with CGO disabled and enabled, 26 tested packages in each mode. Durable elapsed 112.202/118.160 seconds; these different workloads are not an optimisation comparison. Focused scope/context/timer/ownership checks pass three normal repetitions without CGO (shuffle 42) and three race repetitions with CGO (shuffle 7), including the [new queue/process-crash controls](durable-group-lifetime.md). Root vet passes in both CGO modes. Scoped staticcheck 0.8.2 reported only the then-existing usage_json.go:11 U1000 finding; [current root checks](staticcheck.md) pass. Raw baseline/verification logs and copied source excerpts were removed after analysis.

That earlier run did not include acceptance profiling or a full durable race run. Source scheduler/index parity, full semantic review and CPU/alloc_space/alloc_objects acceptance profiling are unfinished.
