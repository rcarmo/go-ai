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

## Canonical queued drafts and extended transaction assertions

The literal source209-230 busy-steer packet now passes, including the host check/document write, one commit containing admission and host entry, exact Sent IDs, exact `[{id,mode:"steer",content:"steer"}]` Inbox and unchanged running input IDs. Its initial failure exposed a private `input` envelope in the document, not an assertion or fixture mismatch.

Input Inbox producers now store canonical string/block `content` and optional picked `entry` metadata, retaining explicit null data. Public queued writes and compaction writes store a canonical EntryDraft under `entry`; placement IDs/task attribution/private Value are not included. The native generation checkpoint and private submission payload still drive execution: this aligns queued document production, not canonical builtin execution or imported-queue interoperability.

Additional assertions in the same consolidated suite cover:

- Block input/metadata exact document shape, with an independent original-source probe.
- Idle input progresses without Resume or Submission.Wait; actual publication signals replace source polling.
- Failed-run continue precedes the host table write and settles without a fresh progress request.
- Definite pre-append SQLite failure preserves failure identity and leaves no request/task/entry/run after a file-backed reopen. This is source-derived: the injected value is StorageRejected, not coverage of every arbitrary/uncertain Storage failure. The first test run incorrectly tried to close storage already owned/closed by Session; correcting that fixture required no runtime change.
- A model-visible queued write has an exact canonical entry draft and is eventually placed/completed. The fixture holds a provider rather than the source's tool, so it is source-derived, not acceptance of that entire source case.

Baseline failures were the exact input and passive-write Inbox shapes. The write codec is shared with compaction admission; Staticcheck caught and prompted correction of an error-variable shadow in that producer before acceptance.

Latest full durable **418.205s**, tools **19.360s**; earlier full run **396.479s/16.846s**. Ten focused shuffled repetitions, seed42: **16.943s**. Repository vet, Staticcheck v0.8.2 and logging pass. Three independent compiler overlays introducing a noncanonical input field, replacing `entry` with a noncanonical field, or omitting SubmitIn's progress request fail semantic tests; all scratch is deleted. Reference now **189 unchanged source cases +49 probes**, including the new canonical queued-draft probe (181 original-file cases +eight unchanged Storage cases +35 admission probes +14 other probes).

The full busy-steer shape packet, idle progress and failed-run continuation gaps listed in the preceding checkpoint are now covered. The source tool-bound placement variant, complete arbitrary/uncertain Storage failures, trace/link/span propagation, task-bound lifetime/close and exhaustive resume/scheduling order remain open. See [the completion report](upstream-conformance-report.md) for the broader unfinished migration; no release or full-parity acceptance is claimed.

## Canonical queued write consumption

Write boundaries now consume the Inbox item's EntryDraft directly, following pinned `harness/inbox.ts:94–111`. They no longer infer a write from the native submission Value, look up its record before appending, or skip the append because the target record is already terminal. `PlaceSubmission`/`SettleSubmission` queue the transition; existing assembly resolves the target after the callback and preserves terminal records unchanged.

Test-first source-derived controls failed on all three differences: a canonical queued record without private payload produced an empty pi.user entry; an Inbox write targeting a terminal record was suppressed; and a missing record interrupted admission inside the callback. The real admission/provider path now preserves the queued kind, explicit-null data and model blocks. Matching independent original-source probes pass. These are source-derived controls, not a claim that the whole original Inbox suite has native equivalents.

`placeQueuedWrites` and final write placement now share the same write lane. Numeric staleness sees preceding writes' actual head changes; canonical `head:"self"` resolves to the freshly assigned entry ID. The historical native reset item path and private input/generation/tool scheduler still remain; full canonical builtin execution is not closed. This slice verifies the block-content form of the source model-message union, not every imported model encoding.

Existing native rollback controls now inspect queued submission changes before callback refusal rather than assuming immediate settlement. Their positive retry still checks the fully settled record, persisted entry, cleared Inbox and exact sequence. Boundary fixtures now create canonical records and supply canonical queue drafts. Application note callers/assertions use explicit source data rather than the private Value field; no acceptance-time envelope normalisation was added.

Verification: full durable **409.428s**, tools **16.262s**; ten focused shuffled repetitions, seed42, **115.206s**; repository vet, Staticcheck v0.8.2 and logging pass. Three compiler overlays restoring private payload consumption, early terminal suppression or early missing-record lookup each fail semantic assertions. Disposable overlays are deleted.

Selected reference coverage is now **189 unchanged cases +51 probes**. Full original-corpus execution and remaining harness/resource blockers are reported separately in [the oracle note](upstream-oracle.md); that full run is not accepted as passing. Source-derived probe fixture mistakes in argument placement/receiver were corrected before the passing selected receipt. The D20 prior close caveat and all broader release gates remain open.

## Canonical string content and placement clock

The original idle-input packet (input-entries:99–122) now passes with its exact kind, data, transcript, settled record, historical context and provider text. The persisted user message is content:"ann: hi", not a text-block array, and its timestamp is42 from the placement clock. Other inputs placed in that same boundary share the clock value. Existing native assertions now require the string arm explicitly rather than normalising both encodings.

The original MemoryStorage capture now includes user/system string content, empty strings, timestamp zero and nested edit replacements (three conversations, nine entries). Public JSON and SQLite reopen retain these fields. MessageReceipt.ContentText represents the string arm in Go; it is never a second wire property. Both arms at once, string assistant/tool-result content, caller ownership leaks and byte-budget bypasses are rejected by dedicated controls. Translation to a text block happens only for the block-only Go AI runtime. Compaction serialization also retains string input.

These changes do not complete canonical builtin generation/tool execution, migrations, every input/block/message variant, UTF-16 text or the public API mapping. Selected original execution passes200 unchanged cases plus51 probes; see the oracle note for asset provenance and unresolved image/live-provider acceptance. Verification: uncached sequential durable830.400s/tools26.898s; ten focused shuffled repetitions15.726s; vet, Staticcheck v0.8.2, logging and register pass. Four compiler-overlay faults (string mutation, omitted timestamp zero, wrong placement clock and dropped compaction string) each fail their semantic assertion; overlays are deleted. An earlier complete run failed only the remaining block-only history assertion (874.059s); that assertion now requires canonical string content. Earlier shorter diagnostic runs were interrupted/timed out and are not passing receipts. This is ordinary development verification, without race or pre-release CPU/allocation acceptance.

## Canonical Inbox input consumption

Input append boundaries now read the selected Inbox item, including picked kind/data, rather than reconstructing it from the private generation checkpoint. Queued/final, request-preparation and no-model append paths share this source appendInput codec. There is no checkpoint-payload fallback when the item is absent. Compaction admission still consults historical checkpoint input for unplaced work; full canonical selection/execution/migrations remain open.

The new literal matrix first failed because an empty block array became a string, then because copying collapsed it to null and empty text omitted its required text property. String empty, array empty and a text block containing an empty string now remain distinct. Slice copying retains nil/empty identity of the union arm without sharing mutable blocks. A held-provider control changes only the real queued Inbox item to content:[], kind:app.canonical, data:null; the eventual entry matches that item, not the old private checkpoint. Both independent original probes pass. Five native legacy Inbox fixture sites now use canonical content instead of an input envelope; unplaced ownership fixtures seed an actual Inbox item without changing their rollback/foreign-owner assertions.

Verification: full uncached durable842.589s/tools26.862s; ten focused shuffled repetitions35.080s; four semantic compiler faults (content authority, metadata authority, empty-array copy, required empty text) rejected. Vet, Staticcheck v0.8.2 and logging pass. Original selected execution:200 unchanged cases plus53 independent probes,253 total, zero skipped. Early probe receiver/address mistakes were fixed; failing attempts are not green receipts. Overlays and analysed logs are deleted. This remains development evidence, not complete alignment or pre-release profiling acceptance.
