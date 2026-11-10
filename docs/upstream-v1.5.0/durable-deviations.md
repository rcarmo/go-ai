# Durable deviations from pi-durable v1.5.0

The native durable package is not yet semantically equivalent to pi-durable v1.5.0. This register records **15 open confirmed incompatibility groups**, **two fixed groups**, **10 unverified areas**, **one verification gap** and **one approved backend exclusion**. Every entry has evidence, a fix path and acceptance criteria. These groups are not a count of individual failing assertions.

Target: `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native baseline: `a89368c99be4f270baf1108a143b82fb08f849e9`. The uncommitted `session_writer.go` and direct writer tests are a prototype, excluded from implementation and parity receipts here. The inventory includes currently known deviations; the assertion-level audit is incomplete and may find more.

- [Machine-readable register](durable-parity-gaps.json): IDs, evidence, fixes, acceptance, dependencies and blockers.
- [Complete durable review obligations](durable-review-obligations.tsv): all **165 pinned test/support/fixture paths**, including **101 test files**, with original SHA-256, historical disposition, outstanding review and gap IDs.
- [Semantic-port receipts](durable-semantic-port.md): completed bounded slices and retained measurements.
- [Historical crosswalk](upstream-tests-f9a26365.tsv) and [dependency queue](dependency-tests-f9a26365.tsv): broader ai/schema scope. Historical dispositions have not been promoted.

## What can be fixed

Each open confirmed gap has an implementation route. No external blocker has been identified. Four representation decisions must be settled before promising complete equivalence:

1. Public task/message/error values must be lossless, including arbitrary refusal values and empty/absent fields (D03, D07–D09, D12).
2. Existing native SQLite data must remain readable. D08 now retains presence using the existing optional string field; metadata already lost by older writers cannot be recovered. Original-database interoperability still needs explicit format detection and migration rules (D11).
3. Shared readonly views need an enforceable immutable representation and revision identity. Writable Go maps and detached copies cannot both satisfy the original contract (D14–D17).
4. Go admission, callback results and publication scheduling need an agreed language mapping that preserves observable order and caller semantics (D01–D06, D10, D13).

Read-only external viewers also require a storage reader/refresh design compatible with single-writer ownership (D27). Other reviewed areas may expose further blockers. Fixability is established entry by entry with source tests; it is not established merely by listing a proposed implementation.

SQLite-only support is the authorised product exclusion (D25). Shared Storage assertions still apply. Restoring Memory/JSONL/IndexedDB availability would require a scope change. Arbitrary wrapper support, original public record shapes and readonly viewers have **not** been waived.

## Fix sequence

1. Capture exact original records and method/result shapes; decide versioned envelopes, arbitrary error values, callback results and wrapper authority (D03, D07–D13).
2. Co-design the Session admission/read-key/writer/publication state machine (D01–D06). These entries have reciprocal dependencies because they must be integrated and tested together. Preserve serial callbacks, exact-prefix receipts, first-cause fencing, all outcome gates and the 256-job admission bound.
3. Introduce immutable mounted values and public observer scheduling (D14–D17), then readonly viewer refresh (D27). Keep existing ownership and close guarantees.
4. Complete scheduler/lifecycle/resource/dependency contracts and the full source crosswalk (D18–D24, D26). Schema/engine gaps need separate source-backed packets.

No caller workaround, normalised comparison, private-only hook, deep-copy alias or optional replacement API closes an entry. Close an entry only after the unchanged source assertions and necessary durability/ownership controls pass against the integrated public path. Preserve old data/assets; no automatic migration or weakening of bounds.

## Gap index

| ID | State | Area |
| --- | --- | --- |
| [D01](#d01--independent-session-admission-and-storage-batching) | confirmed | Independent Session admission and storage batching |
| [D02](#d02--keyed-unpublished-read-dependencies) | confirmed | Keyed unpublished read dependencies |
| [D03](#d03--failure-boundaries-and-arbitrary-rejectionrefusal-values) | confirmed | Failure boundaries and arbitrary rejection/refusal values |
| [D04](#d04--publication-turns-continuations-and-storage-publication-scope) | confirmed | Publication turns, continuations and storage publication scope |
| [D05](#d05--256-started-but-undelivered-job-bound-and-ordered-outcomes) | confirmed | 256 started-but-undelivered job bound and ordered outcomes |
| [D06](#d06--deferred-changes-unload-reload-deadlines-and-close-during-writer-flight) | unverified | Deferred changes, unload, reload, deadlines and close during writer flight |
| [D07](#d07--original-harness-message-kinds-and-model-payloads) | confirmed | Original harness message kinds and model payloads |
| [D08](#d08--present-but-empty-submission-requestid-and-deduplication) | fixed | Present-but-empty submission requestId and deduplication |
| [D09](#d09--built-in-public-task-writes-are-refused) | confirmed | Built-in public task writes are refused |
| [D10](#d10--opensession-rejects-arbitrary-conforming-storage-implementations) | confirmed | OpenSession rejects arbitrary conforming Storage implementations |
| [D11](#d11--persisted-sqlite-schema-and-record-envelopes-differ) | confirmed | Persisted SQLite schema and record envelopes differ |
| [D12](#d12--storage-readquery-result-shapes-and-remaining-public-record-dtos) | confirmed | Storage read/query result shapes and remaining public record DTOs |
| [D13](#d13--session-commit-callback-resultoptions-and-remaining-exported-api-surface) | confirmed | Session commit callback result/options and remaining exported API surface |
| [D14](#d14--shared-readonly-conversation-view-and-observer-values) | confirmed | Shared readonly conversation-view and observer values |
| [D15](#d15--viewentry-identity-lifetime-mount-collection-and-empty-array-identity) | unverified | View/entry identity lifetime, mount collection and empty-array identity |
| [D16](#d16--public-observecommits-host-dispatch-and-waiter-ordering) | confirmed | Public observeCommits host dispatch and waiter ordering |
| [D17](#d17--synchronous-document-observer-readonly-identity-and-full-contract) | unverified | Synchronous document observer readonly identity and full contract |
| [D18](#d18--resource-policies-safe-number-domain-and-persisted-limits) | unverified | Resource policies, safe-number domain and persisted limits |
| [D19](#d19--grouped-scheduler-ownershipwait-graph-and-read-races) | unverified | Grouped scheduler ownership/wait graph and read races |
| [D20](#d20--lifecycle-restartrecovery-hook-and-submission-corpus-coverage) | unverified | Lifecycle, restart/recovery, hook and submission corpus coverage |
| [D21](#d21--telemetry-environments-cross-owner-export-and-diagnostic-parity) | unverified | Telemetry, environments, cross-owner export and diagnostic parity |
| [D22](#d22--codemode-engineworker-and-complete-restart-semantics) | unverified | Codemode engine/worker and complete restart semantics |
| [D23](#d23--schema-dependency-and-host-regexpcompiler-semantics) | unverified | Schema dependency and host RegExp/compiler semantics |
| [D24](#d24--providerai-semantics-inherited-by-durable-execution) | unverified | Provider/AI semantics inherited by durable execution |
| [D25](#d25--original-alternative-storage-engines) | scope-exclusion | Original alternative storage engines |
| [D26](#d26--assertion-level-crosswalk-and-release-verification-are-incomplete) | verification-gap | Assertion-level crosswalk and release verification are incomplete |
| [D27](#d27--read-only-sessionharness-mode-and-concurrent-external-viewers) | confirmed | Read-only Session/Harness mode and concurrent external viewers |
| [D28](#d28--raw-storage-initial-state-explicit-ids-and-empty-batches) | fixed | Raw Storage initial state, explicit IDs and empty batches |
| [D29](#d29--empty-unanswered-reason-for-continue-submissions) | fixed | Empty unanswered reason for continue submissions |

## Evidence and acceptance

`confirmed` means either a reproduced failing oracle or a direct source/native API/representation contradiction. `unverified` means incomplete source coverage, not an asserted runtime failure. `fixed` closes only the named bounded deviation with the listed controls; it does not promote whole upstream files or establish full package parity. Line numbers below refer to the pinned source. Each evidence statement distinguishes a fresh result from historical measurements or static review.

### Writer installation status

The writer is installed on Session and the full native suite passes. Admission
ordering now survives read-gate waits while publication takes only the memory lock.
D01–D04 remain open: ALL_KEYS barriers still serialise whole-state transactions;
source170 disjoint admission/batching and the Session-level outcome fence, failure
boundary and continuation assertions have not been established. See
[the current crosswalk](../../notes/writer-install-crosswalk.md#writer-installed-admission-queue-retained-across-touch-supersedes-attempts-57).

### D01 — Independent Session admission and storage batching

**confirmed.** Fresh adapted source170 oracle: callbacks b/c do not run during held a; calls [1,1,1], expected [1,2]. Storage multi-batch support alone passes.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/storage_group.go`](../../durable/storage_group.go).
- Fix: Separate the serial mutation line from the writer flight. Transfer sealed parts to one writer and batch the waiting parts. Keep callbacks serial; do not unlock around arbitrary concurrent mutation.
- Acceptance: Source170 unchanged: b/c admitted before release; no early publication; calls [1,2]; distinct increasing sequences; a/b/c outcome order.
- Dependencies: D02, D03, D04.
- Blocker/decision: none identified; acceptance still required

### D02 — Keyed unpublished read dependencies

**confirmed.** Current global Session line blocks all reads/callbacks through settlement. Existing serial barrier tests cannot establish keyed overlap.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/session_read_line.go`](../../durable/session_read_line.go); [`durable/transaction.go`](../../durable/transaction.go).
- Fix: Register exact write keys, including newly created identities, document addresses, ancestry and selected held changes. Touch newest-key gates; unkeyable scans drain the admitted prefix. Revalidate candidate graph against effective pending state.
- Acceptance: Source201-380: leaked entry, duplicate document creation, new conversation, all line reads, unkeyable scans, held-document reads. Verify disjoint jobs progress and dependent jobs wait.
- Dependencies: none recorded.
- Blocker/decision: Footprint must cover whole-image finalisation and ownership reads before global baseline checks can be narrowed.

### D03 — Failure boundaries and arbitrary rejection/refusal values

**confirmed.** Closed: public boundaries carry the reported value with presence, independently of Go diagnostics. `CommitResult.Refused` is `*Refusal` (nil = absent) and returns the value verbatim; the Session failure slot keeps the cause chain intact for classification and exposes the thrown value through `*Failure` (`SessionFailed.Failure`, `SessionEnd.Failure`), including present null/undefined/false/0/empty string; uncertain-append poisoning keeps the reported value reachable without changing its diagnostic text. Open: multipart own-error set, first-failing-job recording and prefix publication before fencing still require the independent writer flight.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/session_failure.go`](../../durable/session_failure.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/storage_group.go`](../../durable/storage_group.go); [`durable/storage.go`](../../durable/storage.go); [`durable/session_group_commit_outcomes_test.go`](../../durable/session_group_commit_outcomes_test.go); [`durable/storage_refusal_value_v150_test.go`](../../durable/storage_refusal_value_v150_test.go).
- Fix: Remaining: record first failing job and same-call own-error set; publish received prefixes before fencing, then settle all gates/outcomes; later calls inherit the first cause. Value/presence representation is implemented in `durable/session_failure.go` and `durable/storage_public.go`.
- Acceptance: All source414-562 parameterised cases, including undefined/null/false/0/empty string; own vs SessionFailed identity; real rollback and uncertain COMMIT; earlier durable prefix survives reopen.
- Dependencies: D01.
- Blocker/decision: Remaining scope depends on D01: without an independent writer flight there is no single place that owns multipart publication, fencing order and the same-call own-error set.

### D04 — Publication turns, continuations and storage publication scope

**confirmed.** The installed writer publishes off the admission queue and supports awaitable internal publication scopes. The native host uses the no-scope path. Source 698/721/753/770/796/815 continuation-order assertions still require Session-level verification.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session_publication.go`](../../durable/session_publication.go); [`durable/session_receipt.go`](../../durable/session_receipt.go); [`durable/session_sync_observers.go`](../../durable/session_sync_observers.go).
- Fix: Design one serial publication dispatcher with awaitable scope completion, explicit continuation/outcome ordering and failure rechecks immediately inside the deferred scope. Retain detached exact-prefix receipts and contain scope throws according to the source.
- Acceptance: Source698-815 unchanged ordering assertions; defer a scope, throw without executing it, fail between parts and while scope is pending. No later part publishes.
- Dependencies: D01, D03.
- Blocker/decision: Go has no JS microtask/macrotask queue. A scheduler/bridge must reproduce observable order; runtime.Gosched or sleeps do not establish parity.

### D05 — 256 started-but-undelivered job bound and ordered outcomes

**confirmed.** Session currently serialises through settlement and has no source-equivalent outstanding-job accounting. Writer queue alone would otherwise permit unbounded admissions.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go).
- Fix: Bound jobs at source admission, not merely queued writes. Account for reads, empty commits, callback errors and held parts. Deliver outcomes in line order; release room only at delivery.
- Acceptance: Source390 and920: only255 additional jobs start behind first; all300 finish; outstanding/unpublished/reads return to zero; cancellation and errors release every slot.
- Dependencies: D01, D04.
- Blocker/decision: none identified; acceptance still required

### D06 — Deferred changes, unload, reload, deadlines and close during writer flight

**unverified.** Serial deferred persistence has focused passing controls; concurrent source 325/360/662/680/839/857/885/902 interactions are not ported as a complete group contract.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`test/session-deferred-commits.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-deferred-commits.test.ts).
- Native/evidence: [`durable/session_deferred.go`](../../durable/session_deferred.go); [`durable/session_timers.go`](../../durable/session_timers.go); [`durable/session.go`](../../durable/session.go).
- Fix: Admit memory-only parts in the same writer order; pin held revisions and store each selected revision once. Put unload/reload/deadline/close flushes behind the flight and join their outcomes.
- Acceptance: All named source cases; close drains admitted work and held changes; no duplicate persistence, lost revision, early memory publication or gate/worker leak.
- Dependencies: D01, D02, D04, D05.
- Blocker/decision: none identified; acceptance still required

### D07 — Original harness message kinds and model payloads

**confirmed.** Fresh pending oracle fails live and after reopen: kind message/value instead of pi.user/pi.assistant/model. Readers accept both. Previous attempted producer switch caused 37 failures and one hang because native receipt errorCode/tagged details violate contribution validation.

- Source: [`src/entries.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/entries.ts); [`src/harness/generation.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts); [`test/harness-input-entries.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-input-entries.test.ts).
- Native/evidence: [`durable/message_entry.go`](../../durable/message_entry.go); [`durable/entry_record_wire.go`](../../durable/entry_record_wire.go); [`durable/registry.go`](../../durable/registry.go); [`durable/entry_definition.go`](../../durable/entry_definition.go).
- Fix: Capture original user/assistant/tool-result receipts covering errors, diagnostics, details, empty fields and all content blocks. Introduce lossless public-message conversion, keep legacy readers, then switch every producer. Separate private receipt witnesses from original public fields.
- Acceptance: Pending entry_message_kinds_v150_test.go unchanged; original tool-result data.diagnostics, model details/error fields, exact required-zero/empty presence; full generation, tool, compaction, abort and reopen regressions.
- Dependencies: none recorded.
- Blocker/decision: A tag-only switch loses native receipt semantics; complete receipt conversion is prerequisite.

### D08 — Present-but-empty submission requestId and deduplication

**fixed.** Baseline strict replay lost requestId empty string on submission12. The replay now compares every captured record exactly, without dropEmptyRequestID. Present-empty request keys survive public/native codecs, transaction and Harness admission, lookup, lifecycle updates, frame replay and checkpoint replay.

- Resolution: RequestIDPresent on SubmissionRecord, Input and private Submission retains optional-string presence. The existing requestId JSON field encodes the bit: present empty remains an empty string; absent and nonempty legacy encodings are unchanged. Nonempty RequestID still implies presence. Empty-key lookup and dedup distinguish absent keys. Native replacement/uniqueness guards remain enforced.
- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_replay_v150_test.go`](../../durable/storage_replay_v150_test.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/storage.go`](../../durable/storage.go); [`durable/submission_native_wire.go`](../../durable/submission_native_wire.go); [`durable/submission_presence_v150_test.go`](../../durable/submission_presence_v150_test.go).
- Original fix path: Add explicit presence to the versioned private representation, propagate it through creation, copy, update, snapshot and indexing, and define legacy-file decoding. Preserve empty key as an actual per-conversation dedup key.
- Acceptance: All12 captured batches with exact JSON equality and no dropEmptyRequestID; empty vs absent dedup/lookups before and after reopen; transitions, duplicates and caller ownership.
- Verification: TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches: unchanged captured records, seq1..12, exact comparison. TestV150SubmissionRequestPresenceLegacyWire, TestV150SubmissionRequestPresenceReopenAndCheckpoint, TestV150SubmissionRequestPresenceSessionAndHarness.
- Dependencies: none recorded.
- Blocker/decision: No new persisted field, schema change or automatic migration. Presence already lost by older writers cannot be reconstructed; old absent fields stay absent. General raw Storage duplicate-request/update policy still needs D12 conformance review.

### D09 — Built-in public task writes are refused

**confirmed.** public nativeTask refuses every pi.* kind, while original Storage.commit accepts task records independently of native runtime checkpoint adapters. Namespace-wide refusal is stricter than the original.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/task_definition.go`](../../durable/task_definition.go); [`durable/task_record_wire.go`](../../durable/task_record_wire.go).
- Fix: Separate storage canonical records from runtime interpretation. Preserve exact public task data, including opaque checkpoints; translate only when execution needs a known adapter. Do not ban application kinds merely by pi. prefix.
- Acceptance: Replay captured original built-in and opaque task records through Storage.Commit/read/reopen; invalid runtime phases fail only at source-equivalent execution boundary.
- Dependencies: D11, D12.
- Blocker/decision: Private execution envelope is not reversibly equivalent to all original task records.

### D10 — OpenSession rejects arbitrary conforming Storage implementations

**confirmed.** OpenSession requires private nativeStorage/nativeCore. Adding public Commit does not admit a source-shaped wrapper. Session settlement uses core directly, so original BatchStorage hold/refusal/scope wrappers cannot be used faithfully.

- Source: [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`test/storage-runtime-boundary.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/storage-runtime-boundary.test.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/session_receipt.go`](../../durable/session_receipt.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/types.go`](../../durable/types.go).
- Fix: Use the original Storage capabilities at the Session boundary. Keep a verified native fast path only if it cannot bypass wrapper authority. Provide exact-prefix adoption for non-native storage without exposing private frames.
- Acceptance: Run original wrapper-shaped hold/refusal/publishing tests through OpenSession; wrapper reads/writes must intercept all relevant calls; native prefix-adoption and ownership controls remain green.
- Dependencies: D11, D12.
- Blocker/decision: Current ID reservation also takes the native core gate. A hold after that gate is not the original before-delegation seam.

### D11 — Persisted SQLite schema and record envelopes differ

**confirmed.** Native SQLite stores its own configuration/frames/checkpoints, not the original table schema. Public codecs deliberately leave existing native storage unchanged. Direct original-database interoperability is unverified and cannot be inferred from wire equality.

- Source: [`src/storage/sqlite/migrations.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/sqlite/migrations.ts); [`test/sqlite-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/sqlite-storage.test.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go); [`durable/types.go`](../../durable/types.go).
- Fix: Specify supported original/native format detection, read/export/import or explicit migration with version markers, rollback and old-reader behaviour. Never reinterpret private frames as public JSON. Preserve user data and durability during migration.
- Acceptance: Open/read original-produced DB fixture or verified lossless migration in both directions as required; native legacy reopen; crash at each migration boundary; unsupported version rejection; no partial conversion.
- Dependencies: none recorded.
- Blocker/decision: Database drop-in interchange requires an explicit migration design and fixtures, not a marshaler change.

### D12 — Storage read/query result shapes and remaining public record DTOs

**confirmed.** Storage still requires native Apply/Snapshot/Limits and exposes raw Task, Submission and materialised Document; original entry read returns {entry,commitSeq}, documents distinguish record/stored content, and original single-record methods differ. Captured cursor and selected-record wire slices do not cover the whole API.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts); [`test/session-tables.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-tables.test.ts).
- Native/evidence: [`durable/types.go`](../../durable/types.go); [`durable/storage.go`](../../durable/storage.go); [`durable/scan.go`](../../durable/scan.go); [`durable/document_history.go`](../../durable/document_history.go).
- Fix: Inventory every original Storage method and public union, with an explicit Go spelling map but identical data/absence/order semantics. Add missing single-record/head/document methods and public DTO boundaries; keep private envelopes private.
- Acceptance: Run pinned storage conformance through only the declared public interface; exact method result fixtures for tasks/submissions/documents/entries, scopes/points/retirement/forks, query fields and absence.
- Dependencies: none recorded.
- Blocker/decision: none identified; acceptance still required

### D13 — Session commit callback result/options and remaining exported API surface

**confirmed.** Original Session.commit returns callback result and accepts persistence options; Go Commit accepts func(*Tx) error and returns sequence. Separate CommitDeferred and caller-side result capture are alternate caller semantics. Complete exported symbol/type conformance has not been enumerated.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/index.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/index.ts); [`test/session-tables.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-tables.test.ts); [`test/session-definitions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-definitions.test.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/transaction.go`](../../durable/transaction.go); [`durable/session_deferred.go`](../../durable/session_deferred.go).
- Fix: Create a source-export-to-Go mapping for all functions, interfaces, tokens and unions. Preserve result/options semantics in the accepted primary API; account for Go generic-method limitations without requiring application semantic workarounds.
- Acceptance: Source callback-value tests, empty/error/async-equivalent results, deferred options on same entry point, tokens, typed lookup and original defaults; compile an external consumer using only the accepted mapped API.
- Dependencies: none recorded.
- Blocker/decision: Public API compatibility change; Go cannot define generic methods. Decide the lossless language mapping before treating it as drop-in.

### D14 — Shared readonly conversation-view and observer values

**confirmed.** Public ConversationHandle.View and ConversationWatch.Value/callbacks now expose sealed, comparable ReadonlyValue handles. Source160/330/525 watch identity assertions pass, including empty entries identity; entry-only revisions preserve docs, document-only revisions preserve entries and unaffected docs, concurrent windows share adopted docs. Explicit Copy preserves detached-content/operation tests. Public synchronous observeConversation is still absent; complete observer/API and wrapper contracts remain open.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view.go`](../../durable/view.go); [`durable/view_publication.go`](../../durable/view_publication.go); [`durable/immutable_value.go`](../../durable/immutable_value.go).
- Fix: Complete the synchronous observeConversation surface and its guard/dispatch contracts using the same readonly values. Audit wrapper fallback identity, bounded retention and every pinned assertion. Keep the watch API readonly; mutable data requires explicit Copy.
- Acceptance: Promoted source160/330/525 identity assertions retain object-identity semantics through comparable readonly handles; source160/274/330/525 content/window/identity, readonly authority, peer isolation, remount and complete synchronous-observer acceptance.
- Dependencies: none recorded.
- Blocker/decision: none identified; synchronous observeConversation and complete assertion-level acceptance still required

### D15 — View/entry identity lifetime, mount collection and empty-array identity

**unverified.** Public readonly handles now represent empty-array and individual entry-node identity. New production-watch tests verify retained entries through append/trim, late attachment shares the adopted docs, last stop removes all mount owners, and remount creates equal docs with new identity. A source570-shaped first-note/200-more SQLite test collects the actual readonly EntryRecord node within three GCs after releasing a positive retention control. Existing backing-array GC tests remain. Broader observation surfaces and repeated lifecycle stress remain open.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/transcript.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/transcript.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view_gc_oracle_main_test.go`](../../durable/view_gc_oracle_main_test.go); [`durable/view_identity_oracle_main_test.go`](../../durable/view_identity_oracle_main_test.go); [`durable/view_window.go`](../../durable/view_window.go).
- Fix: Finish lifetime coverage across synchronous observeConversation, wrappers and repeated attachment/detachment. Preserve individual node identity and bounded cleanup without retaining prior histories.
- Acceptance: Source274 remount, source570 individual record lifetime, shared slices/windows and last-detach cleanup; bounded retention and repeated attach/detach stress.
- Dependencies: D14.
- Blocker/decision: none identified; the remaining public observation path and complete lifetime crosswalk require acceptance

### D16 — Public observeCommits host dispatch and waiter ordering

**confirmed.** Private stored guard is synchronous on the line. Public SubscribeCommits is asynchronous. Neither supplies original observeCommits one-microtask host dispatch with observer membership selected at dispatch time. Private source 284 waiter control passes but is not public parity.

- Source: [`test/session-observers.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-observers.test.ts); [`test/harness-submissions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-submissions.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session_sync_observers.go`](../../durable/session_sync_observers.go); [`durable/session_local_observers.go`](../../durable/session_local_observers.go); [`durable/observation.go`](../../durable/observation.go); [`durable/testdata/upstream-main/pending/harness_commit_observer_wait_test.go`](../../durable/testdata/upstream-main/pending/harness_commit_observer_wait_test.go).
- Fix: Implement public stored observation with source dispatch/membership/error-isolation semantics, immutable safe publication values, close/failure ending and callback-before-wait/abort ordering.
- Acceptance: Complete pinned session-observers corpus, public source284 wait test, attach/detach between publication and dispatch, re-entry, listener faults, close/failure and multipart publications.
- Dependencies: D04, D14.
- Blocker/decision: none identified; acceptance still required

### D17 — Synchronous document observer readonly identity and full contract

**unverified.** Public Session/Harness/ConversationHandle ObserveDefinition has focused passing guards and lifecycle tests; returned values are detached. Some mutation fault probes survived. TaskRuntime source uses watchDoc, not synchronous observeDoc; earlier ownership-gap diagnoses need reconciliation.

- Source: [`test/session-observers.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-observers.test.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/session_document_observers.go`](../../durable/session_document_observers.go); [`durable/session_public_document_observer_main_test.go`](../../durable/session_public_document_observer_main_test.go); [`durable/observation.go`](../../durable/observation.go).
- Fix: Map the exact original DocumentObserver vs Session observer surfaces. Preserve synchronous forbidden-reentry rules and readonly identity; port remaining acquisition, cancellation, lifecycle and listener assertions before expanding APIs.
- Acceptance: Source77/87/105/132/162/241/254/308/469/496/520, detach/end guards, queued acquisition/cancellation, retirement/recreation, scheduler publication and immutable values.
- Dependencies: D14, D16.
- Blocker/decision: none identified; acceptance still required

### D18 — Resource policies, safe-number domain and persisted limits

**unverified.** Source 202 now passes new defaults and fold-threshold controls. Existing databases retain stricter persisted profiles. Native member/node/depth/bytes/record/ID policies may still reject other valid original inputs; complete policy and numeric-domain comparison is absent.

- Source: [`test/session-watches.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-watches.test.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/types.go`](../../durable/types.go); [`durable/json.go`](../../durable/json.go); [`durable/watch_pending.go`](../../durable/watch_pending.go); [`durable/delta.go`](../../durable/delta.go).
- Fix: Catalogue each original vs native bound, integer domain and default. Preserve bounded execution but implement valid original-domain behaviour; distinguish configurable operational limits from semantic rejection. Version any stored-policy change explicitly.
- Acceptance: Source202 and8000/10000 controls stay green; exact safe integers and cursor/document points; original large-input boundary fixtures; intermediate-growth, cancellation and ownership controls; strict old-profile reopen.
- Dependencies: D11.
- Blocker/decision: none identified; acceptance still required

### D19 — Grouped scheduler ownership/wait graph and read races

**unverified.** Persisted opposite-wait cycle admission is fixed. Full grouped pending-edge admission, source 491 index/sweep,608/626 reconcile marks,646 record-cache bound,699 submission lookup and714 wait continuation remain unexecuted or use limited seams.

- Source: [`test/harness-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-group-commit.test.ts); [`test/harness-scheduler-indexes.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-scheduler-indexes.test.ts); [`test/harness-scheduler-retention.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-scheduler-retention.test.ts).
- Native/evidence: [`durable/task_wait_cycle.go`](../../durable/task_wait_cycle.go); [`durable/task_scheduler.go`](../../durable/task_scheduler.go); [`durable/harness_group_commit_main_test.go`](../../durable/harness_group_commit_main_test.go).
- Fix: Repeat graph/ownership validation against the effective candidate state at admission. Port production read/index seams and measure cache/retention bounds; preserve foreign-owner and terminal/background distinctions.
- Acceptance: Whole harness-group-commit corpus plus opposite-wait oracle, longer cycles, acyclic chains, pending creation/end ordering, idle indexes, bounded cache and close signalling.
- Dependencies: D01, D02, D03.
- Blocker/decision: none identified; acceptance still required

### D20 — Lifecycle, restart/recovery, hook and submission corpus coverage

**unverified.** Selected history/close/cancellation/wait fixes pass and earlier afterRun/close-drain failures were fixture translation errors. The complete per-assertion lifecycle/restart/turn-limit/tool-hook review is unfinished; source 149 held submission read vs close has no faithful current proof.

- Source: [`test/harness-after-run.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-after-run.test.ts); [`test/harness-restart-failure.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-restart-failure.test.ts); [`test/harness-submissions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-submissions.test.ts); [`test/harness-turn-limit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-turn-limit.test.ts); [`test/harness-tool-hooks.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-tool-hooks.test.ts).
- Native/evidence: [`durable/harness.go`](../../durable/harness.go); [`durable/hooks.go`](../../durable/hooks.go); [`durable/submission.go`](../../durable/submission.go); [`durable/task_runtime.go`](../../durable/task_runtime.go); [`docs/upstream-v1.5.0/durable-semantic-port.md`](../../docs/upstream-v1.5.0/durable-semantic-port.md).
- Fix: Reconcile old diagnoses against current code; port unchanged complete source scenarios, especially real read-hold/close, restart charging, hook ordering and mixed continue/input boundaries. Do not resurrect disproved diagnoses as runtime gaps.
- Acceptance: Every assertion in the named files mapped and executed or independently blocked; real crash/reopen, stale-attempt fencing, first cause, queue preservation and callback counts.
- Dependencies: D01, D03, D07, D16, D19.
- Blocker/decision: none identified; acceptance still required

### D21 — Telemetry, environments, cross-owner export and diagnostic parity

**unverified.** Selected task/submission/built-in trace receipts cover a subset; full event order, labels/phases, carrier metadata, clock/error callbacks, environment lifetimes and cross-owner exports are not fully reconciled.

- Source: [`test/harness-telemetry.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry.test.ts); [`test/harness-telemetry-options.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry-options.test.ts); [`test/harness-reports.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-reports.test.ts); [`test/harness-host-inputs.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-host-inputs.test.ts); [`test/harness-ownership.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-ownership.test.ts).
- Native/evidence: [`durable/telemetry.go`](../../durable/telemetry.go); [`durable/environment.go`](../../durable/environment.go); [`durable/report.go`](../../durable/report.go); [`docs/upstream-v1.5.0/builtin-task-traces.md`](../../docs/upstream-v1.5.0/builtin-task-traces.md).
- Fix: Inventory original events and environment/ownership capabilities; capture deterministic traces with synthetic carriers; port exact ordering, presence and error-classification assertions without exposing credentials.
- Acceptance: Complete named source corpora; re-entry, reporter failure, parent/link presence, owner retirement, reopening and no secret/raw-provider data leaks.
- Dependencies: D03, D04, D07, D19.
- Blocker/decision: none identified; acceptance still required

### D22 — Codemode engine/worker and complete restart semantics

**unverified.** QuickJS/wazero undefined/null and late-result/reopen slices pass; official-worker and complete source codemode corpus parity are unverified. Native undefined sentinel is an explicit Go mapping, not proof of every boundary. Environment-dependent tests have skips.

- Source: [`test/codemode.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode.test.ts); [`test/codemode-checkpoints.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-checkpoints.test.ts); [`test/codemode-imports.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-imports.test.ts); [`test/codemode-describe.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-describe.test.ts); [`test/codemode-models.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-models.test.ts).
- Native/evidence: [`durable/codemode_runtime.go`](../../durable/codemode_runtime.go); [`docs/upstream-v1.5.0/codemode-undefined-packet.md`](../../docs/upstream-v1.5.0/codemode-undefined-packet.md); [`internal/quickjs`](../../internal/quickjs).
- Fix: Port all guest-visible values, module/import/worker delivery, interruption, replay and checkpoint assertions; test official wire fixtures independently of native host mocks and classify any engine differences.
- Acceptance: Whole codemode corpus with exact guest assertions, undefined/null presence, deterministic replay, exports/imports, bounded cancellation and no late delivery; explain and close each skipped requirement.
- Dependencies: D07, D11, D20.
- Blocker/decision: none identified; acceptance still required

### D23 — Schema dependency and host RegExp/compiler semantics

**unverified.** Known invalid-pattern compile/reachability and Node-vs-QuickJS regex differences are recorded in schema-pattern-packet. Plain-path preflight is private/unwired. Other numeric multipleOf, recursion, cloning, diagnostics and draft contracts lack complete durable dependency acceptance.

- Source: [`packages/schema/src/compiler.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/schema/src/compiler.ts); [`packages/schema/src/shared.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/schema/src/shared.ts); [`packages/ai/src/utils/validation.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/ai/src/utils/validation.ts); [`test/harness-json.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-json.test.ts).
- Native/evidence: [`schema_validation_preflight.go`](../../schema_validation_preflight.go); [`schema_validation.go`](../../schema_validation.go); [`internal/quickjs/regexp.go`](../../internal/quickjs/regexp.go); [`docs/upstream-v1.5.0/schema-pattern-packet.md`](../../docs/upstream-v1.5.0/schema-pattern-packet.md).
- Fix: Make separate source-backed schema packets: reachable compilation/ref/dynamic scope, host regex grammar, exact decimal arithmetic, cloning/cache and issue paths. Use pinned runtime fixtures and preserve fail-closed resource handling.
- Acceptance: Retained validation_invalid_pattern_test.go, pinned compiler/dynamic/annotation-order cases and regex-host probes; exact numeric/recursive/ownership tests; durable schema consumers.
- Dependencies: none recorded.
- Blocker/decision: Host JS engine versions can disagree. Choose and test the pinned supported runtime contract; do not silently accept QuickJS-only syntax.

### D24 — Provider/AI semantics inherited by durable execution

**unverified.** Inference fixes and prompt-cache audits are separate receipts. Durable correctness still depends on exact message/tool metadata, retry/stream/error/cancellation and schema behaviour. Full AI corpus is outside the completed durable slices.

- Source: [`packages/ai/src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/ai/src/types.ts); [`test/provider-session-cache-e2e.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/provider-session-cache-e2e.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv`](../../docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv); [`docs/upstream-v1.5.0/assessment-fixes.md`](../../docs/upstream-v1.5.0/assessment-fixes.md); [`durable/generation.go`](../../durable/generation.go).
- Fix: Link each durable provider boundary to its AI discrepancy ticket and mock-transport fixture. Review inherited behaviour without folding all AI work into a false durable pass.
- Acceptance: Pinned provider fixtures exercised by durable, terminal/tool/error/cancellation paths and cache request semantics; no real paid calls needed.
- Dependencies: D07, D23.
- Blocker/decision: none identified; acceptance still required

### D25 — Original alternative storage engines

**scope-exclusion.** User explicitly selected SQLite/modernc as the sole backend. Original Memory/JSONL/IndexedDB engines are absent by that product scope choice.

- Source: [`test/memory-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/memory-storage.test.ts); [`test/jsonl-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/jsonl-storage.test.ts); [`test/indexeddb-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/indexeddb-storage.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/sqlite-only.md`](../../docs/upstream-v1.5.0/sqlite-only.md).
- Fix: Keep excluded backend constructors/engine-specific tests visible. Still port shared Storage semantics to SQLite; reintroducing other engines requires a scope change, not automatic waiver of shared assertions.
- Acceptance: Document each excluded engine-only assertion; retain shared conformance, SQLite rollback/ownership/crash tests.
- Dependencies: none recorded.
- Blocker/decision: Authorised SQLite-only scope excludes these engines; full original package backend availability is therefore not a completion claim.

### D26 — Assertion-level crosswalk and release verification are incomplete

**verification-gap.** Historical crosswalk has 161 durable test/support/fixture paths; 147 are pending-review, seven prior-partial-needs-delta-review, six reviewed-open and one partial. These stale file dispositions are not a parity percentage. Current full native test pass cannot close source assertions. Complete-tree verification found four additional worker support/configuration paths missing from that historical inventory; the new obligations cover 165 paths in total.

- Source: [`src/index.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/index.ts); [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv`](../../docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv); [`docs/upstream-v1.5.0/upstream-review-f9a26365.json`](../../docs/upstream-v1.5.0/upstream-review-f9a26365.json).
- Fix: Track every pinned durable path and enumerate every test/assertion per subsystem; map exact source/native adapters, results, mutants, unknowns and dependencies. Include required ai/schema/codemode dependencies. Complete regression/fault/static/profile verification after implementation.
- Acceptance: No unmapped pinned durable paths; no open source assertion, skipped required contract or normalised fixture counted as passing. Current full-suite, fault, CPU/alloc_space/alloc_objects and public-consumer checks retained with exact revisions.
- Dependencies: D01, D02, D03, D04, D05, D06, D07, D08, D09, D10, D11, D12, D13, D14, D15, D16, D17, D18, D19, D20, D21, D22, D23, D24, D27, D28, D29.
- Blocker/decision: none identified; acceptance still required

### D27 — Read-only Session/Harness mode and concurrent external viewers

**confirmed.** Original readOnly mode opens running work without scheduling or writing, refuses mutations with ReadOnlySession and supports an externally driven viewer. Native Session/Harness options have no readOnly mode; SQLite uses exclusive native writer ownership and initialisation.

- Source: [`test/harness-read-only.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-read-only.test.ts); [`src/harness/harness.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/harness.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/harness.go`](../../durable/harness.go); [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go).
- Fix: Add original read-only admission and scheduler policy, error classification, refresh and close behaviour. Design a safe SQLite viewer path that coexists with the driver without violating single-writer ownership or running recovery/migration writes.
- Acceptance: Complete harness-read-only source corpus: no sequence advance on open/close, no wake or recovery writes, every forbidden mutation refused, existing running tasks preserved, viewer stays usable and sees externally driven data.
- Dependencies: D10, D11, D12, D20.
- Blocker/decision: Needs a reader/refresh architecture compatible with native inode ownership and snapshots. Removing ownership locks is unacceptable.

### D28 — Raw Storage initial state, explicit IDs and empty batches

**fixed.** Fresh original MemoryStorage capture has no conversation 1; an empty batch commits at seq1; explicit unminted conversation99 commits and raises next mint to100. Original MemoryStorage starts nextId=2 and original SQLite metadata seeds next_id='2', next_seq=1, so ID 1 is reserved but never materialised: `harness.root()` stages `tx.createRootConversation()` with ownerless ownership inside its own commit, and every original test or benchmark that addresses conversation 1 creates it with an explicit conversation write (`testing/storage-conformance.ts:32`, `testing/storage-benchmark.ts:93`). Upstream has no implicit-root read path and no persisted config version; its only versioning is the SQLite schema migration table (version 1 initial schema, version 2 sleeping status). Native previously preinstalled root1 in `initialState`, required it in `restoreCheckpoint`, and raw commits refused unreserved IDs.

**Resolution.** Raw Storage no longer fabricates the reserved root: `initialState()` holds no records and reserves allocator position 1 so the first mint is 2, and `restoreCheckpoint` no longer requires conversation 1. `Harness.Root`/`RootWithInit` materialize conversation 1 through `tx.CreateConversation` inside their own commit (the native equivalent of `tx.createRootConversation`); a second claim of the identity is refused. New native SQLite files write config version 2 and carry no root. Config version 1 files, written while the root was fabricated and which never persisted a root write, keep their implicit reserved root so their existing data retains the semantics its writer intended; an unknown config version is `ErrCorrupt` rather than being upgraded implicitly. Version 1 compatibility is a native-only shim because upstream persists no config version. Empty batches store each inner empty array with its own sequence and receipt, including grouped calls, reopen and checkpoint replay, and an empty outer call does not advance the sequence; native `writes:null` remains invalid. Explicit IDs are accepted for raw (non-Session) Storage commits: an unused ID in 1..MaxID commits and the allocator rises to the highest such ID atomically, while global type collisions, immutable recreation and out-of-domain IDs are refused; Session-owned writes still require reserved IDs. The native test corpus now declares its root dependency: fixtures that address conversation 1 create it before the store is owned (`seedRootConversation`, the `apply` helper, and the `sqliteStore`/`sqliteStoreFresh` split), and seq-relative replay fixtures run against a version 1 store so every captured sequence number and document point stays intact.

- Source: [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`src/storage/sqlite/migrations.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/sqlite/migrations.ts); [`src/session/transaction.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/transaction.ts); [`testing/storage-conformance.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/testing/storage-conformance.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/storage_checkpoint.go`](../../durable/storage_checkpoint.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go); [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_group.go`](../../durable/storage_group.go); [`durable/harness.go`](../../durable/harness.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json); [`durable/storage_boundaries_v150_test.go`](../../durable/storage_boundaries_v150_test.go); [`durable/storage_explicit_id_v150_test.go`](../../durable/storage_explicit_id_v150_test.go); [`durable/storage_root_version_v150_test.go`](../../durable/storage_root_version_v150_test.go); [`durable/harness_root_bootstrap_v150_test.go`](../../durable/harness_root_bootstrap_v150_test.go).
- Fix: Separate raw Storage initialisation from Session root bootstrap. Implement original raw empty-batch sequence semantics and atomic allocator advancement for explicit IDs, with global type collision checks. Preserve native transaction ID reservation and writer ownership rather than deleting them.
- Acceptance: Replay storage-boundaries.json through the public native SQLite boundary with no preparatory minting: fresh root absent, empty batch accepted at seq1, explicit99 accepted, next mint100; original outbox replay and reopen. Concurrent allocator/explicit-ID, collisions, backend rollback and uncertain outcomes must preserve uniqueness.
- Verification: TestV150StorageFreshRootAbsent; TestSQLiteRootConfigVersionCompatibility and TestSQLiteUnknownConfigVersionIsCorrupt; TestHarnessRootBootstrapsReservedConversation; TestV150StorageExplicitIDRaisesAllocator, TestV150StorageExplicitIDGroupAndCollisions, TestV150StorageExplicitIDRealRollback, TestV150StorageExplicitIDReplayRejectsForgedHighWater, TestV150SessionStillRequiresReservedIDs; TestV150StorageEmptyBatch, TestV150StorageEmptyBatchRollback, TestV150StorageEmptyBatchRealRollback, TestV150StorageEmptyBatchRefusalKeepsPrefix, TestInternalReceiptImageAcceptsEmptyPart; TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches and TestPiDurableV150PublicStorageCommitEnforcesIDOwnership. Full durable suite green in 360.203s (GOMAXPROCS=2, -p 1) with go vet, pinned staticcheck, the logging gate and the register checker passing.
- Dependencies: D10, D11, D12.
- Blocker/decision: Native config version 2 is a native-only persisted-format marker; upstream persists no config version. Version 1 files keep their implicit reserved root, so their fresh-state shape stays different from a version 2 store, and data written while the root was fabricated cannot be rewritten as if its owner had committed it. Session/Harness root bootstrap consumes one sequence on a fresh store, so sequence-sensitive fixtures acquire the root explicitly as the original tests do. Broader raw Storage duplicate/update policy remains under D12 review.

### D29 — Empty unanswered reason for continue submissions

**fixed.** Baseline rejected the captured empty unanswered continue reason and empty typed settlements. Continue storage validation and input/write/continue settlement now accept present empty strings. Missing/non-string private reasons, missing public-wire reasons and illegal answer/entry/detail combinations remain rejected.

- Resolution: Removed the stricter nonempty-string checks from validateContinueSubmission and SettleSubmission. Reconciled three legacy test expectations against the original fixture and retained positive empty-reason plus negative missing/invalid union controls.
- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json); [`durable/storage_boundaries_v150_test.go`](../../durable/storage_boundaries_v150_test.go).
- Original fix path: Preserve reason presence independently of its string contents through all submission subtypes and transitions. Remove only the stricter continue nonempty requirement after a retained original-derived failing control; do not allow missing reason or illegal answer/detail combinations.
- Acceptance: Original storedSubmission in storage-boundaries.json round-trips exactly for continue; input/write/continue unanswered empty, missing and nonempty reasons; dedup and reopen; invalid union controls and submission settlement remain green.
- Verification: TestV150SubmissionEmptyContinueReason: exact original storedSubmission payload before and after reopen. TestV150SubmissionEmptyReasonSettlement and TestV150ContinueReasonStillRequired; complete existing TestMainSubmissionContinue group.
- Dependencies: none recorded.
- Blocker/decision: No envelope or schema migration; old data remains readable. Broader submission/runtime corpus review remains D20.

## Three-deviation implementation checkpoint

D08, D28 and D29 are fixed within their named scope; D28 now includes the raw/fresh root split with explicit Harness root bootstrap and version-1 file compatibility. No whole-file disposition has been promoted. The Session writer prototype is excluded from the compiled verification candidate and the commit.

Test-first baseline results:

- Exact 12-batch replay lost submission 12's present empty requestId.
- Empty batch was refused as invalid count; the grouped backend-failure control never reached storage.
- Original continue empty reason was refused after the preceding conversation batch; typed input/write/continue empty settlements were also refused.

After the fixes, the broader focused selection (V150/public-wire, grouped storage, SQLite, continue lifecycle, request dedup and storage-request controls) passes normally and with count=3/shuffle=42 (**99.942s** for the repeated durable selection). The complete durable/tools suite passes (**21.942s**). Repository-wide vet and Staticcheck pass with CGO_ENABLED=0. Staticcheck initially found two uncalled message-conversion prototypes from the earlier wire checkpoint; those unused functions were removed without changing message producers/readers (D07 remains open).

The first integrated full run failed in **966.531s**: the new standard native submission marshaler emitted nested map keys in varying order, causing snapshot byte-comparison failures, and a receipt test still classified a newly valid empty write array as corrupt. A dedicated deterministic-marshal test reproduced the ordering bug before correction. Native standard marshaling now sorts map keys; the corrupt receipt uses writes:null, and a positive empty-part reconstruction test is retained. The affected snapshot/ownership/receipt controls plus V150 tests pass count=3/shuffle=42 (**81.483s**). The corrected complete durable regression passes in **725.786s**, with the uncommitted writer production/test prototype excluded through a compile overlay. Vet, Staticcheck and logging checks pass. These ordinary development tests are unprofiled; they provide no CGO race-detector or pre-release profiling coverage.

Compatibility details:

- Existing absent/nonempty private requestId JSON is unchanged. Presence is retained in the existing scalar field; no extra SQLite field, table/configuration version change or automatic migration is introduced. Previously lost presence cannot be reconstructed.
- Newly valid empty batches use the existing writes array with zero members. Private writes:null remains invalid. Older binaries do not implement the new empty-batch/empty-key semantics and may reject newly valid records.
- Native request identity/uniqueness guards remain in force. General raw Storage duplicate-request/update policy still needs D12's conformance review.
- Real SQL rollback, logical checkpoint/reopen and refusal-prefix controls cover the new empty-batch path. Missing/non-string reasons and illegal submission union fields remain rejected.

Reproduce the focused tests:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=4
make test PACKAGES=./durable TEST_FLAGS='-run TestV150|TestPiDurableV150|TestInternalGroup|TestSQLite|TestMainSubmissionContinue|TestM1bRequest|Test.*StorageRequest -count=3 -shuffle=42 -timeout=3m'
make test PACKAGES=./durable/tools TEST_FLAGS='-count=1 -timeout=3m'
make vet
make staticcheck
```

## Reproduced red controls at the baseline

At native baseline a89368c, a temporary Go compile overlay activated parked tests without altering their assertions. The replay overlay removes only `dropEmptyRequestID` so the comparison uses exact `canonicalJSON`. Results:

| Control | Observed mismatch |
| --- | --- |
| Original batch replay | Submission 12 loses present `requestId: ""`. |
| Session source170 | b/c callbacks do not run while a is held; storage calls are `[1,1,1]`, expected `[1,2]`. |
| View source160 | Unchanged `pi.live` and docs identity differ. |
| View source330 | Independent observers expose different docs maps. |
| View source525 | Independent windows expose different docs identities. |
| Original message producer | Live entries 9/10 retain native `kind:"message"` and `value`; reopened entries also fail. |

Retained red assertions live in [pending fixtures](../../durable/testdata/upstream-main/pending/). A fixture overlay must alias the original files into the compiled durable package, preserving the original hold/read/observer seam. The current native-core restriction (D10) limits some wrapper-based oracles; an after-core hold cannot substitute for before-storage admission.

That baseline replay failure is fixed: the committed test now compares every original field exactly, and the normalisation helper is removed. The replay still pre-mints IDs (D28), so it does not establish raw allocator/bootstrap parity. Built-in task rejection remains an incompatibility (D09).

## Additional original boundary capture

[storage-boundaries.json](../../durable/testdata/pi-durable-v150/storage-boundaries.json) captures actual original MemoryStorage output for D28/D29. The corresponding SQLite source boundary also raises allocation above explicit IDs. Native tests now execute the captured empty-reason payload unchanged and test the original empty-batch result/receipt, with reopen and real rollback controls. The root/explicit-ID parts remain established by code inspection and the stricter native ownership tests; their complete acceptance tests are still outstanding. Reproduce the original capture with:

```sh
bun run scripts/capture-durable-storage-boundaries.mjs /path/to/reference durable/testdata/pi-durable-v150/storage-boundaries.json
```

The reference directory must contain the official 1.5.0 package and its dependencies under node_modules. The capture changes no original payload fields.

## Existing passing slices

These controls remain useful but do not close the groups above:

- SQLite grouped call rollback/uncertain-outcome and admission settlement; original partial durable prefixes remain intact.
- Five source256 Session history-read surfaces; typed hook history, cancellation/close signalling and first-failure propagation.
- Persisted wait-cycle graph admission; grouped pending graph validation still needs D19.
- Public TaskRecord/TaskState, cursor, Conversation/Entry, StorageWrite and SubmissionRecord codec slices; public grouped Storage.Commit for representable native records.
- Empty unanswered reason and present empty errorCode conversion, subject to complete submission lifecycle coverage.
- Source202 watch threshold/default resource-policy slice; old persisted limits are retained.

Before this implementation, a complete native durable run passed in **720.342s**, and vet/format checks passed before baseline publication. One earlier full run failed `TestMainBoundaryEligibilityLocallyDecidedFailFast/held/failFast/sqlite` with `scheduler context canceled`; isolated repetitions and the later full run passed. The root cause is unconfirmed. The inventory-only checkpoint 4f0ae62 did not include a fresh full-suite run; the current implementation results are recorded above. Direct writer-prototype tests pass independently; they do not exercise Session integration or establish publication parity.

## Review and validation

The historical durable file dispositions are 147 `pending-review`, seven `prior-partial-needs-delta-review`, six `reviewed-open` and one `partial`. These labels predate some recent slices. All 165 paths receive an explicit review obligation; a support/fixture path also needs relevance review. Four worker support/configuration paths were missing from the historical inventory and now have explicit obligations labelled `not-in-historical-inventory`. No row is marked complete by this inventory. Gap IDs are routing aids, not an assertion-level conformance mapping. D26 includes every otherwise-unclassified assertion and source export.

Run the register checks without an original checkout:

```sh
bun run scripts/check-durable-parity-register.mjs
```

With the pinned source snapshot available, also verify all 165 hashes, every source link and equality with the complete original durable test and test-workers trees. A Git checkout also verifies HEAD; the retained source-only snapshot has no .git, so its commit identity is not independently reverified by this check:

```sh
bun run scripts/check-durable-parity-register.mjs --upstream /path/to/pi-f9a26365568127461c47a07a37dcb42c140a3ac5
```

Inventory-only checkpoint 4f0ae62: both validation modes pass (29 entries, 165 obligations, 101 test files). Negative controls reject a missing historical obligation, an unknown dependency and source hash drift. The original boundary capture reproduces byte-for-byte, both scripts pass Node syntax checks, and git diff --check passes. No production Go file changed in that inventory-only checkpoint.

These checks validate inventory integrity. They do not execute the source assertions. Continue to run test-first bounded packets, full native regression, fault/static checks and allocation/CPU profiles at implementation checkpoints. Keep failed required assertions visible until they pass without adapters that change meaning.

## Explicit-ID checkpoint

Raw Storage commits now accept unused explicit IDs and raise the allocator atomically, matching storage-boundaries.json (conversation99, next mint100). Session transactions still require reserved IDs. Commit frames carry the post-commit high-water value in the existing column; replay recomputes and verifies it, so forged values fail as corruption. Earlier binaries refuse such files rather than misread them. The native-only assertion that ID99 must be refused was replaced by an immutable-recreation refusal. Full durable suite passes in **339.226s** (GOMAXPROCS=2, -p1, writer prototype excluded); durable/tools 17.070s; vet, Staticcheck, logging pass. An earlier attempt running the full suite concurrently with Staticcheck exhausted workspace memory; checks now run sequentially. Root preinstallation remains open.

## Checked against source and cleared (not deviations)

Each of these looked like a deviation and was tested against the pinned tree before any code was
written. None of them is a gap; recording them so the next pass does not spend effort "fixing" a
thing upstream does not have.

| Candidate | Why it looked wrong | What the pinned source says |
| --- | --- | --- |
| `storeCore.line` is a contended one-slot token | The Session line had exactly this defect and was a real gap (see `8a4d06a`) | `GuardedStorage` (session.ts:1922) does not serialise backend calls at all — it tracks `#underway` so close can wait, and latches failures. Upstream needs no storage ordering because it has no storage contention. `storeCore.line` is Go standing in for single-threaded execution, so making it FIFO would align with nothing |
| Native `Storage` has no `publishing` scope, and the writer host runs publications directly | The hook exists in the source contract | `publishing` is optional (`types.ts:1353`) and `scopeOf` (cached-storage.ts:337) returns `undefined` when a backend omits it; no pinned backend implements it. Running the publication directly is upstream's documented no-scope path, not an omission |
| Failure-driven shutdown routed through the writer's `fail` hook | Felt like a native addition to the failure path | `fail()` closes fire-and-forget itself: `this.close(BACKGROUND_CONTEXT).catch(() => {})` (session.ts:450). Routing shutdown from the failure hook is the upstream behaviour |
| Passing a `nil` adoption candidate | Looked like a latent lossy projection in HEAD | Every production adoption site already passes the exact image captured at that receipt (`session.go:372`, `session_deferred.go:384/447`, `close_requeue.go:47`). The loss existed only inside the reverted flip, so no fix is owed in HEAD |

The Session line order (D02/D05 evidence) is now aligned at `8a4d06a`: jobs are granted in
admission order, matching `#tail`. D01/D02/D03-remainder/D04 remain open — publication still runs
on the line, so admission is still not handed off.
