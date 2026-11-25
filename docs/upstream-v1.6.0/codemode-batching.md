# Codemode journal batching

The complete pinned case at `packages/durable/test/codemode.test.ts:913–946` returns `50` and requires fewer than ten journal publications. The [native published-worker comparison](../../durable/codemode_worker_concurrent_results_v160_test.go) returns the same value after all 50 deliveries. Round 47 reduced the count from 103 to 54 in both lane repeats. The upstream limit is still unmet.

## Where the protocols differ

The pinned host's `pump` in `packages/durable/src/codemode/codemode.ts:952–999` puts completed results and pending acknowledgements into one commit. It delivers those results after the commit and uses the committed counts to decide which new calls may launch.

Go's [parallel loop](../../durable/codemode_parallel.go) commits a delivery before passing it to the guest. The [drain handler](../../durable/codemode_drain_commit.go) can now commit a non-final drain and one ready result from an already-admitted call together. The host still waits for the commit before acknowledging the drain or delivering the result. A [real SQLite cutoff test](../../durable/codemode_parallel_fusion_v160_test.go) verifies one publication before acknowledgement and exact replay without repeating either callback. The [journal validator](../../durable/codemode_integrity.go) still permits a missing count only on the last delivery of an unsettled run. A single commit cannot yet add several deliveries with counts still unknown.

## Requirements for a batched path

A batched path would need an explicit policy pinned on new saved starts. Older records must keep their current validation and replay behavior. These are requirements for a possible implementation, not changes to the current storage format:

- Only include outcomes of calls already admitted by a committed count. Persist every exact value, error and call identity before delivering it.
- Represent a bounded set of unacknowledged deliveries explicitly. Check identity and cumulative counts in guest order. Until the corresponding acknowledgement commit is durable, do not launch newly issued effects.
- Handle a script ending with part of a committed batch undelivered. Successful completion must distinguish the delivered prefix from unused results without deleting or rewriting a committed record.
- Preserve the configured write, document, page and output limits; batching must not create special exemptions for this fixture.

## Checks before adoption

Use real published guests and writable SQLite reopens. Cut off after batch commit before delivery, between deliveries, and after the last delivery before acknowledgement commit. The recovered run must replay exact saved values and must not repeat a completed effect or admit a fresh one on divergence. Also check terminal drain and abandonment, rejection of an invalid saved count, and old-start replay. Only then compare the unchanged 50-result case's publication count and final value. A lower observer count alone is insufficient if it comes from moving writes out of the observed journal.
