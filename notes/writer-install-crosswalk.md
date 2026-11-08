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
