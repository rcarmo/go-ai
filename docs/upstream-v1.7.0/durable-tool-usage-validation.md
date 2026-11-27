# Durable tool usage validation

This is native-only validation for the pi v1.7.0 Durable port, not a claim of upstream test parity. The candidate applies to base `15d9040d37157d03fc739d82b89d8a9ad25aa5be` and the pinned source `fb0fe823539f6d04850f6e34748c01f01f212e1f`.

## Boundary and fix

The native `ToolExecutionResult` has a Go `Commit` callback, which pi's `tool.ts` result does not have. Canonical tool settlement must therefore validate a detached contribution's usage, then validate and stage aggregate usage in the current transaction before invoking that callback. A failed callback rolls the staged usage, receipt and application writes back together. Before this ordering fix, invalid finite usage or an aggregate overflow could reach the callback; SQLite rollback removed the write but could not undo callback execution.

`settleCanonicalTool` now rejects an individually invalid usage value before transaction work and moves the aggregate usage read/add/write ahead of the callback. Receipt creation and final task state follow in the same transaction. Valid results still retain the existing native settlement contract, including the separate Go case in which a result is paired with an executor error.

## Native regression

`TestToolUsageValidationFencesApplicationCommit` exercises twelve cases through the public harness and a real SQLite store. It checks invalid individual counters, invalid image-output presence, invalid cost, non-finite cost, aggregate counter and cost overflow, a valid aggregate, callback rollback, no callback, and paired result-plus-error handling. Every case verifies invocation counts, callback/write fencing, attributed receipts, the usage ledger, terminal outcome, ownership and reopen/request identity. The paired result-plus-error case deliberately preserves the current failed-outcome, billed-usage and callback behaviour; it is not a green assertion for an upstream-only policy.

Two narrower pre-fix controls isolate the same ordering defect: `individual-usage.out` catches a valid-shape individual cost rejection occurring after the callback, and `aggregate-order.out` catches both aggregate overflow paths after callback execution.

## Preserved evidence

Validation used Go 1.27.3, the project environment, `CGO_ENABLED=0` and the shared project lock. The receipt directory is `/tmp/go-ai/runs/durable-port/m1c-usage-validation.HLNq7a` (selected by `/tmp/go-ai/m1c-usage-validation.current`):

- `closest-original.out`: the closest pinned original passed unchanged.
- `go-public-red.out`: unchanged native production reproduced six callback leaks.
- `go-public-green.out`: all twelve public/native cases passed twice.
- `individual-usage.out` and `aggregate-order.out`: focused mutation controls failed on the pre-fix ordering.
- `go-final.out`: seven focused groups passed twice; vet and Staticcheck passed.
- `go-legacy-final.out`: the old M1c fixture remains intentionally red and visible.

The old M1c fixture now reaches its no-callback checks for invalid cases, but its `invalid_usage` and checkpoint/terminal-shape expectations are stale relative to canonical task faults. This slice does not update, skip or mark that fixture green. Its paired valid result-plus-error case is also unchanged and still red: the established native contract admits the valid usage, callback and result receipt even though the legacy fixture expects no callback. Changing that policy requires a separate contract decision.

No full-suite, acceptance, profiling or release result is implied by this focused validation.
