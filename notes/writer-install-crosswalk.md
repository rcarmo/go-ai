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

## Admission flip, fifth attempt: both original failures fixed, full suite deadlocks

Furthest the flip has got, and it still does not land. Diff kept in
`notes/writer-install-crosswalk.patch`.

**Both earlier failures are fixed, for known reasons:**

1. `TestTaskRuntimeQueuedCapabilitySealAndCallerCancellation/{document,historical-document}/seal`.
   `TaskRuntime.SnapshotDefinition` and `SnapshotDefinitionAsOf` entered the line with receiver
   name `session`, so the regex sweep that gates reads on `touchAll` skipped them - the only two
   capability kinds left ungated, which is why 20 of 22 kinds passed. Adding the gate fixed them.
   Recorded here because a gate sweep keyed to one variable name silently misses entry points; the
   sweep must match the enter/leave pair, not an identifier.
   Note: `enterKeyed` does not exist in the tree; an earlier hypothesis about it was wrong.

2. `TestTaskRecoveryProvisionalReservationRollbackBeforeFrontierAdmission` - "fatal reservation
   leaked provisional authority". Real race, not a test quirk: the reservation's `taskRollback`
   clears provisional authority, and the invariant is native's own
   (`taskRollback func(error) // trusted bounded bookkeeping BEFORE Session.leave`). With the
   handoff there is no leave after Storage, so the rollback ran after the part's outcome closed and
   a gated reader could pass `touchAll` and observe the un-rolled-back invocation. Upstream has no
   `rollback` or `provisional` in `session.ts` at all, so there is nothing to copy; the invariant is
   native's to preserve.
   Fix: the writer's `discard` hook runs *before* `settle()` closes the outcome and gate, which is
   exactly the required ordering. `discard` took no argument, so it became `discard func(error)`,
   and the commit's rollback is now once-guarded so Storage failure and the deferred teardown
   cannot both run it. All 22 capability kinds and the reservation case pass.

**New blocker, structural:** the full suite deadlocks in `TestMainHarnessRecoveryBeforeFirstStore`.
The scheduler's `run` defer calls `taskBookkeeping`, which blocks on the line for 19 minutes. The
holder is a publish callback: the flip makes publication re-acquire the line to adopt, and the
adoption path reaches scheduler bookkeeping that calls `taskBookkeeping` again. The line is not
reentrant, so publication waits on itself while owning the line, and every line entrant behind it
strands. `taskBookkeeping` has 19 non-test call sites, several on adoption-adjacent paths.

Upstream cannot deadlock this way: `#adopt` runs synchronously on the same promise chain that
admits readers, with no lock to re-enter. The line-owned memory model needs the publication path to
be provably re-entrancy-free. That is a design task - thread line ownership explicitly through the
bookkeeping the publication path reaches, or make adoption call the inner functions directly rather
than the acquiring wrappers. It is not a one-line fix, and `taskBookkeeping` using
`context.Background()` rules out passing ownership by context without touching every site.

Nothing half-gated is left in the tree.

## Sixth attempt: the deadlock was host timer code, not re-entrancy; flip direction confirmed upstream

**My earlier root cause was wrong.** The dump showed the publication goroutine had already
*released* the line, then blocked in `leaveThen` -> `sessionTimer.disarm` -> host `cancel()`.
The test's host cancel performs a Session read (`Snapshot` -> `touchAll`), and that read gates on
the very publication whose gate is still open because `settle` has not run yet. Cycle:
publication -> disarm -> host cancel -> read -> gate on publication. Nothing to do with line
re-entry.

**Flip direction verified from source.** `#onLine` (session.ts:1670) holds the line for the line
part, `work` returns `{value, after}`, the line is released when the line part resolves and
`after` (publication) settles off it, with `#ended(job)` firing only after `after`. Releasing the
line before publication completes is upstream behaviour.

**Fix that made the targeted groups pass:** `handoffLeave` - release the line without consuming
`pendingTimers`, used both at the commit's handoff and for the publication's release. Whoever holds
the line next (normally the commit, after its outcome closes and before it returns) flushes them.
This removed the deadlock *and* kept the arming ordered before `Commit` returns, which is what the
earlier async-dispatch variant broke (`assertArmed` saw a stale armed timer). Also verified in
source that the hold timer is re-armed in the after-store publication ("After their store: forget
holds, unpin their documents, and move the ns to the next deadline"), so the changes belong to the
publication's owner, not to the admission handoff - flushing at both points was what contended.

`TestMainSessionDeferredDeadline*`, both capability kinds, the reservation rollback, the adopted
SQLite image and the queued observer all pass with this. `go vet`, staticcheck v0.8.2 and
check-logging.sh pass. The full suite no longer hangs (409s) but 8 tests fail.

**Remaining blocker, one root cause:** after the handoff, `s.readImage` advances only at
publication, so the next commit prepares against a baseline that misses parts already admitted.
Six of the eight failures are `stale prepared publication baseline` - my own reject at admission,
masking the real gap: native needs an *admitted* high-water image distinct from the published read
image, the way upstream's line part keeps admission ordered. `TestMainSessionReadImageGapFailsClosed`
is explicit that such a gap must fail the Session closed rather than reject the commit, so the
reject must go and a real mechanism must replace it. The other two are group-commit live-owner
ordering (`P must hold as completing while D is live`) and a submission poison/close case
(`want durable: session failed, got durable: closed`).

Attempt 6 is the furthest yet: no deadlock, no hangs, one named mechanism missing.

## Seventh attempt: the "missing high-water image" was my own invented check

**My previous root cause was wrong again, and the instrumented reject proved it.** The failures
printed `base=26 readImage=26 coreState=27 unpublished=0`. The writer held no unpublished part, so
the gate model was not leaking - `admit()` registers `unpublished[key]` and `newest` under its lock
before returning, so `drained()` and `gate()` see a part the moment it is admitted. Nothing was
missing.

What was wrong was the check itself. The pre-flip serial settle compared only the prepared baseline
(`p.base.Seq != s.readImage.Seq`). The flip added `s.core.state.Seq != s.readImage.Seq`, and native
deliberately tolerates the store image running ahead of the read image: `commitPersistence` rebases
on `tx.state` with the comment "A typed version reload can independently store held changes while
this callback runs. Its rebased Tx owns the new receipt/history." Held (deferred) stores advance
`s.core.state` without advancing `s.readImage`, so my extra clause rejected legitimate commits and
six of eight failures were that reject masking nothing.

Dropping the invented clause leaves only upstream's baseline comparison. `TestCodemode120Parallel*`
and the nested auto-admission case go from failing to passing, and the reporter case changes from a
masked reject to a real coalescing failure.

**Six real failures remain, each distinct:**
- `TestMainGroupCommitHoldsLiveOwnerRaceCreation` - "P must hold as completing while D is live, got
  running": group-commit live-owner ordering depends on when a parent's state becomes visible.
- `TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission` - "builtin hold raw status
  disagreement": a hold's raw status is read from an image the flip advances differently.
- `TestMainSessionLocalListenerFatalAfterPublication/stored` - a published call must resolve despite
  its own listener error; it now reports session failed.
- `TestMainSessionReadImageGapFailsClosed` - the gap must fail the Session closed; it now appends
  stale legacy writes. This one is about the test's serial settle helper bypassing the writer.
- `TestBackgroundReportersCoalescedSteersReportSharedAnswerOnce` - "shared answer reported more than
  once [32 35]": keyed coalescence admits two where one should coalesce.
- `TestSubmissionPendingPublicationWaitCloseAndPoisonRemoveRegistrations/poison` - poison reports
  `durable: closed` instead of `durable: session failed`.

Full suite: 409s, no hangs, 6 failures. Attempt 7 is the closest yet, and the failure list is now
honest - none of it is my own reject masking the real mechanism.

## Writer installed: admission queue retained across touch (supersedes attempts 5–7)

The admission flip is installed and the full durable suite passes twice (412.016s,
409.146s). This change remains in production code. The saved attempt patch is historical.

The missing invariant was upstream's `#tail`: `#onLine` awaits `work`, including a
`touch` wait. Later jobs do not run while that job is waiting. Earlier notes claimed
otherwise. Releasing the sole native line during touch allowed later jobs to
pass a suspended job and use stale scheduler authority.

Session now has two queues: `jobs` preserves the upstream admission slot across
read gates; `line` protects Go memory and can be released while the writer publishes.
Normal line release drops both. A gated read drops only the memory lock, and every
exit (including failure/cancellation) reacquires it before caller cleanup. Publication
and discard acquire only the memory lock: they must resolve the part a queued job
is waiting for. Discard rolls back provisional authority before opening the gate.

This resolves owner visibility, reporter coalescence and builtin status failures.
Own-listener errors are returned to the writer as listener failures, so the stored
call resolves before failure fences later work, matching writer.ts. A lifecycle
cancellation of Submission.Wait checks the Session failure before returning ErrClosed.
A native owned-store prefix gap is checked under the store lock after the publication
barrier, before preparing or appending a new batch.

Timer changes remain pending across admission and publication memory releases.
The commit reacquires memory after its part settles, then flushes changes before
returning. Host cancellation may read the Session without gating on its own open
part. This preserves the deadline tests' arming-before-return ordering.

Verification: full durable suite twice; go vet; Staticcheck v0.8.2; logging gates;
parity-register checker. The gate regression now verifies that publication can
acquire memory while a later ordinary job stays queued. Gate tests pass three repeats.

### Parity still requires acceptance evidence

Installing the writer does not close D01–D04. Whole-state transactions still use an
ALL_KEYS barrier. Upstream source170 requires disjoint callbacks to finish and admit
while another storage call is held, then batches those parts in a single call.
That acceptance has not been established by this suite. The source's per-job outcome
fence, failure-boundary index/own-outcome set and publication continuation ordering
also need Session-level acceptance tests. Shared readonly views and the assertion-level
crosswalk remain open. Do not infer full semantic equivalence from a green native suite.

## Address-keyed current and historical document reads

Replaced ALL_KEYS at Session.SnapshotDefinition and SnapshotDefinitionAsOf, and their
TaskRuntime counterparts, with pinned source dependencies:

- Current: document address (`session.ts #loadDocument`).
- Historical: requested conversation and entry, then the document address owned by
  the visible entry's actual ancestor, then its incarnation (`keys.ts GatedStorage`,
  `session.ts snapshotAsOf`). Resolve the incarnation again after the address gate.
- Invocation reads first touch their runtime's task record and recheck lifetime
  after each suspension, before migration or cache callbacks.

`TestSessionV150DocumentReadsTouchOnlyTheirDependencies` failed for both current and
historical disjoint reads before the change. Both now finish while an unrelated
storage call is held. Dependent reads are witnessed at their gate and remain pending
until publication. Values are checked at the requested current/historical point.
The test passes three repeats. Full durable suite: 404.792s; vet, Staticcheck and
logging gates pass. This removes the global barrier on these read paths. It does
not remove it from transaction admission or establish source170 batching.

The user's scope is full pinned-upstream equivalence, including replacement or removal
of incompatible native code. In particular, native whole-snapshot transaction
preparation and prebuilt sequence-stamped publication frames are implementation
choices to replace if they prevent keyed transaction admission and receipt-time adoption.
Preserving those choices is not an acceptance criterion.

## Plain Session transactions: disjoint admission and receipt-time metadata

The source170-style assertion now passes: three new conversation documents are
created by disjoint callbacks, callbacks b/c finish while storage a is held,
nothing publishes early, and storage calls contain [1,2] parts. Receipt sequences
are consecutive and every createdAt equals its own receipt. The assertion failed
before changing transaction admission. It passes ten repeats with the related
queue/lifetime regression group.

Transaction read methods now have a gated host boundary. Document address reads,
incarnation-handle reads and allocation no longer share a blanket ALL_KEYS wait.
When a dependency publishes, staged writes retain their intent and their cached
validation image is invalidated/rebased. Other transaction methods still conservatively
touch ALL_KEYS. Scheduler-bound or deferred transaction entry still drains because
native scheduler callbacks read tx.state directly; this is remaining migration work.

Receipt-time adoption assigns publication Seq, entry placement and document lifecycle
metadata from the receipted image. Queued-part fallback subscription snapshots are
rebuilt from that exact prefix instead of the admission-time snapshot. The batching
assertion initially exposed [3,3,3] publication sequences before this correction.

Failure reporting now waits for the writer gate before invoking a reentrant harness
reporter, so provisional reservation rollback precedes inspection. Queue admission
consults Context.Done consistently on both contended and fast paths. The old test
counted only contention and could falsely report zero admissions. New gate tests
signal the gate consultation after the two queue acquisitions.

Verification: full durable suite 406.652s; vet and Staticcheck; batching, read-key,
capability lifetime, reservation rollback and admission-count tests ten repeats.
D01–D04 remain open for scheduler admission, precise non-document Tx dependencies,
ordered outcome delivery/failure boundaries and publication continuations.

## Harness disjoint document admission

Removed the scheduler-presence condition from public keyed transaction admission.
The source170-style test now runs through both Session.Commit and Harness.Commit,
including initially absent conversation documents. Both pass ten repeats and the
full durable suite passes in 406.673s (vet and Staticcheck also pass).

Internal adapters that still consume a whole graph now declare ALL_KEYS at that
read boundary. CommitTasks only touches its bound conversation before checking it.
Private scheduler/invocation admissions remain whole-state until their graph walks
are ported to source membership/record dependencies. This is no longer a blanket
barrier on public Harness document transactions.

The abort-cutoff recovery fixture was nondeterministic: public AbortTask resumes
execution, so a generation could finish before the fixture rewrote it to simulate
an interrupted host. It now seeds the intended durable running+abort-marked input
through a private task admission without enabling execution. Assertions are unchanged;
the fixture and batching/lifetime regression group pass ten repeats.

## Outcome fences cover ordinary reads (corrects the earlier read assertion)

Re-reading #enqueue showed that the earlier historical read test conflated
execution progress with outcome delivery. A cold read may execute its disjoint
storage reads before a held write publishes, but its public outcome still follows
all earlier outcomes. The earlier assertion that historical reads *return* before
storage release was wrong.

Session now registers an outcome at ordinary line admission. Gate waits retain
that outcome while suspending only the memory lock; publication and maintenance
have no ordinary outcome. Commit handoff retains its outcome until the part
settles. Delivery waits off both lines, so later disjoint callbacks keep running.
Cold document tests now witness migration/read execution before storage release
and separately assert fenced delivery. Read-only commit delivery has its own
source-derived regression, observed failing without the fence.

Scheduler.wake() in source is a synchronous observation of its published graph,
not a storage scan. Native Wake now reads the published graph through maintenance
without an ALL_KEYS gate or ordinary delivery fence; a background wake scan must
not block disjoint document admission behind an unrelated write.

An admitted touch asserts health rather than fresh usability after its wait:
Close cannot replace its own cancellation with ErrClosed. The cancellation/Close
fixture now waits for the actual read deadline signal and tests the fenced error
outcome asynchronously. A native SIGKILL helper now retains a timer while every
host intentionally parks, preventing Go's deadlock detector from exiting status 2
before the parent sends SIGKILL. Stage witnesses and SIGKILL assertions are unchanged.

Verification: full durable suite 404.787s; vet/Staticcheck; read-key, read-only fence,
batching and cancellation/Close tests ten repeats; real SIGKILL stages five repeats.
Remaining source enqueue work includes exempt internal outcomes, outstanding-job
backpressure, failure-boundary poisoning and cached-snapshot fast-path delivery.

## Enqueue backpressure and internal exemptions

Ported MAX_OUTSTANDING=256 at ordinary job start, before acquiring Go memory.
The admission queue keeps its order while waiting for a delivery slot, but the
writer can still publish and release slots. Delivery releases the slot; maintenance
is not counted. The new source920-style assertion fails before this change and
passes afterwards: job 257 cannot start until a preceding outcome is delivered,
then every waiting job completes and retained outcomes/unpublished keys return to zero.

Added the source commitInternal primitive. Only no-write/no-hit internal outcomes
bypass preceding outcomes. Their own delivery releases capacity immediately, while
the ordered fence still follows its predecessor for later ordinary jobs. Separate
assertions prove a gate hit or a disjoint write removes the exemption. Native
whole-graph taskCommit callbacks declare their ALL_KEYS dependency explicitly;
invocation/public commits do not inherit the exemption.

Verification: full durable suite 410.099s; vet and Staticcheck. New exemption/bound
assertions failed before implementation and pass, including hit/write counter-cases.
Failure-boundary poisoning, cached-snapshot delivery fast paths and precise private
scheduler dependencies remain open.
