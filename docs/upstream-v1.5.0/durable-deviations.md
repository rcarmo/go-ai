# Durable deviations from pi-durable v1.5.0

The native durable package is not yet semantically equivalent to pi-durable v1.5.0. This register records **17 confirmed incompatibility groups**, **10 unverified areas**, **one verification gap** and **one approved backend exclusion**. Every entry has evidence, a fix path and acceptance criteria. These groups are not a count of individual failing assertions.

Target: `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native baseline: `a89368c99be4f270baf1108a143b82fb08f849e9`. The uncommitted `session_writer.go` and direct writer tests are a prototype, excluded from implementation and parity receipts here. The inventory includes currently known deviations; the assertion-level audit is incomplete and may find more.

- [Machine-readable register](durable-parity-gaps.json): IDs, evidence, fixes, acceptance, dependencies and blockers.
- [Complete durable review obligations](durable-review-obligations.tsv): all **165 pinned test/support/fixture paths**, including **101 test files**, with original SHA-256, historical disposition, outstanding review and gap IDs.
- [Semantic-port receipts](durable-semantic-port.md): completed bounded slices and retained measurements.
- [Historical crosswalk](upstream-tests-f9a26365.tsv) and [dependency queue](dependency-tests-f9a26365.tsv): broader ai/schema scope. Historical dispositions have not been promoted.

## What can be fixed

Each confirmed gap has an implementation route. No external blocker has been identified. Four representation decisions must be settled before promising complete equivalence:

1. Public task/message/error values must be lossless, including arbitrary refusal values and empty/absent fields (D03, D07–D09, D12).
2. Existing native SQLite data must remain readable. Record evolution and original-database interoperability need explicit format detection and migration rules (D08, D11).
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
| [D08](#d08--present-but-empty-submission-requestid-and-deduplication) | confirmed | Present-but-empty submission requestId and deduplication |
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
| [D28](#d28--raw-storage-initial-state-explicit-ids-and-empty-batches) | confirmed | Raw Storage initial state, explicit IDs and empty batches |
| [D29](#d29--empty-unanswered-reason-remains-rejected-for-continue-submissions) | confirmed | Empty unanswered reason remains rejected for continue submissions |

## Evidence and acceptance

`confirmed` means either a reproduced failing oracle or a direct source/native API/representation contradiction. `unverified` means incomplete source coverage, not an asserted runtime failure. Line numbers below refer to the pinned source. Each evidence statement distinguishes a fresh result from historical measurements or static review.

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

**confirmed.** Original accepts unknown error values, including null/undefined/falsy. Public CommitResult.Refused and SessionFailed.Cause only carry Go errors. Native private refusal has presence tracking, public boundary does not preserve the original value. Single-commit wrapping does not establish multipart own-error outcomes.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/session_failure.go`](../../durable/session_failure.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/storage_group.go`](../../durable/storage_group.go).
- Fix: Retain presence and original value independently of Go error diagnostics. Record first failing job and same-call own-error set. Publish received prefixes before fencing, then settle all gates/outcomes; later calls inherit the first cause.
- Acceptance: All source414-562 parameterised cases, including undefined/null/false/0/empty string; own vs SessionFailed identity; real rollback and uncertain COMMIT; earlier durable prefix survives reopen.
- Dependencies: D01.
- Blocker/decision: Needs a lossless public error-value representation and compatibility review; converting everything to strings is unacceptable.

### D04 — Publication turns, continuations and storage publication scope

**confirmed.** Native publication runs on the Session line synchronously; no general Storage publication-scope interface. Source 698/721/753/770/796/815 require deferred scopes and inter-part continuation turns. Draft writer host only accepts a synchronous scope and cannot yet implement those cases.

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

**confirmed.** Fresh strict replay fails submission12: original requestId empty string becomes absent. Native RequestID string plus omitempty and empty-key index skip cannot preserve presence. Existing normalised replay is partial evidence only.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_replay_v150_test.go`](../../durable/storage_replay_v150_test.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/storage.go`](../../durable/storage.go).
- Fix: Add explicit presence to the versioned private representation, propagate it through creation, copy, update, snapshot and indexing, and define legacy-file decoding. Preserve empty key as an actual per-conversation dedup key.
- Acceptance: All12 captured batches with exact JSON equality and no dropEmptyRequestID; empty vs absent dedup/lookups before and after reopen; transitions, duplicates and caller ownership.
- Dependencies: D11.
- Blocker/decision: Requires deliberate private-envelope evolution; no silent persisted-format rewrite.

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

**confirmed.** Fresh source 160/330/525 oracles fail unchanged pi.live/docs identity and shared docs across windows/observers. Public Value/callbacks deep-copy mutable maps. Immutable primitives exist but are not integrated.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view.go`](../../durable/view.go); [`durable/view_publication.go`](../../durable/view_publication.go); [`durable/immutable_value.go`](../../durable/immutable_value.go).
- Fix: Integrate revision-stable immutable shared values and mounted view ownership. Reconcile legacy tests that allow mutating detached maps; do not expose mutable aliases or provide an optional alternate view instead of the original contract.
- Acceptance: All three pending identity oracles unchanged; source160/274/330/525 content/window/identity, mutation prevention, peer-listener isolation and remount-new-identity controls.
- Dependencies: none recorded.
- Blocker/decision: Representation/API migration is required. Ordinary writable Go maps cannot enforce shared readonly ownership.

### D15 — View/entry identity lifetime, mount collection and empty-array identity

**unverified.** Existing Go GC controls establish backing-array lifetimes; they do not establish source 570 individual EntryRecord retention or JavaScript empty-array object identity. Internal last-observer mount reuse/rebuild identity remains unverified.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/transcript.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/transcript.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view_gc_oracle_main_test.go`](../../durable/view_gc_oracle_main_test.go); [`durable/view_identity_oracle_main_test.go`](../../durable/view_identity_oracle_main_test.go); [`durable/view_window.go`](../../durable/view_window.go).
- Fix: Tie immutable entry identity and mount lifetime to retained handles/revisions. Define a testable language representation for empty collections and weak ownership. Retain no full prior snapshots just to preserve identity.
- Acceptance: Source274 remount, source570 individual record lifetime, shared slices/windows and last-detach cleanup; bounded retention and repeated attach/detach stress.
- Dependencies: D14.
- Blocker/decision: WeakRef/GC timing differs between runtimes; observable ownership must be established without flaky forced-GC timing claims.

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

**confirmed.** Fresh original MemoryStorage capture has no conversation 1; an empty batch commits at seq1; explicit unminted conversation99 commits and raises next mint to100. Native initialState preinstalls root1, publicBatch refuses empty writes, and adoption refuses unreserved IDs. The replay fixture pre-mints IDs, masking that mismatch. Original SQLite DurableCommit also raises allocation above explicit IDs.

- Source: [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`src/storage/store/commit.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/store/commit.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/storage_public_v150_test.go`](../../durable/storage_public_v150_test.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json).
- Fix: Separate raw Storage initialisation from Session root bootstrap. Implement original raw empty-batch sequence semantics and atomic allocator advancement for explicit IDs, with global type collision checks. Preserve native transaction ID reservation and writer ownership rather than deleting them.
- Acceptance: Replay storage-boundaries.json through the public native SQLite boundary with no preparatory minting: fresh root absent, empty batch accepted at seq1, explicit99 accepted, next mint100; original outbox replay and reopen. Concurrent allocator/explicit-ID, collisions, backend rollback and uncertain outcomes must preserve uniqueness.
- Dependencies: D10, D11, D12.
- Blocker/decision: Raw initial state and explicit-ID allocator semantics affect existing persisted native files; require explicit legacy compatibility and Session root bootstrap rules.

### D29 — Empty unanswered reason remains rejected for continue submissions

**confirmed.** Fresh original capture accepts an unanswered continue submission with reason empty string. nativeSubmission now permits empty unanswered reasons, but validateContinueSubmission still rejects errorCode empty string. The earlier input-submission replay fix does not cover this subtype.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json).
- Fix: Preserve reason presence independently of its string contents through all submission subtypes and transitions. Remove only the stricter continue nonempty requirement after a retained original-derived failing control; do not allow missing reason or illegal answer/detail combinations.
- Acceptance: Original storedSubmission in storage-boundaries.json round-trips exactly for continue; input/write/continue unanswered empty, missing and nonempty reasons; dedup and reopen; invalid union controls and submission settlement remain green.
- Dependencies: D08, D12.
- Blocker/decision: No new representation blocker beyond presence tracking; current private validator conflicts with the original union.

## Reproduced red controls

At the native baseline, a temporary Go compile overlay activates parked tests without altering their assertions. The replay overlay removes only `dropEmptyRequestID` so the comparison uses exact `canonicalJSON`. Results:

| Control | Observed mismatch |
| --- | --- |
| Original batch replay | Submission 12 loses present `requestId: ""`. |
| Session source170 | b/c callbacks do not run while a is held; storage calls are `[1,1,1]`, expected `[1,2]`. |
| View source160 | Unchanged `pi.live` and docs identity differ. |
| View source330 | Independent observers expose different docs maps. |
| View source525 | Independent windows expose different docs identities. |
| Original message producer | Live entries 9/10 retain native `kind:"message"` and `value`; reopened entries also fail. |

Retained red assertions live in [pending fixtures](../../durable/testdata/upstream-main/pending/). A fixture overlay must alias the original files into the compiled durable package, preserving the original hold/read/observer seam. The current native-core restriction (D10) limits some wrapper-based oracles; an after-core hold cannot substitute for before-storage admission.

The committed replay test intentionally uses `dropEmptyRequestID`. Its 12 accepted batches and sequence/order checks are partial evidence; exact original-record equality fails. Built-in task rejection is also an incompatibility, even though its current safety test passes.

## Additional original boundary capture

[storage-boundaries.json](../../durable/testdata/pi-durable-v150/storage-boundaries.json) captures actual original MemoryStorage output for D28/D29. The corresponding SQLite source boundary also raises allocation above explicit IDs. Native contradictions are currently established by code inspection and existing stricter ownership tests; complete native acceptance tests for these new cases have not yet been added. Reproduce the original capture with:

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

The last complete native durable run passed in **720.342s**, and vet/format checks passed before baseline publication. One earlier full run failed `TestMainBoundaryEligibilityLocallyDecidedFailFast/held/failFast/sqlite` with `scheduler context canceled`; isolated repetitions and the later full run passed. The root cause is unconfirmed. This register-only change does not constitute a fresh full-suite result. Direct writer-prototype tests pass independently; they do not exercise Session integration or establish publication parity.

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

Register checkpoint: both validation modes pass (29 entries, 165 obligations, 101 test files). Negative controls reject a missing historical obligation, an unknown dependency and source hash drift. The original boundary capture reproduces byte-for-byte, both scripts pass Node syntax checks, and git diff --check passes. No production Go file changed in this checkpoint.

These checks validate inventory integrity. They do not execute the source assertions. Continue to run test-first bounded packets, full native regression, fault/static checks and allocation/CPU profiles at implementation checkpoints. Keep failed required assertions visible until they pass without adapters that change meaning.
