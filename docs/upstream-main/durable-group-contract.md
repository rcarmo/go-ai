## Group-commit implementation requirements

Both pinned group-commit test files have been reviewed through their final assertions. Native public Apply and Session retain single-batch behaviour. [Internal group settlement](durable-group-settlement.md) now implements prefix preparation, receipts and SQLite atomic frames; the requirements below also cover the unfinished Session pipeline and host contract.

Source: `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/durable/test/session-group-commit.test.ts`, `packages/durable/test/store-storage-commit.test.ts`, and `packages/durable/src/types.ts` around `CommitResult`/`Storage`. The declaration counts are lexical: 34 Session declarations and 16 store declarations plus three tables. Parameter loops expand cases; these counts are not execution results.

### Storage settlement and receipts

- Preserve legacy `Apply`. A group needs an ordered list of non-empty batches and a receipt for each accepted batch's index and seq. Receipts establish durability; publication follows separately.
- A normal result distinguishes accepted-prefix length from refusal presence. A present refusal can carry undefined, null, false, zero or an empty string. A tagged value and an independent presence flag are required at a JavaScript boundary. A nil Go error cannot represent a received rejection with a null/undefined value; use a non-nil typed wrapper if adapting those values.
- A refused batch and all later batches remain unstored. A rejection has unknown outcome for unreceipted parts. Earlier receipts remain valid, including when the enclosing call rejects later. Do not infer rollback from rejection alone.
- Source StoreStorage places the accepted prefix in one backend transaction, with one seq/outbox record per batch. Check rejection/refusal before writes. Deliver receipts synchronously, in index order, after COMMIT. Confirmed rollback leaves seq, rows, allocator and outbox unchanged and permits retry; failed COMMIT or unconfirmed rollback stops the store.
- Another owner's transaction and a read cannot enter halfway through that transaction. A store export holds the queue and excludes a series queued behind it. Closing a store rejects queued calls as released while allowing its running call to settle. These store-close rules differ from Session's drain.
- Reopening after a crash recovers every stored batch, the individual seqs, document versions and non-reused IDs. Retain no settled batch container or receipt closure after its lifetime ends.

Native SQLite stores framed snapshots/metadata instead of the source schema. Its one-call transaction, per-batch frames/seqs, allocator and failure behaviour need dedicated tests. Source's five/thirteen-statement assertions require a separate format/cost disposition; a different schema does not waive atomicity or receipts. [Journal process-crash checks](durable-group-lifetime.md) now recover the confirmed prefix before adoption/receipts, after a receipt, after return and after normal refusal. Machine power loss and arbitrary device-write interruption are outside those checks.

### Preparation and publication

Use distinct preparation, writer and ordered-publication stages. A held writer must allow disjoint callbacks to prepare; one serial line around every stage cannot satisfy the source batching assertion. Record read/write dependencies by key, including creation, held-document flushes and read-triggered writes. An unkeyed scan waits for every earlier unpublished part. Preserve detached public values and wrapped Snapshot fault gates when choosing private read paths.

| Boundary | Required result |
| --- | --- |
| Transaction callback throws | Reject that job; keep the Session usable |
| Storage rejects after receipts | Received parts may publish, with that call's rejection still delivered to their callers; preserve the failure boundary for other jobs |
| Adoption or cache write-through throws | Preserve earlier publications, fail that part with the original cause, leave later parts unpublished with SessionFailed; stored data remains recoverable |
| A standalone deferred adoption throws | Settle its caller and close; no outstanding/unpublished/read bookkeeping survives |
| Earlier continuation fails the Session | Recheck failure before each later publication, including inside a delayed storage publication scope |
| Publication scope throws before invoking publication | Report the scope error and publish once through the fallback |
| Deferred commit behind a writer | Publish only after the predecessor; never publish behind its failure |

Publish each stored part inside its own host batch, including that batch's flush. Preserve the first fatal cause. Receipt delivery, adoption, cache write-through, observer delivery and caller settlement are separate steps; sharing a writer result does not merge their failure boundaries.

### Deferred persistence, admission and close

Document unload, version migration/reload and deadline persistence all queue behind the in-flight writer. Readers of held changes wait for the deferred publication and flush those changes once. Session close drains in-flight calls and persists held changes before reporting closed. Internal failure still settles all callers and closes without leaking pending counters.

Source admits at most 256 outstanding jobs without a delivered outcome. In the held-writer test exactly 255 further callbacks start; returning the idle counters to zero is also required. A serial implementation admitting only one callback satisfies the upper bound but fails that test. Release capacity on delivered outcomes, not merely on storage completion.

The five deterministic random seeds check caller outcomes against reopened log entries, each callback's reads against earlier stored increments, final documents against replay and zero idle counters. Native model tests must check those relationships across controlled interleavings and refusals without depending on Go scheduling order.

### Continuation ordering needs a host contract

The source Promise continuation of part B runs before part C publishes. A Go goroutine returning from blocking `Commit` can be descheduled before its next statement. `runtime.Gosched`, timers or a dispatcher hop cannot guarantee that arbitrary caller code runs before the next publication.

A continuation-aware API could explicitly enqueue caller continuations between publications. It must define follow-on commits, reentrancy, failure and Close/drain behaviour. An acknowledgement API could also supply ordering, but makes progress depend on callers acknowledging; adding that requirement to existing blocking Commit would break compatibility. A storage publication scope alone does not observe arbitrary post-return caller work.

Keep the existing blocking API and fences while prototyping those alternatives. Exact legacy post-return ordering needs either a contract-preserving mechanism demonstrated by tests or Rui's acceptance of the observable difference. No scheduler-equivalence assumption closes it.

### Candidate-retention constraint

`prepareCommit` currently creates each validated prefix through `prepare`/`candidateTables`. Every candidate copies all six tables; unchanged nested values remain shared and immutable. `commitBatches` discards intermediate candidates. Retaining a `[]Snapshot` until SQLite COMMIT would retain a table copy for every accepted batch. The encoded frame cap bounds payload bytes, not this product of base-state table size and prefix count. No multi-candidate retention or unbounded full-image array has been added. Per-part publication needs a bounded representation before overlap is enabled. The final raw SQLite image cannot serve as an earlier receipt's image.

Wrapped storage remains authoritative for its Snapshot/scan/Entries data and errors, including replacements after Open. Native-only read projection does not supply a wrapper projection protocol. Existing fault gates and serial blocking APIs stay intact.

### Next implementation slice

The internal receipt/refusal protocol and backend settlement now sit behind the existing single-batch API. Ordered receipts, prefix refusal, rollback/uncertainty, ownership, reopen and Close have initial native coverage; same-core queued reads and SQLite/journal process crashes have bounded native coverage. Export/cross-owner isolation and collection tests need further work. Keep value presence explicit even when native backends normally produce errors. Do not let optional group dispatch bypass a wrapper's existing fault gates.

Then separate preparation from the writer, adding keyed dependencies and global scan barriers before enabling overlap. Add ordered publication with tested adoption failures and an explicit continuation contract. Connect cache writes and deferred persistence only after those boundaries hold. The existing [barrier checks](durable-commit-barriers.md) and [cache-cost follow-up](durable-cache-storage.md) remain regression requirements. The full development durable race gate passes at a5dedbd (631.158 seconds, CGO enabled); acceptance and CPU/allocation profiling are unfinished.
