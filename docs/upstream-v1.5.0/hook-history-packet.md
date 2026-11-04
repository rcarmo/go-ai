# Hook and task history reads

The pinned `packages/durable/test/harness-hook-reads.test.ts` has two tests. Both use `TaskRuntime.entries`, which is absent from the Go `TaskRuntime` and therefore from embedded `HookAPI`. Source `types.ts:328–333` defines committed, fork-aware raw history. `harness/scheduler.ts:2828–2836` binds the scan to the invocation's conversation and Session line.

1. Source test at line 18 reads ascending raw history in `onYield`, reads the user by ID, and checks that the assistant token rejects the user entry. Its expected history contains only `pi.user`: the current answer has not been committed. Port through the real hook invocation and retain the ordering assertion.
2. Source test at line 51 reads task history ascending in pages, follows the continuation and checks inclusive minimum/maximum entry-ID bounds. It expects user then assistant. Port through the actual task runtime, including page termination and cursor/query binding.

Native `durable/task_runtime.go:523–558` supplies `Entry` and `TypedEntry`, and `HookAPI` embeds `TaskRuntime`; it does not supply a paginated range scan. `durable/history.go` defines `EntryQuery` and `durable/scan.go` supplies `Session.ScanEntries`; `TestScan110OrderForksCursorsAndLegacy` covers backend fork, order, bound and cursor behaviour. Those lower-level controls do not establish hook/task binding, invocation lifetime, cancellation, Session ordering or the source hook's pre-answer boundary. `TestMainAfterRunHookReadsCommittedEntryBoundaries` reads individual committed IDs at a different hook boundary and cannot replace either source test.

## Implementation boundary

P01's runtime pause remains in force. Before implementation, port the two complete source scenarios into a pending fixture and record the missing-surface baseline. Reuse the native bounded query and cursor implementation only after checking invocation validity before and after Session admission, conversation binding, cancellation and ownership. Match source committed history, including fork visibility and inclusive bounds; do not use transaction staged-write scans or silently read an arbitrary conversation. Retain the source-kind mismatch and pre-answer checks. The independent source694 after-run fixture must preserve its own assertions and remains blocked on P01 direction.

Focused acceptance includes both translated hook/task tests; `TestScan110OrderForksCursorsAndLegacy`; invocation-ended and cancelled-acquisition controls; foreign-conversation/cursor misuse and detached-result controls; and the existing afterRun boundary test. Run through the protected-prototype overlay and pure-Go project environment. This packet is a source inventory and test plan, not an executed baseline or approval to change P01.
