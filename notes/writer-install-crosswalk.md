# Installing the ported writer on Session (D01/D02/D03-remainder/D04)

Status: **not started.** The port (`durable/session_writer.go` + `session_writer_v150_test.go`,
pushed at `4772c98`) is exercised only by its own boundary tests. Nothing on `Session` calls it.
This note records the integration surface read from pinned v1.5.0 so the lift is driven by the
reference, not by inference. Upstream paths are relative to
`/tmp/go-ai/runs/durable-port/pi-f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable`.

## What upstream actually does (src/session/session.ts:203-215)

The line runs one job at a time (callback + settlement). A commit with writes then **joins the
writer's pending batch — logical admission — and the line goes on**. The writer stores every
commit admitted while no call is in flight in one `commit()` call **off the line**, then publishes
them one by one in line order, a macrotask apart: read caches, adoption, telemetry, the Session's
own observers, then a microtask later `observeCommits()`, and only then the commit resolves and
its aborts queue. Outcomes are delivered in line order.

Two consequences that the native port has not yet taken on:

1. **A commit can be stored and not yet published.** Reads must therefore gate on keys, or a job
   observes state that is not yet adopted in memory.
2. **Publication is a separate turn.** The line does not own publication any more.

The current native path does neither: `commitPersistence` (session.go:247) holds the line across
`prepareSessionPublication` → `settlePreparedPublication` → `publishSessionPublication`. The code
says so itself: "Preparation, settlement and publication still run on the same Session line.
Keeping the stages explicit does not enable overlap or relax wrapper gates"
(session_publication.go:5-7). So the three-stage split is currently documentary, not structural.

## Integration surface, from the reference

| Upstream | Native today | Work |
| --- | --- | --- |
| `new Writer({...})` session.ts:308 | none | build host hooks from Session: `commit`, `stored`, `publishing`, `failure`, `fail`, `report` |
| `GuardedStorage` + `GatedStorage(storage, key => this.touch(key))` session.ts:306-307 | no gate layer | add the keyed gate layer over the storage read path |
| `touch(key)` session.ts:424-433 | none | on the line: wait on `writer.gate(key)` / `drained()` for `ALL_KEYS`, then `#assertHealthy()`; mark `#current.hit` |
| `#writer.admit({index, writes, keys, context, publish, discard}).outcome` session.ts:869, 1122, 1150, 1284 | synchronous settle+publish inside the line | replace with admission; the job awaits `outcome` |
| `stored(writes)` → `#cached?.published(writes)`, `#tails?.apply(writes)` session.ts:310-313 | cache/tail bookkeeping inside `publishSessionPublication` | move to the publication turn |
| `#publishing(publish, context)` | ported: `publishing` returns nil-or-channel | wire to Session's publication scope |
| `#writer.drained()` on close session.ts:1687 | `Close` (session.go:463) assumes an idle line | close must wait the newest gate |
| `#writer.unpublished` introspection session.ts:968-974 | none | expose for the memory-bounding tests |
| read paths skip addresses with an unpublished writer session.ts:1011, 1054 | n/a | same guard where caches are consulted |

## Why it has to move together

* **Keyed reads without admission split** = gates that can never be open, since publication
  completes before the line releases. That would be a decorative gate, not the upstream contract.
* **Admission split without keyed reads** = a job can read a pre-admission image while a later
  commit is already stored, so the observed state is not stored state. That breaks the guarantee
  the upstream doc comment is explicitly about.
* **Admission split without close/drained and the failure boundary** = close can return while a
  stored commit is unpublished, and the first-fatal-cause handling (`own` vs `SessionFailed`) has
  no owner. `session_writer.go` already encodes those semantics and expects a host that supplies
  `failure`/`fail`/`report` and an explicit `hop` ("Go has no JS macrotasks; Session integration
  must explicitly establish continuation ordering here").

So D01 (admission handoff off the line), D02 (keyed read gates), the D03 remainder
(first-failing-job, same-call own-error set, prefix-before-fencing) and D04 (close/drained,
failure boundary) are one change, not four.

## Suggested slicing (each step must leave the suite green)

1. **Key model + gate layer, unused.** Port `keys.ts` key forms if not already present, add
   `GatedStorage`-equivalent and `touch()`, wired to a writer that is admitted to by nothing yet.
   Tests: gate layer returns "nothing to wait for" when the queue is empty.
2. **Publication turn.** Split `publishSessionPublication` so adoption/cache/observer work can run
   from a writer publication callback, taking the line itself, instead of from the caller holding
   it. Behaviour unchanged (still one commit at a time), so the suite should stay green.
3. **Admission.** Ordinary (non-deferred) commits admit a part and await `outcome`. Turn on keyed
   reads in the same step. This is where D01/D02 land together.
4. **Group commit.** Let the writer batch admitted parts into one `commit()` — upstream's batching
   and newest-key gates, covered by the existing `session_writer_v150_test.go` cases.
5. **Close/drained + failure boundary.** `Close` awaits `drained()`; move the first-fatal-cause
   ownership onto the writer host hooks. Closes out D04 and the D03 remainder.

Deferred/held-document persistence (`commitPersistence` `deferred` path, `holdPreparedDocuments`)
and the memoryOnly branch need their own pass at step 3, since upstream publishes a deferred
commit only after the commits before it.

## Verification gate for this item

Full durable suite sequential (`CGO_ENABLED=0 GOMAXPROCS=2 ... -p 1`), then vet, staticcheck
v0.8.2, `scripts/check-logging.sh`, and the parity-register check against the pinned tree. Claim
D01/D02/D03-remainder/D04 only when the keyed-read and off-line-publication guarantees are
actually exercised, not merely present.

## Admission flip: attempted 2026-11, reverted (stash: "writer admission flip")

The step-3 handoff was implemented and driven to two passing fixes before it was reverted.
What was established, in the order it was found:

1. **Teardown ownership.** The commit job's teardown (`taskRollback`, poison wakes) is
   line-owned. Releasing the line for the publication wait means teardown must take the line
   back; it cannot run off it. Handled by a held/released flag plus reacquire before teardown.
2. **Failure-driven shutdown must move with the failure.** Once the writer settles storage, a
   storage failure is seen by the writer, not by `commitPersistence`. If the writer's `fail`
   hook only latches the cause, nothing drives shutdown and callers waiting in `Closed` hang
   forever. The hook has to run the same first-cause + shutdown path `backendResult` ran.
3. **The read gate is load-bearing, not decorative.** `TestMainGroupCommitLineReadsWaitForPendingMembership`
   asserts a read cannot complete inside the pending window. With the line released, only
   `touch` delivers that. Gating `Snapshot` and the commit entry point is not enough: harness
   task reads go through `readTaskView`, and each whole-state read path has to be found and
   gated individually.
4. **Blocked (revert cause): failure-driven `Harness.Close` deadlocks.** With the failure
   reported from the writer's flush goroutine, `backendResult`'s `go scheduler.h.Close(...)`
   joins hosts through a path that also wants the Session line, while the publication that
   would settle the failing part wants that same line. `Close` additionally waits the writer
   drained *before* taking the line, and the drain cannot complete while a publication waits
   for a line held by a close that is waiting for the drain. This is a real ordering cycle
   between host joins, the line, and `drained()`; it needs a designed answer, not a patch.

Implication for the next attempt: the failure boundary is not the last detail of the flip, it
is the part the flip changes most. The cycle above should be resolved on paper first — which
of {host joins, line, drained} may be crossed, and in what order — because the native line is
a mutex and upstream's is a suspending chain, so upstream's ordering is not portable by
inspection.

## Second attempt: same cycle, now located

Restoring the flip and instrumenting the line with an acquire/release token guard ruled out
double-release: the guard never fired. The hang is ordering, not accounting.

Failing shape (`TestCodemode120RejectedDeliveryNeverResolvesVMAndReopensKey`): storage fails
inside the writer's settle -> `writerFail` -> `backendResult` -> failure-driven `Harness.Close`.
That close joins hosts and takes the line; the writer's publication for the same part also wants
the line; and `Close` waits `writer.drained()` before taking it. Three waits, one line.

The single fact behind all of it: **publication needs the Session line.** Upstream's publication
does not need a line at all - it runs off the line, and it is safe there only because JS is
single-threaded, so adopting into memory is atomic with respect to jobs. Go has no such
accident. Native memory (`readImage`, caches, scheduler state) is line-owned, so publication
must hold the line, and a mutex held across the publication is exactly what the handoff exists
to avoid.

So the flip is not blocked on a detail of the flip. It is blocked on one of these, neither of
which is a patch:

1. **Convert the line to upstream's suspending job model** - jobs are queued continuations that
   release the line at an await point, so `touch` and publication both suspend rather than hold.
   This is the faithful port and the largest one: every line-owned invariant that currently
   assumes continuous ownership has to be re-examined.
2. **Move line-owned memory behind its own ownership**, so publication mutates state without the
   line and the line stays a job-ordering mutex. Smaller, but it redefines what "line-owned"
   means across the package and needs the same invariant sweep.

Choosing and designing one of these is the actual next step for D01/D02/D04. Until then the
installed-but-not-admitting writer at HEAD is the correct state: every guarantee it would relax
is one it now cannot relax safely, and it claims none.

## Admission flip, third attempt: the cycle is gone, the cost is elsewhere

Landed the admission-ordered line first (`8a4d06a`, full suite ok 392.560s on its own merits),
then re-applied the flip on top of it. The ordered line **did** break the cycle: both tests that
had hung for 20+ minutes - `TestMainGroupCommitLineReadsWaitForPendingMembership` and
`TestCodemode120RejectedDeliveryNeverResolvesVMAndReopensKey` - passed, and the failure family
passed. That confirms the diagnosis: the deadlock was a contended token letting close and host
joins get ahead of the publication that had to settle them.

The flip still does not land. Reverting it produced four behavioural regressions plus a new
hang, and they are not one bug:

- `TestMainSQLiteAdoptedTaskSnapshot` - "internal task read copied the adopted SQLite image".
  Self-inflicted: adoption was given a nil candidate, so `adoptReceipt` rebuilt the image from
  the receipt frame instead of adopting the exact image native storage captured at that receipt.
  Correct but a real deviation, and a measurable one.
- `TestMainHarnessDocumentObserverQueuedAttachment`, `TestTaskRuntimeQueuedCapabilitySealAndCallerCancellation`,
  `TestTaskRecoveryProvisionalReservationRollbackBeforeFrontierAdmission` - publication no longer
  completes before the commit returns, and several paths reason about state as though it did.
- A scheduler `taskBookkeeping` blocked on the line indefinitely: ordering is fair now, but the
  scheduler's line jobs and the new publication jobs queue against each other in a way that needs
  working out, not a patch.

So the honest shape of the remaining work: the flip costs (a) threading the adoption candidate
through the writer so memory adoption still uses the exact stored image, (b) re-examining every
path that assumes publication preceded commit return, and (c) settling how scheduler line jobs
interleave with publication jobs. (a) is well-defined. (b) and (c) are each open-ended.

HEAD stands at the ordered line with the writer installed and admitting nothing. The one real
deviation this attempt could have introduced - a weaker line - is now fixed and verified
independently, which is progress that does not depend on the flip landing.

## Admission flip, fourth attempt: candidate capture fixed, read gates mapped, two failures left

Re-applied the flip on the ordered line with the adoption candidate captured inside the storage
sink, and mapped upstream's read gate across the native read surface. This is the furthest the
flip has got, and it still does not land.

**Fixed by capture:** the SQLite adopted-image regression. `TestMainSQLiteAdoptedTaskSnapshot`
asserts an internal task read shares the backing array of `core.state`, so adoption must take the
exact image storage holds at that receipt, not a rebuilt one. Capturing `s.core.state` inside the
receipt sink satisfies it; single-part flushes only, because in a multi-part group the state at a
later receipt has already moved past an earlier part and adoption must rebuild from the receipt
frame. Checked against source first: upstream's `#adopt` calls `tx.adopt(seq)`, so the candidate is
a native mechanism with no upstream equivalent - the requirement is native's own, not a deviation.

**Read gate sweep, where it belongs:** upstream pairs `readOnLine` with `touch`, and gates every
read. `ReadOnLine` is the direct analogue. Gating `Snapshot`, commit entry, `readTaskView` and the
definition/observation/scan entry points (`document_definition.go`, `context_read.go`,
`observation.go`, `scan.go`, `entry_definition.go`, `session_document_observers.go`) fixed the
context and document capability cases and the queued-observer case.

**What is NOT a gate site:** `document_history.go` and `submission_transaction.go` use `t.enter()`,
which is transaction-scoped. A Tx runs inside a commit that already owns the line; releasing it
mid-transaction is something upstream never does. The first attempt to add gates there was wrong
and was reverted.

**Two failures remain:**
- `TestTaskRuntimeQueuedCapabilitySealAndCallerCancellation/historical-document/seal` - the last
  capability kind, so a historical-document read path still answers from state the admitted commit
  has not published. Not yet located.
- `TestTaskRecoveryProvisionalReservationRollbackBeforeFrontierAdmission` - "fatal reservation
  leaked provisional authority". Unrelated to read gating: the reservation/authority path behaves
  differently once publication no longer precedes commit return.

Both are read-path and scheduler-path questions inside the same change, not one-line fixes.
