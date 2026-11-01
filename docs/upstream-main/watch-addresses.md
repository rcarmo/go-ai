# Definition watches follow addresses

`WatchDefinition` now survives retirement and follows later incarnations at its acquired address. The target is upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`.

## Runtime change

The watch stores the scope, owner, kind, family flag and key, alongside its current incarnation ID. Retirement releases that ID and its cache pin. Recreation transfers the pin to the new incarnation without reacquiring the watch or rerunning the original definition's migration. A changed incarnation or value version is delivered as a root replacement.

`DocumentWatch.StartFrame` accepts `func(context.Context, WatchFrame) error`. Deliveries have `Kind: "value"` and a `Retired` flag. Retirement alone delivers the preceding committed value with no operations; a mutation in the retiring commit is suppressed. Retirement and recreation in one commit produce one delivery with the recreated value, a root replacement and `Retired: true`. Later updates have `Retired: false`.

`Start` remains the value/operations callback view, with the same callback signature. Its retirement behaviour intentionally changes: it receives the preceding value with no operations and stays open. Callers that need to recognise retirement must use `StartFrame`. Watch endings remain available through `Closed` and `End`; retirement no longer produces a `"retired"` ending.

Preparation still copies values and operations and checks payload bounds before storage settlement. Failed commits do not advance watches. Callbacks remain serial and off the Session line; public values do not alias watch-owned values. Queue overflow retains its bounded root-replacement policy and carries forward any pending retirement flag. Consumed queue slots are cleared so their backing arrays cannot retain earlier incarnations. The former acquisition-value field has been removed.

## Pending operation folding

Definition watches now take the pending batch at the next callback boundary and fold it off the Session line. Acquisition values stay fixed until `Start`; changes made while a callback is running enter the next call. A kept-up listener receives its single frame unchanged. The folded call uses the newest value, sequence and producer context (without producer cancellation), and retains any pending retirement flag.

The pinned `packages/chord/src/delta/pending.ts` rules apply: repeated primitive sets on one path replace earlier leaf operations; string appends join; appends after string sets join into the set. Structural operations clear the leaf index. Root replacement discards earlier operations. Paths use JSON-encoded arrays, so `['a.b']` cannot collide with `['a', 'b']`. Explicit operations that restore an earlier value remain operations; folding does not suppress equal-value results.

The 100-value threshold belonged to upstream `NativeWatch`, not its JSON `Watch`. It no longer resets definition watches. Commit subscriptions keep their separate overflow contract. Pending frame bytes remain bounded, and folded operations are revalidated before callbacks. A folded batch exceeding native operation, DTO or byte limits becomes a bounded root replacement; an oversized root ends the watch with `budget_exceeded`. Per-commit preparation and storage settlement are unchanged; no host work is added to publication.

Native limits remain a visible difference: pending operations must satisfy the existing 4,096-operation delta replay cap and configured `MaxMembers`, whereas upstream `PendingOps` switches to replacement after 10,000 folded operations. The native queue also bounds retained detached frame bytes before folding, so it can replace earlier than upstream even when later folding would shrink the operations. These limits are preserved, not accepted as full parity. Incremental pending storage and the source overflow domain remain open.

`durable/watch_pending_main_test.go` covers delayed start, a blocked callback, 102 repeated sets, pending retirement and recreation, latest context without cancellation, leaf/append folding, distinct path keys, structural barriers, root replacement, independent replay, input immutability, the native operation ceiling and quota refusal. Integration cases run on memory, journal and SQLite. Existing observation tests now expect one folded set instead of ordered pre-start callbacks or a 101-frame reset.

Focused normal tests (`TestMainWatchPending|TestMainWatchAddress|TestObservation|TestMainDefinitionWrapper`) pass three repetitions with shuffle 42 in CGO 0 and 1; the same focused race selection passes three repetitions with shuffle 42. An earlier selection before the final recreation test also passed race shuffle 7. Durable vet passes in both modes. Restoring the previous production drain/100-frame policy through a logical-and-physical-path overlay fails all nine pending-delivery backend cases. Raw source extracts, the diagnosed published-package probe and mutation artifacts are removed after use. No full-suite or full-race gate is claimed for this checkpoint.

## Address-source crosswalk and earlier checks

- `packages/durable/src/session/session.ts:1444–1537`: address matching, retirement values, version/incarnation replacement and incarnation pins.
- `packages/durable/test/session-watches.test.ts:369–414`: suppress retiring mutations; combine retirement and recreation in one commit.
- `packages/durable/test/session-watches.test.ts:678–720`: recreation at a new definition version, followed by incremental updates.
- `durable/watch_address_main_test.go`: separate/same-commit recreation at unchanged/changed versions across memory, journal and SQLite; delayed start, retirement tags, root/incremental operations, pin transfer, consumed-slot clearing, detached callback values, absent acquisition, empty family keys, unrelated-key isolation, rollback and Session close after retirement.
- `durable/observation_test.go`: the earlier terminal-retirement assertion now checks address following through the compatibility callback.

Focused normal tests for address watches, observation and adapter-definition authority pass five repetitions with shuffle seed 42 in CGO 0 and 1. Focused race tests pass three repetitions with seed 42; an earlier run before consumed-slot clearing also passed seed 7 with task close/queued controls. `go vet ./durable` passes in both CGO modes. Restoring ID-only matching through a production-source overlay fails all 12 recreation backend cases. An initial overlay probe missed Go's logical package path; the corrected probe covered both logical and physical paths. Disposable probe output is removed.

## Still open

This is partial watch alignment. Exact source overflow/work bounds, task-owner terminal detachment, synchronous observer semantics, transcript windows and removed state/event APIs still need review and implementation. The pinned watch tests remain only partially mapped; native callback scheduling does not establish JavaScript microtask equivalence. Native detached values differ from upstream shared JavaScript references. Clearing queue references and transferring pins do not establish the upstream garbage-collection test's full lifetime contract.

Commits remain serial; this change enables no writer overlap or grouping. The continuation prototype is excluded. Full semantic acceptance and pre-release profiling remain open. Further validation for this slice is focused under Rui's direction; no new full durable-race result is claimed.
