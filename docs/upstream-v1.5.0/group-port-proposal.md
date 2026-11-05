# Durable group-port proposal for v1.5.0

Status: design only; no runtime or test edits in this pass.

Scope in this pass:
- source: `packages/durable/src/session/session.ts`, `packages/durable/src/session/writer.ts`, `packages/durable/test/session-group-commit.test.ts` at `f9a26365568127461c47a07a37dcb42c140a3ac5`
- native: `durable/session.go`, `durable/session_publication.go`, `durable/session_receipt.go`, `durable/transaction.go`, `durable/storage_group.go`
- review doc: `docs/upstream-v1.5.0/session-group-design.md`

This proposal is intentionally narrow: it targets the exact seams identified in the source tests (`170`, `414`, `441`, `461`, `698`, `920`) and the native current-session publication/receipt path, not a broad “source parity” claim.

## 1. What the source is enforcing

The JS source is not “parallel mutation callbacks”; it is a serial line with a separate writer queue and keyed publication gates.

Relevant source entry points:
- `packages/durable/src/session/session.ts`
  - `MAX_OUTSTANDING = 256`
  - `touch(key)` waits on `Writer.gate(key)` / `Writer.drained()`
  - `fail(error, at, own)` decides `SessionFailed` for later outcomes
  - `#enqueue()` and the `Job`/`#outstanding` logic bound started-but-undelivered jobs
  - `commitWith()`/`commitInternal()` admit a job to the line and then `Writer.admit(...)`
- `packages/durable/src/session/writer.ts`
  - `Writer.admit(spec)` enqueues a `Part`
  - `Writer.#send()` groups parts admitted while no call is in flight into one storage `commit()` call
  - `Writer.#publish()` publishes parts in order with a `hop()` between them
  - `#unpublished` is keyed by write key; a read waits on the newest unpublished writer for that key
  - `#open(part)` resolves `part.gate` and updates `#newest`

The critical contracts in the source tests are:
- `170`: independent callbacks may run while an earlier storage call is in flight; no publication yet; the calls are coalesced later and published in order
- `414`: later outcomes are `SessionFailed` after the first fatal failure; earlier outcomes remain delivered
- `441`: parts received before rejection are published before their own failure is surfaced
- `461`: a failed call fails later jobs with `SessionFailed` except the other parts of that same rejected call
- `698`: publication is ordered one call part apart, not all at once; what resolves from one publication happens before the next publication scope runs
- `920`: at most 256 started jobs may be outstanding without a delivered outcome; when idle, line state returns to zero

## 2. Native baseline and exact mismatch

The current native path still holds one session line through callback preparation, storage settlement, receipt adoption, publication, and observer preparation:
- `durable/session.go`
  - `Session.enter()` / `leave()` serializes access to the Session line
  - `Session.commitPersistence()` runs the callback and then a single settle/adopt publication sequence on that line
- `durable/session_publication.go`
  - `prepareSessionPublication()`
  - `settlePreparedPublication()`
  - `publishSessionPublication()`
- `durable/session_receipt.go`
  - `commitReceipt.checkNext()`
  - `commitReceipt.nextImage()`
  - `Session.settleBatch()`
- `durable/storage_group.go`
  - `storeCore.commitPartsAt()` supports multiple batch frames and receipts but the Session path still treats it as one serial batch path via `settleBatch`
- `durable/transaction.go`
  - `Tx.MintID()` remains a strong durable allocation path through `storeCore.mintID()` and `t.state.HighWater`

This is the key native gap: the source contract is not about “parallel callback execution” as such. It is about independent callback admission while a previous storage call is still in flight, together with keyed read gates and ordered publication outcomes. Native storage settlement is still serialized by the Session line and by `settleBatch`’s single batch receipt assumption.

## 3. Minimal safe staged design

### Stage A: add a writer/part queue without removing the Session line

Minimal safe change: keep the serial line for callback execution and final adoption, but add a separate session-local writer queue analogous to `Writer` in `packages/durable/src/session/writer.ts`.

Concrete native additions:
- `durable/session.go`: add a `sessionWriter` or `writerState` field to `Session`
- add `part` metadata to track:
  - `index`
  - `writes` (optional batch)
  - `keys`
  - `context`
  - `publish` / `discard`
  - `outcome` / `gate`
  - `done`
- add `writerState.queue`, `writerState.flight`, `writerState.unpublished`, `writerState.newest`, and `writerState.outstanding`
- add a `writerState` method that flushes queued admissions into `storeCore.commitPartsAt` while a call is already in flight

Why this is the right minimum:
- it preserves current native lifetime rules (`Session.enter`/`leave`, close, timer, observer sequencing)
- it is the precise source behaviour from `Writer.#send()` / `Writer.#publish()`
- it does not require changing storage backend semantics or removing the single durable line from publication/adoption

### Stage B: add keyed read gates

The JS contract is keyed by write key:
- for each key, the newest unpublished writer is the gate
- a read of a key that has an unpublished writer waits for that gate
- unkeyed scans wait for every unpublished part via `Writer.drained()`

Native port requirement:
- `Session.touch()` or equivalent must wait on the active part gate for the key, never merely on `s.line`
- `ALL_KEYS`/unkeyed waits must resolve when every part admitted so far is published or failed

Concrete native mapping:
- `durable/session.go` should add the equivalent of `touch(key)` and `touchEntries()` as a session-local gate check
- `Tx.current()`, `Session.latestEntryId()`, `latestHeadMarker()`, `scanEntries()`, `scanConversations()` and all direct “read while unpublished” paths must resolve via `writer.gate(key)` / `writer.drained()` instead of only the serial line

Risk: if we gate too broadly on the final Session line, we reintroduce stale reads. If we never gate keyed reads, we violate source test sections `200-380` and the stronger “write visibility after publication” guarantee.

### Stage C: exact receipt handling for grouped prefix adoption

This is the most important correctness boundary.

Source logic depends on a detached/ordered receipt or publication sequence; the native `commitReceipt` design already has the correct abstraction:
- `durable/session_receipt.go`
  - `commitReceipt.checkNext(base)` validates exact sequence continuity
  - `commitReceipt.nextImage(base, limits)` reconstructs a prefix image from the receipt frame without silently substituting later head state
- `durable/storage_group.go`
  - `commitPartsAt()` can produce multiple receipts in order and invoke `receive(index, seq)` for each one

The Stage C port should keep the native receipt semantics exactly:
- each part must carry its exact receipt frame
- adoption must check `receipt.checkNext(s.readImage)` before any image replacement
- `candidate` must be trusted only when `candidate.Seq == receipt.seq`
- if a later image is accidentally passed for an earlier receipt, reject it; do not silently substitute `core.state`

The minimal safe change is not to rewrite `Session.settleBatch()` to a loosely-typed “last image wins” flow. It is to generalize it to a `[]commitReceipt` flow while preserving per-part exact prefix validation under the session line.

### Stage D: ordered outcomes and publication scope

The JS source destination is explicit and strict:
- every part of a call is published in order, with a `hop()` between two parts
- a publication may fail in its own scope, or the Session may fail between publications
- earlier parts keep their durable result even if a later part fails
- the failure boundary is computed after the last successful publication and before later jobs are allowed to proceed

Native mapping:
- `durable/session_publication.go` is already the right place to split preparation, settlement, and publication
- `durable/session.go` still owns the final line finalization and should remain the one place where a first failure decides `SessionFailed` for later job outcomes
- `publishSessionPublication()` should keep the exact “prepared receipt + adopt + post-adopt observers + task/ownership revalidation” order; it must not be collapsed into a raw `core.state` snapshot pass

This is where the current native path is closest to the source model, but it is still too serial and too single-batch. The minimal port is to layer the JS `Writer.#publish` semantics over the existing `prepareSessionPublication` + `settlePreparedPublication` + `publishSessionPublication` loop without reordering the durable adoption invariants.

### Stage E: enforce the 256-job bound exactly

Source `Writer` is not just a queue; it also enforces a job count bound while outcomes are undelivered:
- `MAX_OUTSTANDING = 256`
- a new job blocks past the threshold until an earlier outcome is delivered
- the line state must return to zero when idle

Current native `Session` has no direct equivalent of `#outstanding` / `#room`; it serializes on the line and does not track a started-but-undelivered count.

Minimal native addition:
- add `Session.outstanding` counter and a `pendingRoom` gate (or equivalent channel / condition)
- increment at admission, decrement when the job outcome is delivered
- the generator must block beyond 256 without consuming a write slot or silently dropping a callback
- `lineSizes`-style diagnostics should report `{ outstanding: 0, unpublished: 0, reads: 0 }` at idle

This is the closest translation to the source `#outstanding` contract and should be implemented before any broader “admission overlap” change is considered safe.

## 4. Concrete entry points to change

### Source (reference contract)
- `packages/durable/src/session/session.ts`
  - `MAX_OUTSTANDING`
  - `touch(key)`
  - `fail(error, context?, at?, own?)`
  - `#enqueue()`
  - `commitWith()` / `commitInternal()`
- `packages/durable/src/session/writer.ts`
  - `Writer.admit()`
  - `Writer.#flush()` / `#send()` / `#publish()`
  - `Writer.#unregister()` / `#open()`
  - `hop()`
- `packages/durable/test/session-group-commit.test.ts`
  - `170` (batching while call is in flight)
  - `414` (fatal failure outcome ordering)
  - `441` (publish before reject)
  - `461` (later jobs fail with storage error)
  - `698` (macrotask ordering of publication)
  - `920` (256 outstanding jobs bound)

### Native (minimal safe implementation points)
- `durable/session.go`
  - `Session.enter()` / `leave()`
  - `Session.commitPersistence()`
  - `Session.commit()` / `taskCommit()` / `invocationCommit()`
  - add `sessionWriter`/`writerState` and `outstanding` accounting
- `durable/session_publication.go`
  - `prepareSessionPublication()`
  - `settlePreparedPublication()`
  - `publishSessionPublication()`
- `durable/session_receipt.go`
  - `commitReceipt.checkNext()`
  - `commitReceipt.nextImage()`
  - `Session.settleBatch()`
- `durable/storage_group.go`
  - `storeCore.commitPartsAt()`
  - `prepareCommit()`
  - `commitGroupResult`
- `durable/transaction.go`
  - `Tx.MintID()`
  - `Tx.stage()`
  - `Tx.current()` and admission-time validation checks

## 5. Durable ID reservation: preserve the stronger native guarantee

This is the highest-risk non-source issue.

Native current semantics are stronger than the source contract:
- `Tx.MintID()` calls `storeCore.mintID()` and only advances `t.state.HighWater` after a successful durable reservation
- IDs are not reusable after a rejected transaction or crash, and the durable reservation is not silently undone
- the native logic is designed to avoid “ID leaks after crash” and is stricter than a plain JS-style in-memory counter

The source contract is about `session/group commit` and publication ordering, not about reusing or pre-reserving IDs across failed transactions. The safe rule is:
- do not add a general pre-reserved “ID pool” unless the exact contract is reviewed end-to-end
- if a pool is used, it must be durable, bounded, and incapable of returning an already-visible ID after crash/rejection
- any unused reserved IDs must not silently convert the public high-water/next-ID contract into a gap-based allocation without a separate design review

Recommendation for this port:
- keep the existing native reservation path (`Tx.MintID()` -> `storeCore.mintID()`) as the durable source of truth
- allow overlapping callback admission without exposing a new pool that can duplicate or hide IDs
- if a pool becomes necessary later, treat it as a strictly durable reservation ledger with explicit reuse and crash semantics, not as a JS-like opportunistic fetch from the current write head

This is the safer minimal path because it preserves the stronger native escaped-ID non-reuse guarantee and does not create a false source parity claim by weakening the allocator contract.

## 6. Actual implementation risks

1. Stale reads from overlapping admission
   - If we admit a callback while an earlier storage call is in flight without keyed gates, the next commit can read a state that is not yet durable.
   - The fix is to keep per-key `Writer.gate` semantics and to treat unkeyed scans as a global drain.

2. Silent substitution of a later image for an earlier receipt
   - The current native `Session.settleBatch()` explicitly checks `expectedSeq` and requires a matching `receipt.seq`.
   - The group port must keep that exact check; it must not accept a later `core.state` as the image of an earlier part.

3. Outcome ordering after failure
   - Source `414` and `461` depend on first failure boundary and `SessionFailed` classification.
   - The native `fail()` and `SessionFailed` logic must remain authoritative; later jobs must not resolve as if they were independent of the first failure.

4. Publication scope mismatch
   - `source698` and the storage publishing-scope tests require publication order and scope boundaries more specific than simple channels or a single sink callback.
   - Go channel timing is not equivalent to JS macrotask ordering; the port must expose an explicit `hop`/scope boundary, not rely on incidental scheduling.

5. Job count drift / close deadlocks
   - `source920` requires a precise `outstanding` count and zeroed idle state.
   - close and failed-storage drains must be kept in sync with the writer state or the Session can hang with a leaked `gate` or leaked outstanding count.

6. ID reuse under crash or rejection
   - A “pre-reserved pool” is only safe if it is durable and has a strict non-reuse guarantee across process restart.
   - Without that guarantee, it is not a safe porting shortcut.

## 7. Recommendation: staged minimum safe patch

1. Add `sessionWriter` and `part` queue semantics (`Writer` equivalent) but keep the Session line as the serial owner of callback execution and final adoption.
2. Add keyed read gates (`touch` / `drained`) and preserve `ALL_KEYS` semantics.
3. Generalize `settleBatch` to a multi-receipt/part path without replacing `commitReceipt.checkNext()` / `nextImage()` checks.
4. Keep publication and outcome sequencing aligned to the JS `Writer.#publish` logic, with explicit failure boundary semantics.
5. Add the 256 `outstanding` counter and exact idle restoration before enabling more aggressive overlap.
6. Keep `Tx.MintID()` and `storeCore.mintID()` unchanged unless a separate allocator design proves a non-reuse-safe pool.

This is the narrowest design that actually addresses the source contracts in `170`, `414`, `441`, `461`, `698`, and `920` while preserving the stronger native durable-ID and receipt invariants.
