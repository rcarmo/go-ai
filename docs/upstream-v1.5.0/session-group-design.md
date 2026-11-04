# Session group-publisher design

P05 is in design. Native still holds one Session line through callback, settlement and publication; this document does not claim that it implements the pinned grouping contract. The first implementation gate is source170. No runtime concurrency change is authorised by this design alone.

Source: `packages/durable/src/session/session.ts`, `writer.ts` and `packages/durable/test/session-group-commit.test.ts` at `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native examined at parent `dc53b9d`.

## Source ordering

The source serialises **job execution** on `#tail`; independent callbacks do not execute concurrently with each other. A job that has admitted a stored part can finish its line work while its publication is still pending. The next eligible job can then execute while the prior storage call is in flight. Its `Ran.after` waits for the relevant publication; outcome delivery is normally fenced in job order. An exempt job that neither hit a gate nor awaits its own publication can deliver early. Do not equate overlapping storage with parallel mutation callbacks.

The writer tracks unpublished keys. A gated read or write waits for the relevant predecessor to publish or become unpublishable; an unkeyed scan waits for every unpublished part. Parts admitted while a call is in flight queue for the next storage call. Returned sequences are attached to individual parts; publication runs in order, within storage publication scope, with a macrotask between parts. Failure/adoption/observer errors fence later publication and outcomes according to the first failure and own-outcome rules. At most 256 started jobs have undelivered outcomes.

Source170 asserts callbacks b/c have run during held a, no publication yet, calls `[1,2]`, three distinct increasing sequences and settlement order a/b/c. Source920 asserts first + 255 callbacks while held, no more admission until an outcome is delivered, then all 300 finish and counters are zero. Neither is satisfied by storage-only batching or native serial-writer controls.

## Native boundary

`Session.enter` takes the single `line`; `commitPersistence` retains it through callback, sealing, task/document finalisation, `prepareSessionPublication`, `settlePreparedPublication`, receipt adoption and observer preparation. It releases the line before dispatching watches/stops. `settlePreparedPublication` rejects a base sequence different from the current `readImage` and calls `settleBatch` with **one** batch. The core's `commitPartsAt` can settle multiple batches but the Session path does not use it to group independent jobs.

The global baseline is intentional: task finalisation, candidate/reference validation and held-document folding can scan beyond a simple write key. Removing the line or suppressing the baseline check without a complete dependency witness could admit stale reads, duplicate document creation or incorrect owner/abort decisions. Callbacks can have application effects; rerunning one silently after detecting a conflict is not generally safe.

## Proposed state boundaries

This is a candidate for staged review, not a prescribed patch:

1. Keep one serial admission/preparation owner for transaction callbacks and line-owned scheduler/observer state. Represent each started job with a monotonically ordered index, cancellation state, outcome and delivery state. Count started but undelivered jobs and bound them at 256; waiting for capacity must be cancellable without consuming a slot.
2. A prepared part owns immutable detached writes, dependency/read witness, own publication data, held-document selection, caller outcome and a storage/publication completion handle. Separate private memory adoption of a deferred part from a durable receipt; do not make a deferred part visible after its predecessor fails.
3. A single storage worker takes a queue snapshot and settles its ordered batches. A later part may prepare during an in-flight call only when its required dependencies have a safe view or the relevant unpublished key gate is open. Until native has complete read/write and ownership witnesses, unsafe or unkeyed operations must conservatively wait; they must not be replayed automatically.
4. Return to the Session owner to consume **each receipt** in order. Validate index and sequence and construct the exact prefix snapshot for that part, not the storage core's final image after an atomic SQLite group. Adopt it before retention, scheduler effects and observers; then open that part's gates. Recheck failure at the actual publication boundary.
5. Keep an ordered delivery fence separate from admission and storage. Preserve the source's write-free/gate exemption only with an explicit test. Do not claim exact JavaScript microtask/macrotask continuation ordering from a Go channel send alone: whether publication can safely wait for a caller continuation requires an agreed native mechanism or an explicit unresolved adaptation.
6. Track close/failure and cancellation independently. Cancellation before admission may stop a job; once storage is admitted, complete adopt-or-poison and do not erase an uncertain outcome. Stop admitting later work on fatal failure; retain each part's own error/first cause as its contract requires. Close drains or abandons all started parts and gates before releasing storage. P02's attempt fence must be integrated after its decision.

## Blocking design questions

- Which operations can provide a complete key/read witness, including `Tx.current`, document address creation, task ownership, scans and held values? A conservative global gate preserves safety but will not satisfy source170 for independent callbacks. Specify actual instrumented operations and tests before allowing overlap.
- How are prefix images constructed efficiently and safely after SQLite settles a group atomically? `commitReceipt.nextImage` and core preparation are possible starting points; prove independent receipt/image ownership, limits and failure handling. Do not use the already-final `core.state` as every part's image.
- What is the Go publication/delivery barrier equivalent of a JavaScript macrotask and `Storage.publishing` scope? Keep source698/721/753 unverified until the design proves observable continuation and scope ordering.
- What state survives a context cancellation, close, failure or process crash at each boundary? P04 proves a single SQLite rollback, not multi-part refusal. P03 must check the effective pending dependency graph when concurrent admission is added.
- How are temporary per-job data and retained images bounded without weakening the 256-job rule, record quotas or isolation? Preserve the project temporary-root and cleanup rules.

## Incremental acceptance gates

1. **Harness safety:** held-append test cleanup releases the hook before waiting for `Session.Close`; an old-order fault must hang/fail under a short bounded timeout. This is a test-fixture fix only.
2. **Admission/grouping:** retained or newly ported source170: b/c callbacks execute serially while a's call is held; no publication until release; calls `[1,2]`, increasing unique sequences, delivery a/b/c. Preserve existing source217/240/256/292/310 controls, labeling serial approximations until replaced.
3. **Per-part visibility and failure:** source441/461/486/565/635/662/680 and P04 confirmed rollback/uncertain-outcome controls. Assert earlier prefix image, first cause, later absence and reopen; compare the source's backend predicate mechanism before claiming parity.
4. **Delivery and scope:** source698/721/753/770/796/815 including a continuation that changes failure state before the next part; check exact callback scope and ordering, not only sorted sequence numbers.
5. **Capacity/cancellation/close:** source920 exact `255 + first = 256` started outcomes; all 300 settle after release; counters zero. Add cancellation before/after admission, blocked close and P02/P03 regressions. Run focused race-sensitive sets repeatedly only as authorised.

## Harness correction at parent `dc53b9d`

`openGatesSession` used `t.Cleanup` in an order that invoked `Session.Close` before releasing a held append. An early assertion failure could deadlock cleanup. Its release cleanup now runs first. `TestMainGatesSessionCleanupReleasesHeldAppend` returns from a subtest while append is held and checks that cleanup lets its commit goroutine return. Four focused functions, count3/shuffle42: 12 top-level passes, 0 failures, 10.785s; vet/diff clean. Restoring the old cleanup order via an overlay fault produces the expected 4s test timeout. Fault traces and run scratch were removed. The first fault-script parse error was uncredited. No production runtime was changed.
