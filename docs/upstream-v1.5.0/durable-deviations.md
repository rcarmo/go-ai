# Durable deviations from pi-durable v1.5.0

Full alignment is incomplete and the current production-admission worktree is red. This register reviews all 29 groups against retained receipts and selected current code; it does not complete the assertion audit or supply fresh runtime test results.

Register states: confirmed=11; unverified=13; scope-exclusion=1; verification-gap=1; fixed=3.

Target: `f9a26365568127461c47a07a37dcb42c140a3ac5`. Last pushed full-tree checkpoint: `89c031ebcdc7374b28240ca09511bfcd7e9f8c22`. The nativeBaseline/date fields in the JSON describe the original historical inventory. Its review/currentCheckpoint fields describe this review and the uncommitted migration separately.

- [Fresh findings and execution plan](alignment-roadmap.md): current failures, corrected diagnoses, all workstreams and closure criteria.
- [Machine register](durable-parity-gaps.json): current evidence and historicalEvidence, exact acceptance requirements and dependencies.
- [Complete durable obligations](durable-review-obligations.tsv): 165 pinned paths, including 101 test files; coverage of paths does not establish assertion parity.
- [Task/table assertion mapping](task-table-assertions.tsv), [main crosswalk](upstream-tests-f9a26365.tsv) and [schema queue](dependency-tests-f9a26365.tsv): retain historical dispositions; none is promoted by this review.

## Classification and scope

Confirmed means a direct implementation/API contradiction or reproduced runtime gap. Unverified means remaining acceptance work without a current independently established defect for that group. D01/D03/D05/D07/D14/D16 are now unverified because their named foundations are installed; D20/D21/D23 are confirmed from private-state consumers, event projection and compiler differences. Dependencies can still contain confirmed gaps. These are mixed-progress groups, not a percentage or independent fix count.

D08/D28/D29 retain their bounded historical fixes; they still require integration regression on the final candidate. D25 excludes native alternative engines only. Shared Storage, wrappers, viewers and dependency contracts remain required. Existing data/assets are protected; native API compatibility is not a constraint.

Follow the fresh roadmap: establish the complete red baseline, finish and verify canonical production migration, then close Storage/API/SQLite/viewer, publication, readonly/lifecycle and dependency contracts with assertion mapping throughout. No new implementation starts merely because this plan was saved.

## Gap index

| ID | State | Area |
| --- | --- | --- |
| [D01](#d01--independent-session-admission-and-storage-batching) | unverified | Independent Session admission and storage batching |
| [D02](#d02--keyed-unpublished-read-dependencies) | confirmed | Keyed unpublished read dependencies |
| [D03](#d03--failure-boundaries-and-arbitrary-rejectionrefusal-values) | unverified | Failure boundaries and arbitrary rejection/refusal values |
| [D04](#d04--publication-turns-continuations-and-storage-publication-scope) | confirmed | Publication turns, continuations and storage publication scope |
| [D05](#d05--256-started-but-undelivered-job-bound-and-ordered-outcomes) | unverified | 256 started-but-undelivered job bound and ordered outcomes |
| [D06](#d06--deferred-changes-unload-reload-deadlines-and-close-during-writer-flight) | unverified | Deferred changes, unload, reload, deadlines and close during writer flight |
| [D07](#d07--original-harness-message-kinds-and-model-payloads) | unverified | Original harness message kinds and model payloads |
| [D08](#d08--present-but-empty-submission-requestid-and-deduplication) | fixed | Present-but-empty submission requestId and deduplication |
| [D09](#d09--canonical-builtin-storage-and-runtime-boundary) | confirmed | Canonical builtin storage and runtime boundary |
| [D10](#d10--opensession-rejects-arbitrary-conforming-storage-implementations) | confirmed | OpenSession rejects arbitrary conforming Storage implementations |
| [D11](#d11--persisted-sqlite-schema-and-record-envelopes-differ) | confirmed | Persisted SQLite schema and record envelopes differ |
| [D12](#d12--storage-readquery-result-shapes-and-remaining-public-record-dtos) | confirmed | Storage read/query result shapes and remaining public record DTOs |
| [D13](#d13--session-commit-callback-resultoptions-and-remaining-exported-api-surface) | confirmed | Session commit callback result/options and remaining exported API surface |
| [D14](#d14--shared-readonly-conversation-view-and-observer-values) | unverified | Shared readonly conversation-view and observer values |
| [D15](#d15--viewentry-identity-lifetime-mount-collection-and-empty-array-identity) | unverified | View/entry identity lifetime, mount collection and empty-array identity |
| [D16](#d16--public-observecommits-host-dispatch-and-waiter-ordering) | unverified | Public observeCommits host dispatch and waiter ordering |
| [D17](#d17--synchronous-document-observer-readonly-identity-and-full-contract) | unverified | Synchronous document observer readonly identity and full contract |
| [D18](#d18--resource-policies-safe-number-domain-and-persisted-limits) | unverified | Resource policies, safe-number domain and persisted limits |
| [D19](#d19--grouped-scheduler-ownershipwait-graph-and-read-races) | unverified | Grouped scheduler ownership/wait graph and read races |
| [D20](#d20--lifecycle-restartrecovery-hook-and-submission-corpus-coverage) | confirmed | Lifecycle, restart/recovery, hook and submission corpus coverage |
| [D21](#d21--telemetry-environments-cross-owner-export-and-diagnostic-parity) | confirmed | Telemetry, environments, cross-owner export and diagnostic parity |
| [D22](#d22--codemode-engineworker-and-complete-restart-semantics) | unverified | Codemode engine/worker and complete restart semantics |
| [D23](#d23--schema-dependency-and-host-regexpcompiler-semantics) | confirmed | Schema dependency and host RegExp/compiler semantics |
| [D24](#d24--providerai-semantics-inherited-by-durable-execution) | unverified | Provider/AI semantics inherited by durable execution |
| [D25](#d25--original-alternative-storage-engines) | scope-exclusion | Original alternative storage engines |
| [D26](#d26--assertion-level-crosswalk-and-release-verification-are-incomplete) | verification-gap | Assertion-level crosswalk and release verification are incomplete |
| [D27](#d27--read-only-sessionharness-mode-and-concurrent-external-viewers) | confirmed | Read-only Session/Harness mode and concurrent external viewers |
| [D28](#d28--raw-storage-initial-state-explicit-ids-and-empty-batches) | fixed | Raw Storage initial state, explicit IDs and empty batches |
| [D29](#d29--empty-unanswered-reason-for-continue-submissions) | fixed | Empty unanswered reason for continue submissions |

## Evidence and acceptance

`confirmed` means either a reproduced failing oracle or a direct source/native API/representation contradiction. `unverified` means incomplete source coverage, not an asserted runtime failure. `fixed` closes only the named bounded deviation with the listed controls; it does not promote whole upstream files or establish full package parity. Line numbers below refer to the pinned source. Each evidence statement distinguishes a fresh result from historical measurements or static review.

### Writer installation status

The writer is installed. Independent document admission, [1,2] batching, receipt adoption, first-failure/outcome fencing and the256-job bound have production controls. Remaining work includes exact scheduler/non-document keys, full assertion review and D04's between-part caller-continuation bridge. Public readonly observers are also installed. [Current priorities and closure criteria](alignment-roadmap.md).

### D01 — Independent Session admission and storage batching

**unverified.** Independent admission/writer flight is installed, with historical Session/Harness batching, receipt-time adoption and atomic submission controls. The remaining full outcome/order audit is unverified; no new independent admission defect is established by this review. Exact key gaps and continuation contradictions are tracked separately in D02/D04. The current production migration is red, so older passes are not current-tree acceptance.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/storage_group.go`](../../durable/storage_group.go); [`durable/submission.go`](../../durable/submission.go); [`durable/submission_records_test.go`](../../durable/submission_records_test.go); [`notes/submission-admission.md`](../../notes/submission-admission.md); [`durable/session_group_commit_main_test.go`](../../durable/session_group_commit_main_test.go).
- Fix: Preserve the installed independent admission/writer flight; finish precise non-document/scheduler dependencies and complete source outcome/continuation acceptance without reverting to whole-flight serialization.
- Acceptance: Source170 unchanged: b/c admitted before release; no early publication; calls [1,2]; distinct increasing sequences; a/b/c outcome order.
- Dependencies: D02, D03, D04.
- Blocker/decision: none identified; acceptance still required

### D02 — Keyed unpublished read dependencies

**confirmed.** Address/visibility/incarnation document keys and dependency rebasing are installed. Private whole-graph and scheduler paths still use ALL_KEYS, so precise source dependency behaviour is not fully implemented. Complete effective-pending-state, held-document, unkeyable scan and ownership validation must precede narrowing these barriers.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/session_read_line.go`](../../durable/session_read_line.go); [`durable/transaction.go`](../../durable/transaction.go).
- Fix: Register exact write keys, including newly created identities, document addresses, ancestry and selected held changes. Touch newest-key gates; unkeyable scans drain the admitted prefix. Revalidate candidate graph against effective pending state.
- Acceptance: Source201-380: leaked entry, duplicate document creation, new conversation, all line reads, unkeyable scans, held-document reads. Verify disjoint jobs progress and dependent jobs wait.
- Dependencies: none recorded.
- Blocker/decision: Footprint must cover whole-image finalisation and ownership reads before global baseline checks can be narrowed.

### D03 — Failure boundaries and arbitrary rejection/refusal values

**unverified.** Arbitrary failure/refusal presence, backend error identity, first-failure boundary and ordered own outcomes have passing historical controls. The named value-representation fix is installed; the complete parameterised entry-surface audit and integration with D04 remain unverified. No new failure-value defect was reproduced in this review.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/session_failure.go`](../../durable/session_failure.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/storage_group.go`](../../durable/storage_group.go); [`durable/storage.go`](../../durable/storage.go); [`durable/session_group_commit_outcomes_test.go`](../../durable/session_group_commit_outcomes_test.go); [`durable/storage_public_test.go`](../../durable/storage_public_test.go).
- Fix: Complete the parameterised source failure and entry-surface audit against the installed first-failure boundary, own-outcome set and ordered delivery. Preserve original values and backend error identity; integrate the remaining D04 caller-continuation semantics.
- Acceptance: All source414-562 parameterised cases, including undefined/null/false/0/empty string; own vs SessionFailed identity; real rollback and uncertain COMMIT; earlier durable prefix survives reopen.
- Dependencies: D01.
- Blocker/decision: The independent writer and failure/outcome sets are installed. Complete assertion-level acceptance and the D04 continuation bridge remain open.

### D04 — Publication turns, continuations and storage publication scope

**confirmed.** Storage publication scope completion, exact-prefix adoption and post-scope public dispatch are installed. Direct review still finds hop: func() {} in durable/session_writer_host.go. The between-part caller continuation that can fail the Session before a later publication has no implemented equivalent or complete source assertion receipt. Sleeping or yielding a goroutine cannot establish that contract.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/writer.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/writer.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session_publication.go`](../../durable/session_publication.go); [`durable/session_receipt.go`](../../durable/session_receipt.go); [`durable/session_sync_observers.go`](../../durable/session_sync_observers.go); [`durable/session_writer_host.go`](../../durable/session_writer_host.go); [`durable/types.go`](../../durable/types.go); [`durable/session_group_commit_admission_test.go`](../../durable/session_group_commit_admission_test.go).
- Fix: Design one serial publication dispatcher with awaitable scope completion, explicit continuation/outcome ordering and failure rechecks immediately inside the deferred scope. Retain detached exact-prefix receipts and contain scope throws according to the source.
- Acceptance: Source698-815 unchanged ordering assertions; defer a scope, throw without executing it, fail between parts and while scope is pending. No later part publishes.
- Dependencies: D01, D03.
- Blocker/decision: Go has no JS microtask/macrotask queue. A scheduler/bridge must reproduce observable order; runtime.Gosched or sleeps do not establish parity.

### D05 — 256 started-but-undelivered job bound and ordered outcomes

**unverified.** The 256 started-but-undelivered job bound is installed and historical source-shaped capacity/outcome controls pass. Remaining cache fast paths, cancellation at capacity and every delivery surface need assertion-level verification. This is an acceptance gap, not evidence that the bound is missing.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go).
- Fix: Preserve the installed256-slot start-to-delivery bound and finish cache fast-path, cancellation-at-capacity and all delivery-surface assertions.
- Acceptance: Source390 and920: only255 additional jobs start behind first; all300 finish; outstanding/unpublished/reads return to zero; cancellation and errors release every slot.
- Dependencies: D01, D04.
- Blocker/decision: none identified; acceptance still required

### D06 — Deferred changes, unload, reload, deadlines and close during writer flight

**unverified.** Deferred persistence, timers and close have selected historical passing controls. Concurrent unload/reload/deadline/held-revision/close interactions have not been accepted as the complete source group contract. Public CommitOptions are installed; reimplementing that API is not the remaining task.

- Source: [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts); [`test/session-deferred-commits.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-deferred-commits.test.ts).
- Native/evidence: [`durable/session_deferred.go`](../../durable/session_deferred.go); [`durable/session_timers.go`](../../durable/session_timers.go); [`durable/session.go`](../../durable/session.go).
- Fix: Admit memory-only parts in the same writer order; pin held revisions and store each selected revision once. Put unload/reload/deadline/close flushes behind the flight and join their outcomes.
- Acceptance: All named source cases; close drains admitted work and held changes; no duplicate persistence, lost revision, early memory publication or gate/worker leak.
- Dependencies: D01, D02, D04, D05.
- Blocker/decision: none identified; acceptance still required

### D07 — Original harness message kinds and model payloads

**unverified.** Canonical pi.user/pi.assistant/pi.tool-result producers, string/block content, zero timestamps and optional-field controls are installed. The uncommitted production migration also passes focused transcript image hydration and UTF-16 argument-diagnostic controls. Complete message/receipt unions, abort/cutoff paths and provider/context boundaries remain unverified; normal message kinds are not a missing implementation.

- Source: [`src/entries.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/entries.ts); [`src/harness/generation.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts); [`test/harness-input-entries.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-input-entries.test.ts).
- Native/evidence: [`durable/message_entry.go`](../../durable/message_entry.go); [`durable/entry_record_wire.go`](../../durable/entry_record_wire.go); [`durable/registry.go`](../../durable/registry.go); [`durable/entry_definition.go`](../../durable/entry_definition.go); [`durable/entry_message_kinds_v150_test.go`](../../durable/entry_message_kinds_v150_test.go); [`durable/message_receipt_wire.go`](../../durable/message_receipt_wire.go); [`durable/entry_record_wire_v150_test.go`](../../durable/entry_record_wire_v150_test.go); [`durable/submission_input_source_main_test.go`](../../durable/submission_input_source_main_test.go); [`durable/compaction_text_test.go`](../../durable/compaction_text_test.go); [`durable/input_metadata.go`](../../durable/input_metadata.go).
- Fix: Preserve the installed canonical producers and finish exact tool-result, diagnostic/detail, UTF-16 and empty/absent-field assertion coverage. Reconcile every remaining receipt witness with source public fields; do not repeat the obsolete tag-only producer switch.
- Acceptance: Retain the installed entry_message_kinds_v150_test.go controls; original tool-result data.diagnostics, model details/error fields, exact required-zero/empty presence; full generation, tool, compaction, abort and reopen regressions.
- Dependencies: none recorded.
- Blocker/decision: Normal producers and the live/reopen shape are installed; complete message/receipt union and field-presence acceptance is still open.

### D08 — Present-but-empty submission requestId and deduplication

**fixed.** The bounded present-empty requestId deviation has historical exact-record, dedup/index, lifecycle and SQLite reopen controls. Empty and absent request IDs remain distinct. Raw replacement and duplicate-key policy are separately audited under D12. No fresh full-tree acceptance is claimed for the red migration.

- Resolution: RequestIDPresent on SubmissionRecord, Input and private Submission retains optional-string presence. The existing requestId JSON field encodes the bit: present empty remains an empty string; absent and nonempty legacy encodings are unchanged. Nonempty RequestID still implies presence. Empty-key lookup and dedup distinguish absent keys. The later submission-read port removes incompatible raw replacement/uniqueness guards: raw records may change request keys or share them, while Harness admission still deduplicates.
- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_public_test.go`](../../durable/storage_public_test.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/storage.go`](../../durable/storage.go); [`durable/submission_native_wire.go`](../../durable/submission_native_wire.go); [`durable/submission_records_test.go`](../../durable/submission_records_test.go).
- Original fix path: Add explicit presence to the versioned private representation, propagate it through creation, copy, update, snapshot and indexing, and define legacy-file decoding. Preserve empty key as an actual per-conversation dedup key.
- Acceptance: All12 captured batches with exact JSON equality and no dropEmptyRequestID; empty vs absent dedup/lookups before and after reopen; transitions, duplicates and caller ownership.
- Verification: TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches: unchanged captured records, seq1..12, exact comparison. TestV150SubmissionRequestPresenceLegacyWire, TestV150SubmissionRequestPresenceReopenAndCheckpoint, TestV150SubmissionRequestPresenceSessionAndHarness.
- Dependencies: none recorded.
- Blocker/decision: No new persisted field, schema change or automatic migration. Presence already lost by older writers cannot be reconstructed; old absent fields stay absent. Raw complete replacement and duplicate request keys now follow the tested pinned SQLite cases; full D12 conformance remains open.

### D09 — Canonical builtin storage and runtime boundary

**confirmed.** Pushed 89c031e installs executable Generation2/Tool3 definitions and source migrations; imported/created canonical model/tool/nested/replay packets pass. Only the uncommitted tree migrates normal Submit to Generation2/Inbox/live taskId. That tree fails full regression and retains private-state consumers and custom-task nested paths. All phase names being present does not establish controls, provider hooks, recovery, cleanup or complete record interoperability. See notes/canonical-builtin-execution.md and alignment-roadmap.md R1-R8.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/task_definition.go`](../../durable/task_definition.go); [`durable/task_record_wire.go`](../../durable/task_record_wire.go); [`durable/task_record.go`](../../durable/task_record.go); [`durable/storage_public_test.go`](../../durable/storage_public_test.go); [`durable/session_public_commit_observer_main_test.go`](../../durable/session_public_commit_observer_main_test.go); [`durable/builtin_tasks_test.go`](../../durable/builtin_tasks_test.go); [`durable/builtin_task_projection.go`](../../durable/builtin_task_projection.go); [`durable/storage_tasks_test.go`](../../durable/storage_tasks_test.go); [`durable/testdata/pi-durable-v150/builtin-storage-records.json`](../../durable/testdata/pi-durable-v150/builtin-storage-records.json); [`durable/task_scheduler.go`](../../durable/task_scheduler.go); [`durable/scan.go`](../../durable/scan.go); [`notes/builtin-task-storage.md`](../../notes/builtin-task-storage.md); [`durable/record_reads.go`](../../durable/record_reads.go); [`durable/transaction_tables_test.go`](../../durable/transaction_tables_test.go); [`durable/message_receipt_wire.go`](../../durable/message_receipt_wire.go); [`durable/inbox.go`](../../durable/inbox.go); [`durable/run_boundary.go`](../../durable/run_boundary.go); [`durable/submission_input_source_main_test.go`](../../durable/submission_input_source_main_test.go); [`durable/input_metadata.go`](../../durable/input_metadata.go); [`durable/entry_definition.go`](../../durable/entry_definition.go); [`durable/compaction_text.go`](../../durable/compaction_text.go); [`durable/compaction_text_test.go`](../../durable/compaction_text_test.go); [`durable/testdata/pi-durable-v150/compaction-prompts.json`](../../durable/testdata/pi-durable-v150/compaction-prompts.json); [`notes/canonical-compaction-kind.md`](../../notes/canonical-compaction-kind.md); [`durable/compaction_utf16.go`](../../durable/compaction_utf16.go); [`durable/compaction_utf16_main_test.go`](../../durable/compaction_utf16_main_test.go); [`durable/compaction.go`](../../durable/compaction.go); [`durable/hooks.go`](../../durable/hooks.go); [`durable/compaction_policy.go`](../../durable/compaction_policy.go); [`durable/compaction_generation.go`](../../durable/compaction_generation.go); [`durable/compaction_threshold_test.go`](../../durable/compaction_threshold_test.go); [`durable/settings.go`](../../durable/settings.go); [`durable/compaction_current_retry_test.go`](../../durable/compaction_current_retry_test.go); [`durable/settings_retry_test.go`](../../durable/settings_retry_test.go); [`durable/compaction_status.go`](../../durable/compaction_status.go); [`durable/compaction_status_test.go`](../../durable/compaction_status_test.go); [`durable/testdata/pi-durable-v150/compaction-request.json`](../../durable/testdata/pi-durable-v150/compaction-request.json); [`durable/testdata/pi-durable-v150/compaction-retry.json`](../../durable/testdata/pi-durable-v150/compaction-retry.json); [`scripts/capture-durable-compaction-request.mjs`](../../scripts/capture-durable-compaction-request.mjs); [`durable/compaction_test.go`](../../durable/compaction_test.go); [`durable/hooks_test.go`](../../durable/hooks_test.go); [`durable/events.go`](../../durable/events.go); [`durable/compaction_no_model_test.go`](../../durable/compaction_no_model_test.go); [`durable/harness_builtin_cutoffs_main_test.go`](../../durable/harness_builtin_cutoffs_main_test.go); [`durable/compaction_sleep_140_test.go`](../../durable/compaction_sleep_140_test.go); [`durable/registry.go`](../../durable/registry.go); [`durable/agent_patch_operations_main_test.go`](../../durable/agent_patch_operations_main_test.go); [`durable/task_agent_failure_test.go`](../../durable/task_agent_failure_test.go); [`types.go`](../../types.go); [`message_json.go`](../../message_json.go); [`durable/streaming_arguments_json.go`](../../durable/streaming_arguments_json.go); [`durable/generation.go`](../../durable/generation.go); [`durable/compaction_failure.go`](../../durable/compaction_failure.go); [`classifier_runtime.go`](../../classifier_runtime.go); [`classifier_json.go`](../../classifier_json.go); [`images/api.go`](../../images/api.go); [`images/message_json.go`](../../images/message_json.go); [`durable/models_paid.go`](../../durable/models_paid.go); [`durable/models_paid_test.go`](../../durable/models_paid_test.go); [`durable/codemode_models_test.go`](../../durable/codemode_models_test.go); [`tests/images_test.go`](../../tests/images_test.go); [`tests/classifier_contract_issue1_test.go`](../../tests/classifier_contract_issue1_test.go); [`notes/typed-model-results.md`](../../notes/typed-model-results.md); [`durable/codemode_models.go`](../../durable/codemode_models.go); [`durable/tool.go`](../../durable/tool.go); [`durable/tool_control.go`](../../durable/tool_control.go); [`durable/nested_tool.go`](../../durable/nested_tool.go); [`durable/extension.go`](../../durable/extension.go); [`durable/testdata/pi-durable-v150/model-tools.json`](../../durable/testdata/pi-durable-v150/model-tools.json); [`scripts/capture-durable-model-tools.mjs`](../../scripts/capture-durable-model-tools.mjs); [`durable/agent_patch.go`](../../durable/agent_patch.go); [`durable/extension_wrap_test.go`](../../durable/extension_wrap_test.go); [`durable/models_paid_validation.go`](../../durable/models_paid_validation.go); [`durable/models_classifier_context.go`](../../durable/models_classifier_context.go); [`classifier_types.go`](../../classifier_types.go); [`images/content_json.go`](../../images/content_json.go); [`durable/streaming_arguments_main_test.go`](../../durable/streaming_arguments_main_test.go); [`durable/canonical_generation.go`](../../durable/canonical_generation.go); [`durable/canonical_tool.go`](../../durable/canonical_tool.go); [`durable/canonical_nested.go`](../../durable/canonical_nested.go); [`durable/canonical_boundary.go`](../../durable/canonical_boundary.go); [`durable/canonical_cleanup.go`](../../durable/canonical_cleanup.go); [`notes/canonical-builtin-execution.md`](../../notes/canonical-builtin-execution.md).
- Fix: Finish the uncommitted canonical production transition and all remaining consumers, then remove obsolete private paths after source assertions pass. Do not infer private checkpoints from source input or count phase-name coverage as semantic acceptance.
- Acceptance: Retain exact raw builtin admission/projection, state scans and SQLite reopen/reclaim controls. Execute canonical builtin records and migrations through real Harness paths, then finish complete raw Storage conformance and canonical read/API assertions.
- Dependencies: D11, D12.
- Blocker/decision: No external blocker; canonical builtin execution/migration and complete Storage conformance remain incomplete.

### D10 — OpenSession rejects arbitrary conforming Storage implementations

**confirmed.** Direct review confirms OpenSession still requires private nativeStorage/nativeCore; the public Storage wrapper is not the sole authority for settlement/reservation. Source-shaped before-delegation hold/refusal/publishing wrappers cannot be credited through an internal sink substitution. Public-method additions alone do not repair this boundary.

- Source: [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`test/storage-runtime-boundary.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/storage-runtime-boundary.test.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/session_receipt.go`](../../durable/session_receipt.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/types.go`](../../durable/types.go).
- Fix: Use the original Storage capabilities at the Session boundary. Keep a verified native fast path only if it cannot bypass wrapper authority. Provide exact-prefix adoption for non-native storage without exposing private frames.
- Acceptance: Run original wrapper-shaped hold/refusal/publishing tests through OpenSession; wrapper reads/writes must intercept all relevant calls; native prefix-adoption and ownership controls remain green.
- Dependencies: D11, D12.
- Blocker/decision: Current ID reservation also takes the native core gate. A hold after that gate is not the original before-delegation seam.

### D11 — Persisted SQLite schema and record envelopes differ

**confirmed.** Native SQLite persists configuration/frames/checkpoints rather than the original tables and migration schema. Public JSON fixture import/reopen is verified for selected records, but original-produced database interchange is not. Native API compatibility is not required; preservation of existing data and explicit format/version/crash behaviour still is.

- Source: [`src/storage/sqlite/migrations.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/sqlite/migrations.ts); [`test/sqlite-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/sqlite-storage.test.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go); [`durable/types.go`](../../durable/types.go).
- Fix: Establish original SQLite schema and migration compatibility with original-produced databases. Use direct compatibility or a separately accepted lossless migration contract, with explicit format detection, rollback/crash/version tests and preservation of existing data; do not require legacy native API compatibility.
- Acceptance: Open/read original-produced DB fixture or verified lossless migration in both directions as required; native legacy reopen; crash at each migration boundary; unsupported version rejection; no partial conversion.
- Dependencies: none recorded.
- Blocker/decision: Database drop-in interchange requires an explicit migration design and fixtures, not a marshaler change.

### D12 — Storage read/query result shapes and remaining public record DTOs

**confirmed.** Primary task/conversation/entry/submission reads, canonical scans, committed Tx fences and deferred submission mutations have selected source-derived and original conformance controls. Storage still requires native Apply/Snapshot/Limits and the complete document/query/optional-field interface has not been accepted. Full raw graph/replacement/wrapper conformance remains open.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts); [`test/session-tables.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-tables.test.ts).
- Native/evidence: [`durable/types.go`](../../durable/types.go); [`durable/storage.go`](../../durable/storage.go); [`durable/scan.go`](../../durable/scan.go); [`durable/document_history.go`](../../durable/document_history.go); [`durable/record_reads.go`](../../durable/record_reads.go); [`durable/storage_tasks_test.go`](../../durable/storage_tasks_test.go); [`durable/transaction_tables_test.go`](../../durable/transaction_tables_test.go); [`durable/session_public_image_main_test.go`](../../durable/session_public_image_main_test.go); [`notes/oracles/storage-tasks.upstream.test.ts`](../../notes/oracles/storage-tasks.upstream.test.ts); [`docs/upstream-v1.5.0/task-table-assertions.tsv`](../../docs/upstream-v1.5.0/task-table-assertions.tsv); [`durable/storage_public_test.go`](../../durable/storage_public_test.go); [`durable/entry_definition.go`](../../durable/entry_definition.go); [`durable/harness_documents.go`](../../durable/harness_documents.go); [`durable/task_runtime.go`](../../durable/task_runtime.go); [`notes/primary-record-reads.md`](../../notes/primary-record-reads.md); [`durable/submission_records_test.go`](../../durable/submission_records_test.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission.go`](../../durable/submission.go); [`notes/submission-record-reads.md`](../../notes/submission-record-reads.md).
- Fix: Inventory every original Storage method and public union, with an explicit Go spelling map but identical data/absence/order semantics. Add missing single-record/head/document methods and public DTO boundaries; keep private envelopes private.
- Acceptance: Run pinned storage conformance through only the declared public interface; exact method result fixtures for tasks/submissions/documents/entries, scopes/points/retirement/forks, query fields and absence.
- Dependencies: none recorded.
- Blocker/decision: none identified; acceptance still required

### D13 — Session commit callback result/options and remaining exported API surface

**confirmed.** Callback values and public CommitOptions, complete canonical entry/conversation/fork constructors, current LatestHeadMarker and the narrowed 20-method Tx mapping are installed. Internal setTask is private and now performs complete canonical replacement. Earlier claims that AppendEntry only returns an error or Commit returns a sequence are obsolete. Complete source export/union/default/error mapping, publication-record identity and external-consumer semantics remain open. The uncommitted Settlement answer/task inference and retained Withdraw surface need an explicit source API audit rather than more native compatibility shims.

- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/index.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/index.ts); [`test/session-tables.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-tables.test.ts); [`test/session-definitions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-definitions.test.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/transaction.go`](../../durable/transaction.go); [`durable/session_deferred.go`](../../durable/session_deferred.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/transaction_tables_test.go`](../../durable/transaction_tables_test.go); [`notes/submission-transactions.md`](../../notes/submission-transactions.md); [`durable/submission.go`](../../durable/submission.go); [`durable/record_reads.go`](../../durable/record_reads.go); [`notes/submission-admission.md`](../../notes/submission-admission.md); [`durable/message_receipt_wire.go`](../../durable/message_receipt_wire.go); [`durable/entry_record_wire_v150_test.go`](../../durable/entry_record_wire_v150_test.go); [`durable/compaction.go`](../../durable/compaction.go); [`durable/hooks.go`](../../durable/hooks.go); [`durable/compaction_text_test.go`](../../durable/compaction_text_test.go); [`durable/session_public_commit_observer_main_test.go`](../../durable/session_public_commit_observer_main_test.go); [`durable/compaction_policy.go`](../../durable/compaction_policy.go); [`durable/compaction_generation.go`](../../durable/compaction_generation.go); [`durable/compaction_threshold_test.go`](../../durable/compaction_threshold_test.go); [`durable/settings.go`](../../durable/settings.go); [`durable/compaction_status.go`](../../durable/compaction_status.go); [`durable/compaction_status_test.go`](../../durable/compaction_status_test.go); [`durable/testdata/pi-durable-v150/compaction-request.json`](../../durable/testdata/pi-durable-v150/compaction-request.json); [`durable/testdata/pi-durable-v150/compaction-retry.json`](../../durable/testdata/pi-durable-v150/compaction-retry.json); [`scripts/capture-durable-compaction-request.mjs`](../../scripts/capture-durable-compaction-request.mjs); [`durable/compaction_test.go`](../../durable/compaction_test.go); [`durable/hooks_test.go`](../../durable/hooks_test.go); [`durable/events.go`](../../durable/events.go); [`durable/compaction_no_model_test.go`](../../durable/compaction_no_model_test.go); [`durable/harness_builtin_cutoffs_main_test.go`](../../durable/harness_builtin_cutoffs_main_test.go); [`durable/compaction_sleep_140_test.go`](../../durable/compaction_sleep_140_test.go); [`durable/compaction_current_retry_test.go`](../../durable/compaction_current_retry_test.go); [`durable/registry.go`](../../durable/registry.go); [`durable/agent_patch_operations_main_test.go`](../../durable/agent_patch_operations_main_test.go); [`durable/task_agent_failure_test.go`](../../durable/task_agent_failure_test.go); [`types.go`](../../types.go); [`message_json.go`](../../message_json.go); [`durable/streaming_arguments_json.go`](../../durable/streaming_arguments_json.go); [`durable/generation.go`](../../durable/generation.go); [`durable/compaction_failure.go`](../../durable/compaction_failure.go); [`classifier_runtime.go`](../../classifier_runtime.go); [`classifier_json.go`](../../classifier_json.go); [`images/api.go`](../../images/api.go); [`images/message_json.go`](../../images/message_json.go); [`durable/models_paid.go`](../../durable/models_paid.go); [`durable/models_paid_test.go`](../../durable/models_paid_test.go); [`durable/codemode_models_test.go`](../../durable/codemode_models_test.go); [`tests/images_test.go`](../../tests/images_test.go); [`tests/classifier_contract_issue1_test.go`](../../tests/classifier_contract_issue1_test.go); [`notes/typed-model-results.md`](../../notes/typed-model-results.md); [`durable/codemode_models.go`](../../durable/codemode_models.go); [`durable/tool.go`](../../durable/tool.go); [`durable/tool_control.go`](../../durable/tool_control.go); [`durable/nested_tool.go`](../../durable/nested_tool.go); [`durable/extension.go`](../../durable/extension.go); [`durable/testdata/pi-durable-v150/model-tools.json`](../../durable/testdata/pi-durable-v150/model-tools.json); [`scripts/capture-durable-model-tools.mjs`](../../scripts/capture-durable-model-tools.mjs); [`durable/agent_patch.go`](../../durable/agent_patch.go); [`durable/extension_wrap_test.go`](../../durable/extension_wrap_test.go); [`durable/models_paid_validation.go`](../../durable/models_paid_validation.go); [`durable/models_classifier_context.go`](../../durable/models_classifier_context.go); [`classifier_types.go`](../../classifier_types.go); [`images/content_json.go`](../../images/content_json.go); [`durable/streaming_arguments_main_test.go`](../../durable/streaming_arguments_main_test.go); [`durable/harness.go`](../../durable/harness.go); [`durable/harness_documents.go`](../../durable/harness_documents.go); [`durable/session_group_commit_main_test.go`](../../durable/session_group_commit_main_test.go); [`notes/commit-callback-values.md`](../../notes/commit-callback-values.md); [`durable/harness_tools_test.go`](../../durable/harness_tools_test.go); [`durable/task_recovery_test.go`](../../durable/task_recovery_test.go); [`durable/commit_options.go`](../../durable/commit_options.go); [`durable/task_runtime.go`](../../durable/task_runtime.go); [`durable/session_deferred_main_test.go`](../../durable/session_deferred_main_test.go); [`durable/harness_persistence_settings_main_test.go`](../../durable/harness_persistence_settings_main_test.go); [`durable/entry_draft.go`](../../durable/entry_draft.go); [`durable/entry_definition.go`](../../durable/entry_definition.go); [`notes/entry-constructors.md`](../../notes/entry-constructors.md); [`durable/conversation_draft.go`](../../durable/conversation_draft.go); [`durable/session_group_commit_admission_test.go`](../../durable/session_group_commit_admission_test.go); [`notes/conversation-constructors.md`](../../notes/conversation-constructors.md); [`durable/document_history.go`](../../durable/document_history.go); [`durable/document_history_test.go`](../../durable/document_history_test.go); [`durable/session_read_line.go`](../../durable/session_read_line.go); [`notes/public-tx-surface.md`](../../notes/public-tx-surface.md).
- Fix: Complete the source-export-to-Go semantic map, including record identity, defaults, errors, callbacks, tokens and unions. Retain already implemented canonical constructors/results/options; audit and replace incompatible extra APIs rather than expanding native convenience contracts.
- Acceptance: Source callback-value tests, empty/error/async-equivalent results, deferred options on same entry point, tokens, typed lookup and original defaults; compile an external consumer using only the accepted mapped API.
- Dependencies: none recorded.
- Blocker/decision: Lossless Go representation of source identities/overloads must be demonstrated with external consumers. Existing native API compatibility is not required.

### D14 — Shared readonly conversation-view and observer values

**unverified.** Comparable sealed readonly View/Watch/ObserveConversation values, shared mount identity, canonical entry envelopes, cold/warm builtin hydration and synchronous delivery are installed with historical identity controls. Full wrapper fallback, observer ownership, lifecycle and error-value acceptance remain unverified. Mutable-copy absence is not a current blanket diagnosis.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view.go`](../../durable/view.go); [`durable/view_publication.go`](../../durable/view_publication.go); [`durable/immutable_value.go`](../../durable/immutable_value.go); [`durable/view_readonly.go`](../../durable/view_readonly.go); [`durable/view_identity_oracle_main_test.go`](../../durable/view_identity_oracle_main_test.go); [`durable/view_observer.go`](../../durable/view_observer.go); [`durable/harness_document_definitions.go`](../../durable/harness_document_definitions.go).
- Fix: Complete the wrapper fallback, acquisition/lifecycle stress and full assertion-level view audit. Reconcile remaining cross-surface observer ownership/dispatch and error-value contracts without exposing mutable aliases.
- Acceptance: Promoted source160/330/525 identity assertions retain object-identity semantics through comparable readonly handles; source160/274/330/525 content/window/identity, readonly authority, peer isolation, remount and complete synchronous-observer acceptance.
- Dependencies: none recorded.
- Blocker/decision: none identified; complete assertion-level acceptance still required

### D15 — View/entry identity lifetime, mount collection and empty-array identity

**unverified.** Historical raw-handle controls cover empty arrays, retained entry nodes, append/trim, late attachment, last detach/remount and actual readonly-node GC with a positive retention control. DocumentWatch unregistration-before-Closed also has repeated controls. Complete cross-surface lifecycle/retention stress remains unverified.

- Source: [`test/harness-view.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-view.test.ts); [`src/harness/transcript.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/transcript.ts); [`src/harness/view.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/view.ts).
- Native/evidence: [`durable/view_gc_oracle_main_test.go`](../../durable/view_gc_oracle_main_test.go); [`durable/view_identity_oracle_main_test.go`](../../durable/view_identity_oracle_main_test.go); [`durable/view_window.go`](../../durable/view_window.go); [`durable/observation.go`](../../durable/observation.go); [`durable/watch_owner_main_test.go`](../../durable/watch_owner_main_test.go).
- Fix: Complete synchronous/wrapper lifetime stress and the full assertion crosswalk. Preserve individual node identity and bounded cleanup without retaining prior histories.
- Acceptance: Source274 remount, source570 individual record lifetime, shared slices/windows and last-detach cleanup; bounded retention and repeated attach/detach stress.
- Dependencies: D14.
- Blocker/decision: none identified; complete lifecycle and assertion-level acceptance still required

### D16 — Public observeCommits host dispatch and waiter ordering

**unverified.** Public ObserveCommits, immutable publications, idempotent detach and post-scope host dispatch are installed; selected callback failures, task/submission waiter ordering and builtin publication/progression controls pass. Full async/reentry/weak-lifetime/multipart assertion coverage is incomplete. The confirmed between-part ordering implementation gap is D04; this group no longer describes an absent public observer API.

- Source: [`test/session-observers.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-observers.test.ts); [`test/harness-submissions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-submissions.test.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session_sync_observers.go`](../../durable/session_sync_observers.go); [`durable/session_local_observers.go`](../../durable/session_local_observers.go); [`durable/observation.go`](../../durable/observation.go); [`durable/testdata/upstream-main/pending/harness_commit_observer_wait_test.go`](../../durable/testdata/upstream-main/pending/harness_commit_observer_wait_test.go); [`durable/commit_observer.go`](../../durable/commit_observer.go); [`durable/commit_publication.go`](../../durable/commit_publication.go); [`durable/session_public_commit_observer_main_test.go`](../../durable/session_public_commit_observer_main_test.go); [`durable/session_writer_host.go`](../../durable/session_writer_host.go); [`durable/observer_throw.go`](../../durable/observer_throw.go); [`durable/compaction.go`](../../durable/compaction.go); [`durable/compaction_generation.go`](../../durable/compaction_generation.go); [`durable/task_record.go`](../../durable/task_record.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/builtin_tasks_test.go`](../../durable/builtin_tasks_test.go); [`durable/builtin_task_projection.go`](../../durable/builtin_task_projection.go).
- Fix: Finish D04 grouped caller-continuation/macrotask bridge and the complete stored-observer assertion crosswalk; cover async rejection/arbitrary panic values, reentry/report faults, weak/lifetime and remaining builtin task public projections. Preserve the installed canonical public API and dispatch phase, not the private synchronous guard as an alternate contract.
- Acceptance: Complete pinned session-observers corpus, public source284 wait test, attach/detach between publication and dispatch, re-entry, listener faults, close/failure and multipart publications.
- Dependencies: D04, D14.
- Blocker/decision: none identified; acceptance still required

### D17 — Synchronous document observer readonly identity and full contract

**unverified.** Readonly document observers, snapshots and asynchronous watches share adopted revisions; zero-retention holds, cold builtin hydration and error-valued acquisition cancellation have historical controls. Complete version/reload/eviction/weak-lifetime, task-bound warm reads, cancellation phases and observer assertions remain unverified. Old claims that all cold builtin hydration is missing are superseded.

- Source: [`test/session-observers.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-observers.test.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/session_document_observers.go`](../../durable/session_document_observers.go); [`durable/session_public_document_observer_main_test.go`](../../durable/session_public_document_observer_main_test.go); [`durable/observation.go`](../../durable/observation.go); [`durable/document_observer.go`](../../durable/document_observer.go); [`durable/document_readonly.go`](../../durable/document_readonly.go); [`durable/document_definition.go`](../../durable/document_definition.go); [`durable/session_document_cache_test.go`](../../durable/session_document_cache_test.go); [`durable/transaction.go`](../../durable/transaction.go); [`durable/delta_primitive_batch.go`](../../durable/delta_primitive_batch.go); [`durable/harness_document_definitions.go`](../../durable/harness_document_definitions.go).
- Fix: Preserve installed cold-builtin hydration, readonly snapshots/watches and initial mount borrowing. Finish version/reload/eviction/pin/weak lifetime, task-bound warm reads, exact cancellation causes and remaining observer assertions; complete D16 binding/dispatch acceptance.
- Acceptance: Source77/87/105/132/162/241/254/308/469/496/520, detach/end guards, queued acquisition/cancellation, retirement/recreation, scheduler publication and immutable values.
- Dependencies: D14, D16.
- Blocker/decision: none identified; acceptance still required

### D18 — Resource policies, safe-number domain and persisted limits

**unverified.** Large-watch defaults and folding controls pass historically; complete source/native bounds, Number/ID/cursor domains and persisted profiles have not been compared. Exact-number controls and bounded native policies do not by themselves establish JavaScript-domain equality. Intermediate-growth and old-data safety assertions must remain intact.

- Source: [`test/session-watches.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-watches.test.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts); [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts).
- Native/evidence: [`durable/types.go`](../../durable/types.go); [`durable/json.go`](../../durable/json.go); [`durable/watch_pending.go`](../../durable/watch_pending.go); [`durable/delta.go`](../../durable/delta.go).
- Fix: Catalogue each original vs native bound, integer domain and default. Preserve bounded execution but implement valid original-domain behaviour; distinguish configurable operational limits from semantic rejection. Version any stored-policy change explicitly.
- Acceptance: Source202 and8000/10000 controls stay green; exact safe integers and cursor/document points; original large-input boundary fixtures; intermediate-growth, cancellation and ownership controls; strict old-profile reopen.
- Dependencies: D11.
- Blocker/decision: none identified; acceptance still required

### D19 — Grouped scheduler ownership/wait graph and read races

**unverified.** Opposite-wait cycle admission and selected ownership/held-finalisation controls are implemented. Complete grouped pending-edge, indexes/sweeps/reconcile marks, record-cache/retention and submission continuation assertions remain unverified. The uncommitted canonical scheduler cleanup changes need direct fault/orphan/held-child source controls in addition to generic scheduler tests.

- Source: [`test/harness-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-group-commit.test.ts); [`test/harness-scheduler-indexes.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-scheduler-indexes.test.ts); [`test/harness-scheduler-retention.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-scheduler-retention.test.ts).
- Native/evidence: [`durable/task_wait_cycle.go`](../../durable/task_wait_cycle.go); [`durable/task_scheduler.go`](../../durable/task_scheduler.go); [`durable/harness_group_commit_main_test.go`](../../durable/harness_group_commit_main_test.go).
- Fix: Repeat graph/ownership validation against the effective candidate state at admission. Port production read/index seams and measure cache/retention bounds; preserve foreign-owner and terminal/background distinctions.
- Acceptance: Whole harness-group-commit corpus plus opposite-wait oracle, longer cycles, acyclic chains, pending creation/end ordering, idle indexes, bounded cache and close signalling.
- Dependencies: D01, D02, D03.
- Blocker/decision: none identified; acceptance still required

### D20 — Lifecycle, restart/recovery, hook and submission corpus coverage

**confirmed.** The uncommitted producer migration leaves lifecycle consumers reading private checkpoints: SubmissionHandle.Withdraw still searches generationCheckpoint instead of canonical queued records. Full regression also exposes deferred/abort fixtures with obsolete representations, failed-run trace/owner errors and compaction/history expectation conflicts. These have mixed causes and must be classified against source before fixes. Older queued-progress/close error ordering and repeated-call-ID timeout caveats remain unresolved.

- Source: [`test/harness-after-run.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-after-run.test.ts); [`test/harness-restart-failure.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-restart-failure.test.ts); [`test/harness-submissions.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-submissions.test.ts); [`test/harness-turn-limit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-turn-limit.test.ts); [`test/harness-tool-hooks.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-tool-hooks.test.ts).
- Native/evidence: [`durable/harness.go`](../../durable/harness.go); [`durable/hooks.go`](../../durable/hooks.go); [`durable/submission.go`](../../durable/submission.go); [`durable/task_runtime.go`](../../durable/task_runtime.go); [`docs/upstream-v1.5.0/durable-semantic-port.md`](../../docs/upstream-v1.5.0/durable-semantic-port.md); [`durable/harness_tools_test.go`](../../durable/harness_tools_test.go); [`durable/canonical_cleanup.go`](../../durable/canonical_cleanup.go); [`durable/canonical_generation.go`](../../durable/canonical_generation.go).
- Fix: First port private-state fixtures to canonical records and signal-based boundaries without weakening source assertions. Then correct actual queue/withdrawal/control/recovery consumers and verify deferred cancellation, committed partials, compaction failure, afterRun/turn-limit and close/error ordering.
- Acceptance: Every assertion in the named files mapped and executed or independently blocked; real crash/reopen, stale-attempt fencing, first cause, queue preservation and callback counts.
- Dependencies: D01, D03, D07, D16, D19.
- Blocker/decision: none identified; acceptance still required

### D21 — Telemetry, environments, cross-owner export and diagnostic parity

**confirmed.** Direct review finds events.go reading live.run.task while canonical admission writes taskId; the latest full run misses turn/tool lifecycle events. Selected canonical trace and UTF-16 diagnostic packets pass. Full event order, progress, environment effect/lifetime fences, report errors, clock callbacks and cross-owner exports remain unaccepted. The ordinary tool handler stays in call by source design; that phase is not a runtime defect.

- Source: [`test/harness-telemetry.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry.test.ts); [`test/harness-telemetry-options.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry-options.test.ts); [`test/harness-reports.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-reports.test.ts); [`test/harness-host-inputs.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-host-inputs.test.ts); [`test/harness-ownership.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-ownership.test.ts).
- Native/evidence: [`durable/telemetry.go`](../../durable/telemetry.go); [`durable/environment.go`](../../durable/environment.go); [`durable/report.go`](../../durable/report.go); [`docs/upstream-v1.5.0/builtin-task-traces.md`](../../docs/upstream-v1.5.0/builtin-task-traces.md); [`durable/events.go`](../../durable/events.go); [`durable/builtin_trace_main_test.go`](../../durable/builtin_trace_main_test.go).
- Fix: Migrate event/progress projection to canonical source records and live slots; verify exact order, parent/link/phase presence, environment effect/lifetime fences and report/cross-owner behaviour through production paths.
- Acceptance: Complete named source corpora; re-entry, reporter failure, parent/link presence, owner retirement, reopening and no secret/raw-provider data leaks.
- Dependencies: D03, D04, D07, D19.
- Blocker/decision: none identified; acceptance still required

### D22 — Codemode engine/worker and complete restart semantics

**unverified.** QuickJS/wazero value/replay/model slices have historical receipts. The uncommitted canonical producer now passes the complete selected TestCodemode120 prefix, including actual VM/model/nested calls, prompt configuration, images and reopen. This does not cover the complete pinned guest/import/worker/interruption corpus. Official-worker, engine differences and environment-dependent skips remain unverified.

- Source: [`test/codemode.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode.test.ts); [`test/codemode-checkpoints.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-checkpoints.test.ts); [`test/codemode-imports.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-imports.test.ts); [`test/codemode-describe.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-describe.test.ts); [`test/codemode-models.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/codemode-models.test.ts).
- Native/evidence: [`durable/codemode_runtime.go`](../../durable/codemode_runtime.go); [`docs/upstream-v1.5.0/codemode-undefined-packet.md`](../../docs/upstream-v1.5.0/codemode-undefined-packet.md); [`internal/quickjs`](../../internal/quickjs); [`classifier_runtime.go`](../../classifier_runtime.go); [`classifier_json.go`](../../classifier_json.go); [`images/api.go`](../../images/api.go); [`images/message_json.go`](../../images/message_json.go); [`durable/models_paid.go`](../../durable/models_paid.go); [`durable/models_paid_test.go`](../../durable/models_paid_test.go); [`durable/codemode_models_test.go`](../../durable/codemode_models_test.go); [`tests/images_test.go`](../../tests/images_test.go); [`tests/classifier_contract_issue1_test.go`](../../tests/classifier_contract_issue1_test.go); [`notes/typed-model-results.md`](../../notes/typed-model-results.md); [`durable/codemode_models.go`](../../durable/codemode_models.go); [`durable/tool.go`](../../durable/tool.go); [`durable/tool_control.go`](../../durable/tool_control.go); [`durable/nested_tool.go`](../../durable/nested_tool.go); [`durable/extension.go`](../../durable/extension.go); [`durable/testdata/pi-durable-v150/model-tools.json`](../../durable/testdata/pi-durable-v150/model-tools.json); [`scripts/capture-durable-model-tools.mjs`](../../scripts/capture-durable-model-tools.mjs); [`durable/agent_patch.go`](../../durable/agent_patch.go); [`durable/hooks.go`](../../durable/hooks.go); [`durable/extension_wrap_test.go`](../../durable/extension_wrap_test.go); [`durable/models_paid_validation.go`](../../durable/models_paid_validation.go); [`durable/models_classifier_context.go`](../../durable/models_classifier_context.go); [`classifier_types.go`](../../classifier_types.go); [`images/content_json.go`](../../images/content_json.go); [`durable/streaming_arguments_json.go`](../../durable/streaming_arguments_json.go); [`durable/streaming_arguments_main_test.go`](../../durable/streaming_arguments_main_test.go); [`durable/codemode_catalog.go`](../../durable/codemode_catalog.go); [`durable/codemode_parallel_cleanup.go`](../../durable/codemode_parallel_cleanup.go).
- Fix: Port all guest-visible values, module/import/worker delivery, interruption, replay and checkpoint assertions; test official wire fixtures independently of native host mocks and classify any engine differences.
- Acceptance: Whole codemode corpus with exact guest assertions, undefined/null presence, deterministic replay, exports/imports, bounded cancellation and no late delivery; explain and close each skipped requirement.
- Dependencies: D07, D11, D20.
- Blocker/decision: none identified; acceptance still required

### D23 — Schema dependency and host RegExp/compiler semantics

**confirmed.** Bounded pattern matching, optional/coercion validation and the recorded 166 common-domain fixtures have corrections. The reachable-schema preflight remains unwired; retained compile/reachability and dynamic-anchor probes identify differences from the pinned compiler. Host Node/QuickJS RegExp syntax, dynamic scope, annotations/draft/ref/union behaviour and full diagnostics/cache/cloning/numeric contracts need completion. Do not restore the obsolete diagnosis that all pattern or multipleOf validation is absent.

- Source: [`packages/schema/src/compiler.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/schema/src/compiler.ts); [`packages/schema/src/shared.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/schema/src/shared.ts); [`packages/ai/src/utils/validation.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/ai/src/utils/validation.ts); [`test/harness-json.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-json.test.ts).
- Native/evidence: [`schema_validation_preflight.go`](../../schema_validation_preflight.go); [`schema_validation.go`](../../schema_validation.go); [`internal/quickjs/regexp.go`](../../internal/quickjs/regexp.go); [`docs/upstream-v1.5.0/schema-pattern-packet.md`](../../docs/upstream-v1.5.0/schema-pattern-packet.md).
- Fix: Complete source-backed reachable compilation and dynamic lookup, then wire the accepted compiler into public validation. Reconcile host RegExp grammar, annotations/draft/ref/union rules, diagnostics, cache/cloning and numeric semantics without weakening resource-failure handling.
- Acceptance: Retained validation_invalid_pattern_test.go, pinned compiler/dynamic/annotation-order cases and regex-host probes; exact numeric/recursive/ownership tests; durable schema consumers.
- Dependencies: none recorded.
- Blocker/decision: Host JS engine versions can disagree. Choose and test the pinned supported runtime contract; do not silently accept QuickJS-only syntax.

### D24 — Provider/AI semantics inherited by durable execution

**unverified.** Exact release identity, 1712 catalogue records and 25 exported schemas have historical full-record receipts; selected validation/OAuth/retry and typed-model corrections also pass. Complete AI provider/HTTP/SSE/stream/error/tool/retry/cancellation/session-cache assertions and their use by canonical durable remain unverified. No fresh broader-package acceptance run was made during this review.

- Source: [`packages/ai/src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/ai/src/types.ts); [`test/provider-session-cache-e2e.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/provider-session-cache-e2e.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv`](../../docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv); [`docs/upstream-v1.5.0/assessment-fixes.md`](../../docs/upstream-v1.5.0/assessment-fixes.md); [`durable/generation.go`](../../durable/generation.go).
- Fix: Link each durable provider boundary to its AI discrepancy ticket and mock-transport fixture. Review inherited behaviour without folding all AI work into a false durable pass.
- Acceptance: Pinned provider/transport and durable integration assertions, terminal/tool/error/cancellation and cache request semantics. Mock transports establish only their mapped contracts; original credential-only assertions remain unverified until executed or explicitly excluded.
- Dependencies: D07, D23.
- Blocker/decision: none identified; acceptance still required

### D25 — Original alternative storage engines

**scope-exclusion.** The authorised native backend scope is SQLite through pure-Go modernc only. Memory/JSONL/IndexedDB availability and engine-only assertions are excluded. Shared Storage semantics, arbitrary wrappers, read-only viewers and worker/API contracts are not waived by that decision.

- Source: [`test/memory-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/memory-storage.test.ts); [`test/jsonl-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/jsonl-storage.test.ts); [`test/indexeddb-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/indexeddb-storage.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/sqlite-only.md`](../../docs/upstream-v1.5.0/sqlite-only.md).
- Fix: Keep excluded backend constructors/engine-specific tests visible. Still port shared Storage semantics to SQLite; reintroducing other engines requires a scope change, not automatic waiver of shared assertions.
- Acceptance: Document each excluded engine-only assertion; retain shared conformance, SQLite rollback/ownership/crash tests.
- Dependencies: none recorded.
- Blocker/decision: Authorised SQLite-only scope excludes these engines; full original package backend availability is therefore not a completion claim.

### D26 — Assertion-level crosswalk and release verification are incomplete

**verification-gap.** The register covers 29 groups and all 165 pinned durable test/support/fixture paths, including 101 test files; this is inventory coverage, not assertion acceptance. Historical main/schema crosswalk dispositions are incomplete. Last pushed full-tree verification belongs to 89c031e. Current migration full commands fail/panic, while focused/static checks pass. Selected original tests/probes do not replace native mapping. Full oracle image equality still exhausts the bounded worker heap; credential/platform skips and worker-specific coverage are unaccepted. Exact reference assets and Chord helper have been restored, so missing assets are no longer current blockers.

- Source: [`src/index.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/index.ts); [`test/session-group-commit.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/session-group-commit.test.ts).
- Native/evidence: [`docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv`](../../docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv); [`docs/upstream-v1.5.0/upstream-review-f9a26365.json`](../../docs/upstream-v1.5.0/upstream-review-f9a26365.json); [`scripts/check-durable-parity-register.mjs`](../../scripts/check-durable-parity-register.mjs); [`docs/upstream-v1.5.0/alignment-roadmap.md`](../../docs/upstream-v1.5.0/alignment-roadmap.md).
- Fix: Track every pinned durable path and enumerate every test/assertion per subsystem; map exact source/native adapters, results, mutants, unknowns and dependencies. Include required ai/schema/codemode dependencies. Complete regression/fault/static/profile verification after implementation.
- Acceptance: No unmapped pinned durable paths; no open source assertion, skipped required contract or normalised fixture counted as passing. Current full-suite, fault, CPU/alloc_space/alloc_objects and public-consumer checks retained with exact revisions.
- Dependencies: D01, D02, D03, D04, D05, D06, D07, D08, D09, D10, D11, D12, D13, D14, D15, D16, D17, D18, D19, D20, D21, D22, D23, D24, D27, D28, D29.
- Blocker/decision: Reference image equality exceeds the bounded worker heap; required credential/platform skips and worker-specific coverage are unaccepted. The native worktree is red and the assertion-level audit is incomplete.

### D27 — Read-only Session/Harness mode and concurrent external viewers

**confirmed.** Direct inspection still finds no source readOnly mode on Session/Harness. SQLite owns an exclusive native writer path. The original viewer contract requires no scheduling, recovery/migration writes or sequence advance; forbidden mutation errors and externally driven refresh/close remain unimplemented and unverified. Writer locks must not be removed to simulate a viewer.

- Source: [`test/harness-read-only.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-read-only.test.ts); [`src/harness/harness.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/harness.ts); [`src/session/session.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/session.ts).
- Native/evidence: [`durable/session.go`](../../durable/session.go); [`durable/harness.go`](../../durable/harness.go); [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go).
- Fix: Add original read-only admission and scheduler policy, error classification, refresh and close behaviour. Design a safe SQLite viewer path that coexists with the driver without violating single-writer ownership or running recovery/migration writes.
- Acceptance: Complete harness-read-only source corpus: no sequence advance on open/close, no wake or recovery writes, every forbidden mutation refused, existing running tasks preserved, viewer stays usable and sees externally driven data.
- Dependencies: D10, D11, D12, D20.
- Blocker/decision: Needs a reader/refresh architecture compatible with native inode ownership and snapshots. Removing ownership locks is unacceptable.

### D28 — Raw Storage initial state, explicit IDs and empty batches

**fixed.** The bounded raw initial-state/ID/batch deviation is fixed in prior verified commits: fresh raw root absent, ID 1 reserved, empty batches advance sequence, explicit unused IDs raise the allocator atomically, and Harness creates the root on demand. Config v1/v2 compatibility is native-only and preserves existing data. Complete original SQLite format and broader raw replacement semantics remain D11/D12, not closed by these controls.

**Resolution.** Raw Storage no longer fabricates the reserved root: `initialState()` holds no records and reserves allocator position 1 so the first mint is 2, and `restoreCheckpoint` no longer requires conversation 1. `Harness.Root`/`RootWithInit` materialize conversation 1 through `tx.CreateConversation` inside their own commit (the native equivalent of `tx.createRootConversation`); a second claim of the identity is refused. New native SQLite files write config version 2 and carry no root. Config version 1 files, written while the root was fabricated and which never persisted a root write, keep their implicit reserved root so their existing data retains the semantics its writer intended; an unknown config version is `ErrCorrupt` rather than being upgraded implicitly. Version 1 compatibility is a native-only shim because upstream persists no config version. Empty batches store each inner empty array with its own sequence and receipt, including grouped calls, reopen and checkpoint replay, and an empty outer call does not advance the sequence; native `writes:null` remains invalid. Explicit IDs are accepted for raw (non-Session) Storage commits: an unused ID in 1..MaxID commits and the allocator rises to the highest such ID atomically, while global type collisions, immutable recreation and out-of-domain IDs are refused; Session-owned writes still require reserved IDs. The native test corpus now declares its root dependency: fixtures that address conversation 1 create it before the store is owned (`seedRootConversation`, the `apply` helper, and the `sqliteStore`/`sqliteStoreFresh` split), and seq-relative replay fixtures run against a version 1 store so every captured sequence number and document point stays intact.

- Source: [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`src/storage/sqlite/migrations.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/sqlite/migrations.ts); [`src/session/transaction.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/transaction.ts); [`testing/storage-conformance.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/testing/storage-conformance.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/storage_checkpoint.go`](../../durable/storage_checkpoint.go); [`durable/storage_ownership.go`](../../durable/storage_ownership.go); [`durable/storage_public.go`](../../durable/storage_public.go); [`durable/storage_sqlite.go`](../../durable/storage_sqlite.go); [`durable/storage_group.go`](../../durable/storage_group.go); [`durable/harness.go`](../../durable/harness.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json); [`durable/storage_public_test.go`](../../durable/storage_public_test.go); [`durable/harness_root_bootstrap_v150_test.go`](../../durable/harness_root_bootstrap_v150_test.go); [`durable/storage_conformance_test.go`](../../durable/storage_conformance_test.go).
- Fix: Separate raw Storage initialisation from Session root bootstrap. Implement original raw empty-batch sequence semantics and atomic allocator advancement for explicit IDs, with global type collision checks. Preserve native transaction ID reservation and writer ownership rather than deleting them.
- Acceptance: Replay storage-boundaries.json through the public native SQLite boundary with no preparatory minting: fresh root absent, empty batch accepted at seq1, explicit99 accepted, next mint100; original outbox replay and reopen. Concurrent allocator/explicit-ID, collisions, backend rollback and uncertain outcomes must preserve uniqueness.
- Verification: TestV150StorageFreshRootAbsent; TestSQLiteRootConfigVersionCompatibility and TestSQLiteUnknownConfigVersionIsCorrupt; TestHarnessRootBootstrapsReservedConversation; TestV150StorageExplicitIDRaisesAllocator, TestV150StorageExplicitIDGroupAndCollisions, TestV150StorageExplicitIDRealRollback, TestV150StorageExplicitIDReplayRejectsForgedHighWater, TestV150SessionStillRequiresReservedIDs; TestV150StorageEmptyBatch, TestV150StorageEmptyBatchRollback, TestV150StorageEmptyBatchRealRollback, TestV150StorageEmptyBatchRefusalKeepsPrefix, TestInternalReceiptImageAcceptsEmptyPart; TestPiDurableV150PublicStorageCommitReplaysCapturedOriginalBatches and TestPiDurableV150PublicStorageCommitEnforcesIDOwnership. Full durable suite green in 360.203s (GOMAXPROCS=2, -p 1) with go vet, pinned staticcheck, the logging gate and the register checker passing.
- Dependencies: D10, D11, D12.
- Blocker/decision: Native config version 2 is a native-only persisted-format marker: upstream persists no config version, only the SQLite schema migration table. Version 1 files keep their implicit reserved root, so their fresh-state shape stays different from a version 2 store, and data written while the root was fabricated cannot be rewritten as if its owner had committed it. Session/Harness root bootstrap consumes one sequence on a fresh store, so sequence-sensitive fixtures must acquire the root explicitly the way the original tests do. Broader raw Storage duplicate/update policy remains under D12 review.

### D29 — Empty unanswered reason for continue submissions

**fixed.** The bounded unanswered-reason deviation has historical exact-record and settlement/reopen controls: present empty reasons are accepted for input/write/continue while missing or invalid unions reject. The current canonical migration needs integrated regression; this bounded fix does not close submission lifecycle or full Storage acceptance.

- Resolution: Removed the stricter nonempty-string checks from validateContinueSubmission and SettleSubmission. Reconciled three legacy test expectations against the original fixture and retained positive empty-reason plus negative missing/invalid union controls.
- Source: [`src/types.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts); [`src/storage/memory.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/storage/memory.ts); [`test/store-storage.test.ts`](https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/store-storage.test.ts).
- Native/evidence: [`durable/storage.go`](../../durable/storage.go); [`durable/submission_transaction.go`](../../durable/submission_transaction.go); [`durable/submission_status.go`](../../durable/submission_status.go); [`durable/testdata/pi-durable-v150/storage-boundaries.json`](../../durable/testdata/pi-durable-v150/storage-boundaries.json); [`durable/storage_public_test.go`](../../durable/storage_public_test.go).
- Original fix path: Preserve reason presence independently of its string contents through all submission subtypes and transitions. Remove only the stricter continue nonempty requirement after a retained original-derived failing control; do not allow missing reason or illegal answer/detail combinations.
- Acceptance: Original storedSubmission in storage-boundaries.json round-trips exactly for continue; input/write/continue unanswered empty, missing and nonempty reasons; dedup and reopen; invalid union controls and submission settlement remain green.
- Verification: TestV150SubmissionEmptyContinueReason: exact original storedSubmission payload before and after reopen. TestV150SubmissionEmptyReasonSettlement and TestV150ContinueReasonStillRequired; complete existing TestMainSubmissionContinue group.
- Dependencies: none recorded.
- Blocker/decision: No envelope or schema migration; old data remains readable. Broader submission/runtime corpus review remains D20.

## Historical three-deviation implementation checkpoint

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

## Historical red controls at the baseline

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

These checks validate inventory integrity. They do not execute the source assertions. Continue test-first bounded packets and full native/fault/static verification. CPU/allocation profiling and measured tuning are required at pre-release acceptance, not every development checkpoint. Keep failed required assertions visible until they pass without adapters that change meaning.

## Explicit-ID checkpoint

Raw Storage commits now accept unused explicit IDs and raise the allocator atomically, matching storage-boundaries.json (conversation99, next mint100). Session transactions still require reserved IDs. Commit frames carry the post-commit high-water value in the existing column; replay recomputes and verifies it, so forged values fail as corruption. Earlier binaries refuse such files rather than misread them. The native-only assertion that ID99 must be refused was replaced by an immutable-recreation refusal. Full durable suite passes in **339.226s** (GOMAXPROCS=2, -p1, writer prototype excluded); durable/tools 17.070s; vet, Staticcheck, logging pass. An earlier attempt running the full suite concurrently with Staticcheck exhausted workspace memory; checks now run sequentially. Root preinstallation remains open.

## Historical source checks (bounded non-deviations)

Each of these looked like a deviation and was tested against the pinned tree before any code was
written. None of them is a gap; recording them so the next pass does not spend effort "fixing" a
thing upstream does not have.

| Candidate | Why it looked wrong | What the pinned source says |
| --- | --- | --- |
| `storeCore.line` is a contended one-slot token | The Session line had exactly this defect and was a real gap (see `8a4d06a`) | `GuardedStorage` (session.ts:1922) does not serialise backend calls at all — it tracks `#underway` so close can wait, and latches failures. Upstream needs no storage ordering because it has no storage contention. `storeCore.line` is Go standing in for single-threaded execution, so making it FIFO would align with nothing |
| Native `Storage` has no `publishing` scope, and the writer host runs publications directly | The hook exists in the source contract | `publishing` is optional (`types.ts:1353`) and `scopeOf` (cached-storage.ts:337) returns `undefined` when a backend omits it; no pinned backend implements it. Running the publication directly is upstream's documented no-scope path, not an omission |
| Failure-driven shutdown routed through the writer's `fail` hook | Felt like a native addition to the failure path | `fail()` closes fire-and-forget itself: `this.close(BACKGROUND_CONTEXT).catch(() => {})` (session.ts:450). Routing shutdown from the failure hook is the upstream behaviour |
| Passing a `nil` adoption candidate | Looked like a latent lossy projection in HEAD | Every production adoption site already passes the exact image captured at that receipt (`session.go:372`, `session_deferred.go:384/447`, `close_requeue.go:47`). The loss existed only inside the reverted flip, so no fix is owed in HEAD |

At historical8a4d06a, Session admission became ordered but publication had not yet handed off. The current installed writer separates admission from publication; remaining dependency/assertion and D04 caller-continuation work is recorded above.
