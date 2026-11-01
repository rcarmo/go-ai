## Harness scopes and context retention

The three remaining new harness files at `81f3cd412758ebdd92324c7d63b669a189408d0a` have been reviewed through their final assertions. Four context-retention cases produced twelve native failures across memory, journal and SQLite; those failures are fixed. Scheduler overlap, incremental scope indexes and the off-line context-read protocol still need implementation or validation.

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

### Verification

Go 1.27.3/GOMAXPROCS=4, owned empty homes and project-namespaced scratch: full root normal tests pass with CGO disabled and enabled, 26 tested packages in each mode. Durable elapsed 112.202/118.160 seconds; these different workloads are not an optimisation comparison. Focused scope/context/timer/ownership checks pass three normal repetitions without CGO (shuffle 42) and three race repetitions with CGO (shuffle 7), including the [new queue/process-crash controls](durable-group-lifetime.md). Root vet passes in both CGO modes. Scoped staticcheck 0.8.2 reports only the existing usage_json.go:11 U1000 finding; the wider gate remains open. Raw baseline/verification logs and copied source excerpts were removed after analysis.

No acceptance profiling was performed. Full durable race, source scheduler/index parity, full semantic review and CPU/alloc_space/alloc_objects profiling are unfinished.
