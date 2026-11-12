# One-transaction submission admission

Harness request lookup and fresh admission now run in one Session commit, matching `Submissions.submit` and `admitSubmission` in the pinned source. The native split read/write admission is removed. This follows `ce03209`; full submission/runtime parity remains open.

Target: pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Source contract and baseline

`src/harness/submissions.ts:59–71` runs admission inside one `commitWith`; `admitSubmission` at233 reads the conversation-scoped request index before creating anything. A competing raw transaction queued while that admission reads cannot overtake its fresh write.

The test-first native wrapper captures the actual storage-read state, queues a raw creator, and waits for that job's pending-queue signal. No sleep, polling or scheduler hint establishes the ordering. Before the change, the queued raw record received ID2 and the submission owning the earlier read received ID7. The native initial read had released its job before starting its write.

The independent reference probe queues a raw creation from original Storage.submissionByRequest during `root.submit`. The original submission receives the earlier ID. The native test now asserts the same ordering through the real public Submit path.

A second test-first control showed that Tx submission/request readers ignored installed wrapper methods and read their Snapshot instead. Original transaction readers delegate to the guarded Storage methods. The native fallback now preserves that authority; a corresponding original-source probe passes.

## Changes

- `submitAdmission` validates/normalises the request, then enters one private `submitOnLine` commit. The old initial `readTasks` job and separate `submitNewAdmission` helper are removed.
- The commit checks lifetime, binding and conversation existence, performs the primary Tx request lookup, and either returns the known record without writes or performs fresh admission. Request type conflict and deduplication still precede busy/admission mutations.
- Dedup trace results use canonical SubmissionRecord status. The physical publication observer explicitly projects its native envelope at that internal boundary; public results do not expose private payloads.
- Tx.Submission and Tx.SubmissionByRequest delegate to installed wrapper methods when the wrapper is authoritative, with the existing Session error guard. Native direct-store reads still use the published committed image.
- Existing private-seam lifetime/image-input tests call the unified helper; their ownership, cancellation and close assertions remain.

This closes the reproduced lookup/write job gap. It does not accept every source admission contract: the `submit(tx, draft)` form, complete resume/scheduling order, all queued/reset/compaction edge paths, D04 caller continuations, canonical builtin execution and the full exported-symbol audit remain open.

## Development verification

- Full durable **410.503s**, tools **18.203s**.
- Ten focused shuffled repetitions, seed42: **14.037s**.
- Repository-wide vet, Staticcheck v0.8.2 and logging pass.
- Independent compiler-overlay faults restoring an extra read job before admission or bypassing transaction wrapper methods both fail the new semantic assertions. Disposable overlays/scripts are deleted.
- Reference: **166 unchanged source cases +48 independent probes**:158 tests from eight original files, eight unchanged Storage conformance cases,35 admission probes and13 Storage/transaction/admission probes. Source hashes/tree checks retain their existing provenance limit: the retained snapshot has no `.git`, so commit identity is not independently reverified.

Tests remain in the existing version-neutral submission/transaction suites. These are development receipts, without race or pre-release CPU/allocation acceptance. The earlier queued progress/close error-order caveat remains open under D20; the full parity/release checklist remains incomplete.

## Caller-owned transaction admission

`ConversationHandle.SubmitIn(tx, input)` now maps the original `submit(tx, draft)` form. It returns the submission ID, not a handle. `InvocationConversation.SubmitIn` retains the invocation binding. The implementation shares the actual admission body with standalone Submit: it does not open a nested commit or acquire Harness.mu while the caller owns the Session line.

The Session-created Tx has a private self-identity witness. A foreign Session or a wrapper retaining the owner pointer does not acquire transaction authority. This implements original WeakSet object membership without retaining every completed transaction. The unconditional first-table-write fence precedes all request/dedup paths and retains the exact original diagnostic. Escaped sealed transactions reject. Scheduler progress is requested before admission; writes remain provisional until the caller commits. The transaction context supplies the admission trace; the caller-owned span ends inside the callback, as in the source.

Test-first API controls failed before implementation. Tests remain in `transaction_tables_test.go`:

- Source callback rollback: input/request creation, entries and tasks disappear; no run or sequence advance.
- Source ownership refusal: foreign Session and a wrapper carrying private ownership data reject with the original diagnostic. The Go wrapper check is not JavaScript Proxy coverage.
- Source caught idle rejection: no sequence advance or started run. The fixture materialises builtin documents before the baseline, matching openChat.
- Source busy-continue/whenIdle and dedup packets: continue rejects, steer is the only queued ID, repeated request returns the same ID without a commit.
- Source-derived atomic host-document/admission/host-entry control, using a passive write rather than the original busy-steer packet. It checks one commit and retained host data; it does not claim the source packet's exact Inbox/Live shapes.
- Source-derived first/second-admission and caught failed-write fences, plus sealed-transaction rejection.

The original runner now includes all23 unchanged `harness-submit-in-commit.test.ts` cases. Reference totals are **189 unchanged source cases +48 probes**:181 cases across nine original files, eight unchanged Storage conformance cases,35 admission probes and13 other probes. These original executions do not mean all23 assertions have native counterparts.

Verification: full durable **421.225s**, tools **15.752s**; ten focused shuffled repetitions (seed42) **9.547s**; repository vet, Staticcheck v0.8.2 and logging pass. Two independent compiler overlays removing exact object identity or the unconditional fence fail the corresponding semantic tests. Disposable overlays are deleted. An earlier full run failed only the new host-document assertion's incorrectly typed expected number; its corrected assertion and the subsequent full suite pass.

Still open: the literal busy-steer atomic packet's complete canonical Inbox/Live assertions, Storage failure/reopen, model-visible passive writes, failed-run continuation, trace carriers/running-trace links, task-bound lifetime/close and exhaustive resume ordering. The private builtin scheduler/envelopes remain historical. Complete callback/result/options semantics, D04, D20's prior close caveat and pre-release profiling remain unaccepted. No parity/release claim follows from this slice.
