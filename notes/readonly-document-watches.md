# Readonly document watch revisions

The public document watch now returns `ReadonlyValue` from Value and through Start/WatchFrame.Value. Detached mutable content requires Copy; no alternate mutable public watch remains. The existing mutable-frame transport is private and legacy content tests use explicit detached adapters. Assertion names, replay, authority, lifecycle, backlog, context and budget checks remain.

## Source authority and failures

Pinned target: `f9a26365568127461c47a07a37dcb42c140a3ac5`.

- `session-watches.test.ts:75` keeps acquisition revision until start, coalesces queued commits, installs Value before callback and shares the adopted revision. Its StateDoc has value0, items[a,b], retained.label=stable.
- New independent reference probes exercise those exact fields with two watches, synchronous observation, snapshots, value1/value2 commits and delayed starts. A second probe establishes attached ownership with zero idle retention. Both passed independently; native acquisition identity failed before the public readonly change, then zero-retention acquisition failed before active-node reuse.
- Raw native regressions live in the existing session_document_cache_test.go suite. They compare handles, unchanged children, initial retention, folded operations and callback-time Value; no copied-data normalization stands in for identity.
- The reference runner now also runs all28 unchanged session-watches tests:145 unchanged pinned tests plus12 probes pass. This is a reference receipt, not proof all145 assertions have native equivalents.

## Implementation

Transaction preparation seals one readonly revision per changed document before storage admission. Receipt/memory adoption, held values and each queued watch reuse it. Expensive structural replay is not repeated for every watch. Watch acquisition/drain maintain separate readonly current and observed revisions so an unstarted or in-flight watch does not advance prematurely. Retirement retains its preceding public value. Active readers supply authority on a cold cache fill at retention0, checking both schema and materialised content. Existing adapter-authority controls pass.

## Measured diagnostic tuning

The initial broad two-repeat run exceeded its240s package budget while progressing in Source202. A single-repeat watch/observer/progress slice passed181.436s. Targeted profiling then identified the concrete cost: each primitive set copied and validated the whole growing immutable tree twice.

Same Source202 workload, Go1.27.3 linux/arm64, CGO0, GOMAXPROCS2, -count=1 -p1 -timeout300s:

| Metric | Before | After |
|---|---:|---:|
| Test wall time |59.830s|2.933s|
| Sampled CPU |74.00s|3.34s|
| alloc_space |13,511.35MB|422.67MB|
| alloc_objects |101,666,175|5,311,501|

Readonly publication now reuses the existing exact primitive-object-set intermediate-budget accountant, then copies the changed immutable object once. Unsupported/oversized batches take the original independent-operation path. Differential guards cover replay, unchanged/old nodes and intermediate byte/node/member rejection. Their batches explicitly pass operation-array validation so budget controls reach the accelerated path. No budgets, deadlines or assertions were relaxed.

Remaining sampled costs are primarily record validation/ownership and operation decoding. Removing whole-graph revalidation without the still-open dependency/authority acceptance would be unsafe; this diagnostic does not credit full release profiling. Raw profiles, matching binaries and disposable logs were removed immediately by the wrapper. Only concise analysis remains.

## Verification and remaining scope

- Full durable405.984s/tools16.650s, repository-wide vet/Staticcheck and logging pass.
- Final raw identity and strengthened accelerated-budget controls pass30 shuffled repeats, seed42; only test-fixture strengthening followed the full run.
-145 unchanged source tests +12 probes pass; pinned source/test hashes and complete tree verified.
- Public stored-observer dispatch, exact cancellation/task-bound warm reads, version/cache-pressure/reload/weak lifetimes and complete assertion crosswalk remain open.
- A separate source probe clarifies builtin mounts: initial pi.agent borrows the loaded tracker, but after a configuration operation its changed branch is a separate immutable replay, not the same object as a snapshot. Unchanged pi.live remains shared. Native warm/cold mount hydration still needs this source-specific acceptance; do not force universal cross-surface identity after edits.
