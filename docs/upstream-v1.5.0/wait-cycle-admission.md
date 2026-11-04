# Persisted wait-cycle admission

P03 remains open. On the current serialized Session path, the second task can observe the first task's committed waiting state before it stages the opposite edge. The retained opposite-waits oracle still fails. Concurrent Session group admission (P05) is not needed to reproduce this specific failure; visibility alone does not establish that a proposed fix is correct.

Source: `packages/durable/src/harness/scheduler.ts:2470–2581` and `packages/durable/test/harness-group-commit.test.ts:226` at `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native inspected at `cbcca35`. Runtime changes remain paused pending direction recorded in the pending fixture.

## Graph contract

The pinned scheduler validates a proposed wait before admitting it. It rejects missing targets, waits from an abort handler and invalid `failFast` ownership. To find a cycle it searches from each proposed `on` target for a path back to the waiting task. The graph includes:

- explicit wait edges of non-abort-marked waiting tasks;
- live, ordinary owned work below every reached live task, including traversal through ownership subtrees;
- no wait edges from an abort-marked task, but its live ordinary owned work still holds it;
- no outgoing work from a terminal or unavailable record, and no background work as an ordinary owned-work edge.

The source uses an overlay/live-record view and bounded visited sets. It reports an error containing `wait cycle` and a path. A direct self-reference has separate native reference validation and should not be used as evidence for transitive cycle coverage.

## Native boundary and candidate

`TaskRuntime.commit` copies the returned `TaskState`, updates the task execution and stages a `put-task`. `Session.finalizeTaskWrites` calls `taskScheduler.prepareTaskWrites` before document finalisation, storage settlement and receipt publication. That helper currently returns early unless a staged task has a terminal execution, and otherwise validates terminal-owned work.

`Tx.current` returns a staged candidate or calls `prepareCandidate(..., false)`, folding held documents into the provisional image. Final storage validation checks task records, `validateTaskReferences`, owner chains and ownership cycles. The reference validator rejects self, missing and owner waits and invalid `failFast` ownership, but does not check transitive persisted waiting edges. The candidate must still undergo final reference validation; provisional `references=false` is not a substitute.

`taskScheduler.boundWaitCycle` handles a different API: bounded invocation waits. It also traverses volatile waiter registrations and generation tool children, returns `InvocationWaitRequiresYield`, and only follows owned work for decided/abort-marked tasks. The pinned persisted-wait graph traverses live ordinary owned work from every reached live record. Reusing the helper unchanged would therefore be unproven and could reject or allow different edges.

A candidate implementation to evaluate after direction is a bounded traversal at task-write finalisation, after building the transaction's effective candidate and before storage. For each newly staged waiting transition, search from `On` targets back to its task using source-equivalent waiting and ordinary-owned-work edges, with visited sets. Validate the candidate's final references before returning a cycle-specific error and leave the previous state unchanged. This is a design hypothesis, not a patch: same-batch task/ownership writes, cancellation, abort-marked tasks, terminal tasks with retained hosts and future P05 pending overlays need tests. Avoid scanning the entire task graph for every document-only commit; any complexity limit must not silently admit an unchecked cycle.

## Focused evidence

At `cbcca35`, pure Go/modernc SQLite; protected prototype excluded by overlay:

- Unchanged `TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits`: FAIL in 4.27s, `refusals=map[]`, `ranDone=map[]`. This is the known source226 divergence.
- `TestTaskOwnershipWaitingUnionRejectsAndAllSettledForeignOrder`, `TestTaskOwnershipAllSettledEveryOutcomeLiveBarrier`, and `TestMainGroupCommitChildEndsBeforePhaseWhenMarkUnpublished`: 3 top-level passes, count 1, 0.592s. These preserve existing self/missing/owner/failFast and normal barrier coverage. `go vet` and `git diff --check` passed with the prototype overlay.
- A **diagnostic copy** of the pending fixture logged `tx.state.Tasks[target].Status` inside each waiting callback, without changing its assertions. It recorded `x → y: running`, then `y → x: waiting`; the original cycle assertion still failed in 4.287s. The copy was discarded. This demonstrates visibility for this serialised run, not arbitrary future batched group semantics or a passing oracle.

No runtime modification or implementation experiment was made. Raw diagnostics and overlays were removed after use.

## Next implementation gate

The pending fixture explicitly says it needs direction before runtime changes. After Rui's direction, implement only a candidate graph check and retain the original oracle. Add focused controls for longer cycles; independent acyclic chains; converging shared prerequisites; abort-marked and terminal targets; ordinary/background owned work, including conversation-owned descendants; `failFast` ownership; cancellation; and same-batch replacement. Verify that a refused transition leaves the task able to commit its fallback outcome. Run the existing ownership/wait and bound-invocation controls unchanged.

P05 remains independently necessary for the source's concurrent admission/group-call contract. The witness above narrows the dependency: source226's observed two-edge serial case has sufficient first-edge visibility without P05. A later P05 implementation must repeat cycle checks against the effective pending overlay.
