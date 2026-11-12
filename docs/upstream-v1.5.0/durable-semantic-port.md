# Durable semantic port — work in progress

The user explicitly requested a semantically equivalent durable port with the same guarantees. Target remains official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`; starting native revision `028fb76cace523a92fe70ccf60309071f24f2163`. This request authorises source-aligned implementation of the previously paused lifecycle/admission contracts. It does not authorise weakening storage durability, ownership, cancellation, immutable publication or resource bounds. No complete parity result is claimed.

The [durable deviation register](durable-deviations.md) records current incompatibilities, fix paths and acceptance criteria; its [review obligations](durable-review-obligations.tsv) include every pinned durable test/support/fixture path. Public wire slices and passing native regressions do not establish full parity.

## Source and execution

Exact ai/schema/durable source was fetched under the resolved project run root, `runs/durable-port/pi-f9a26365568127461c47a07a37dcb42c140a3ac5`. It is temporary input, not a committed dependency. Use `source scripts/project-env.sh`, Go 1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4` and Makefile targets. Raw diagnostic files under this run are disposable after their analysis. No other-agent contact, hosted dispatch or release publication has occurred.

## Tested slices so far

### Session-line history reads

Pinned `session-group-commit.test.ts:256` uses five Session-ordered surfaces. The old native translation incorrectly used direct raw Storage for its two line reads; a raw store does not inherit Session job ordering. Current baseline reproduced both direct-read early unknown-conversation errors. This was a test-translation error, not evidence that raw SQLite reads must acquire the Session line.

`session_group_commit_admission_test.go` translates all five surfaces faithfully: transaction latest-entry ID, transaction head marker, transaction entry scan, line entry scan and line head-marker read. The initial compiler baseline established missing APIs, not runtime failure. New `Session.ReadOnLine`, callback-scoped `ReadLine`, `Tx.LatestEntryID` and `Tx.LatestHeadMarker` provide the missing capabilities. Reads use the committed Session image, preserve fork ancestry, detach returned records, reject escaped/overlapping capabilities and honour caller cancellation. Five surfaces plus lifetime/cancellation controls pass count3/shuffle42 (0.938s). The earlier case256 translation now routes line reads through ReadOnLine, preserving its original no-early-outcome assertions.

### SQLite fault semantics

Pinned `store-storage-commit.test.ts:371` requires a backend failure to roll the whole SQLite call back; previously confirmed calls stand. Native SQLite uses appendFrames for grouped calls. The old sequential-backend-error and admission/close fixtures patched only appendFrame, so neither fault/hold reached the grouped path. Their baseline failures were reproduced.

The reconciled atomic-backend test seeds a confirmed earlier call, injects a confirmed rejection or unknown outcome at appendFrames, asserts zero receipts/current-call adoption, preserves the cause/classification and checks the earlier prefix after reopen. Group admission/cancellation/close holds and delegates the real appendFrames path. Group bounds now count actual persisted frame bytes rather than a never-invoked single-frame hook. Existing real-SQL statement rollback, uncertain COMMIT, crash/reopen and post-COMMIT receipt controls remain unchanged.

Focused Session reads + internal group/SQLite group controls pass (2.988s). This is a fixture/API slice, not grouped Session publisher parity.

### Persisted wait-cycle admission

Read exact `harness/scheduler.ts:2470–2581` and `harness-group-commit.test.ts:226`. Retained two-opposite-waits production oracle failed unchanged in 4.289s: neither task settled and no refusal was recorded.

`task_wait_cycle.go` now traverses the final candidate's explicit waits and all live ordinary owned work, across terminal ownership bridges but not background subtrees. Abort-marked tasks omit wait edges but retain owned-work edges; terminal targets have no outgoing edges. The traversal tracks visited tasks/subtrees and returns a concrete cycle path. `prepareTaskWrites` checks waiting candidates before storage, retaining existing reference/ownership/failFast validation and unchanged terminal-owned-work checks. It is separate from volatile invocation-bound wait detection.

The original oracle is promoted to `harness_group_commit_wait_cycle_v150_test.go`, with its assertions retained. Eleven source-derived graph controls cover longer cycles, acyclic chains/diamonds, owned work, background barriers, terminal bridges/targets, abort edges and direct owned targets. Four selected production/ownership controls plus graph controls pass count3/shuffle42 (2.770s). Future grouped admission must repeat validation against effective pending state; this slice does not establish that yet.

## Lifecycle/history and evidence corrections

`TaskRuntime.Entries` and the forwarded HookAPI/InvocationReader surface now bind committed fork-aware history to the invocation conversation. Both complete `harness-hook-reads.test.ts` scenarios are translated, including pre-answer history, typed single reads, paging and inclusive bounds. Additional controls cover lifetime, cancellation, foreign-conversation rejection and detached returned data.

The earlier after-run diagnosis was incorrect: the pending fixture omitted upstream's `waitForIdle` before checking the hook. Restoring that wait and the exact bounded raw-history kind sequence passes without changing generation behavior. The historical promoted fixture used a native role-to-kind mapping and did not establish wire compatibility. The primary-read port removes that mapping: the scenarios now compare stored kinds literally, and the related tests are consolidated in `durable/task_history_test.go`. See [primary-read receipts](../../notes/primary-record-reads.md) and [consolidation proof](../../notes/test-consolidation.md).

The close-in-drain fixture also had translation errors: it expected a durable abort handler at orderly close and confused a published reservation with an actual phase attempt. Correcting those preconditions and adding the original single-entry assertion passes against the original runtime. In-flight close signalling was a real runtime gap. An independent active-invocation registry now seals invocation authority, stops owned watches/timers and signals cancellation before waiting for a held Session/storage line; admitted storage still drains before requeue and close. Existing watch-stop-before-signal, actual-host-join and no-restart-charge controls pass. A no-signal fault is rejected by the source-shaped test.

The first-failure guarantee had another real gap: failure-driven shutdown could return ordinary ErrClosed from Harness entry points or wait wakeups. `closedError` now retains the Session's first cause at those boundaries. Seven later-call controls fail before the correction and pass afterwards; the progress-failure/close gate passes count10/shuffle42.

Recovery fixtures were reconciled with current source contracts: deferred progress is explicitly observed in actual storage before injecting a durability fault; passive writes are already done at placement; live progress is read from pi.live instead of a private execution checkpoint; an unconfirmed interrupted request retries fresh preparation and dispatch, while remote-request counts remain exact. No outcome, queue-preservation, ownership, receipt, usage or effect-count assertion was removed.

## Watch bounds and bounded replay

The source's 10,000 pending-operation threshold is now distinct from a bounded 16,384 replay ceiling, allowing an unchanged first single batch past the folding threshold. New-store defaults permit 16,384 members and 131,072 nodes, within the prior hard ceilings. Existing SQLite files retain their exact persisted policy; a reopen control rejects silent widening.

The actual source202 public fixture now passes under new-store defaults. The 8,000 control and a stricter 10,000 public control reject premature root replacement. Under byte pressure the queue first compacts duplicate old values to the latest value plus folded operations. Compaction retains all publication readiness gates; Stop releases a worker waiting for readiness. Byte/node/member/depth/process bounds still apply. These bounds passing selected source fixtures does not establish that the overall resource policy is drop-in compatible.

The first public source202 run timed out at90s in repeated full-root replay validation. A preflighted primitive-set batch path now accounts for every intermediate member/node/encoded-byte bound exactly, retaining complete generic fallback before mutation for unsupported shapes. Differential controls preserve intermediate-growth failures, precision, ownership and paths. A 1,000-set benchmark moved from154–162ms/about6.2MB to10–11ms/about1.28MB; the public10,001-key fixture moved from the timeout to2.9s. A read-only MAI audit found no concrete mismatch in that narrow optimization. Focused CPU/alloc_space/alloc_objects profiles were inspected and automatically disposed; repeated whole-state validation/ownership copying remains allocation-heavy (about991MB cumulative across the selected large-fixture suite), so no full performance/release acceptance is claimed.

## Strict drop-in checkpoint and next work

The user explicitly requires a drop-in replacement for original pi-durable, not an alternate. The separate-semantics continuation API proposal is withdrawn and unaccepted. Existing blocking/copy-based behavior cannot be declared equivalent by relabeling it, and consumers must not be required to work around changed contracts.

The final native repository suite passes **2,632 top-level functions**, with seven explicitly skipped portable-codemode environment tests. The **unmodified original** pinned `session-group-commit.test.ts` runs independently against original source and passes **40 tests** under Node26/Vitest4.1.12 with official1.5.0 dependencies. Neither result proves the current Go implementation passes that original contract.

A bounded MAI wire audit, checked against actual source, confirms caller-visible mismatches in conversation ancestry/ownership, entry fields, canonical task fields, nested document scopes and the StorageWrite union. Parent inspection additionally rejects the audit's overly broad TaskOutcome/TaskState compatibility claim: TaskValue wrappers and omission of empty required fields still differ. The native Cursor is a base64 string, whereas the source cursor contract is a JSON object. Native SQLite frame schema/file interoperability remains unverified and explicitly differs in existing documentation. These are compatibility work, not accepted Go adaptations or N/A waivers.

MAI supplied immutable JSON primitives with parent-added policy/document-bound, actual-cycle, exact-number and detachment controls. They are not integrated into shared view mounts and are not proof of the original shared readonly view contract. No alternate canonical view API has been adopted.

Next actions: capture original source-produced record/write/cursor fixtures; establish unchanged-assertion Go failures at each public boundary; reconcile canonical API and persistence contracts without leaking private envelopes; port all40 group tests and shared-view tests before declaring alignment. Preserve existing user data/published assets, but do not use legacy native formats or stricter native choices to waive valid original behavior. Full test crosswalk, remaining schemas/dependencies, actual original-suite conformance, migration, security/fault/profile gates and release acceptance remain open.

Transport recovery checkpoint: ECONNRESET was reported after the source audit. Workspace files and local changes are intact; no build/test process was running at inspection. All durable work remains uncommitted on top of `028fb76`; no durable checkpoint has been pushed or represented as a drop-in release. Exact pinned source/reference inputs remain under project-owned scratch for active conformance work; raw diagnostic logs should be disposed after their conclusions are no longer needed.

## Original wire conformance baseline

`scripts/capture-durable-task-records.mjs` captures actual official pi-durable1.5.0 MemoryStorage commit/read outputs. The generated `durable/testdata/pi-durable-v150/task-records.json` includes13 valid records: nullable/primitive input, raw nullable memos, empty waiting On, zero deadline, all terminal outcome forms, explicit empty reason, optional zero metadata, and an opaque stored checkpoint. No native aliases or payload rewrites are applied.

The pending overlay `task_record_dropin_json_test.go` exercises genuine Go public TaskRecord/TaskState JSON import/export. Baseline: all13 TaskRecord cases and8 TaskState cases fail. These failures are required compatibility work, not accepted adaptations. The public/private type and codec boundary must be corrected before these assertions can be promoted. Raw native execution envelopes must remain private and old stored data must not be corrupted during the refactor.

A local checkpoint may preserve the independently tested lifecycle/read/watch fixes and these explicit red baselines; it is not a drop-in candidate or release, and must not be described as such. Native suite pass counts exclude these known-failing overlay tests until their original assertions pass.

## Original scan cursor contract

`scripts/capture-durable-cursors.mjs` captures 31 original MemoryStorage scan pages (conversations, entries, tasks, submissions; default/ascending/descending; legacy no-order, extra-field, changed-filter, order-mismatch and invalid cursors) into `durable/testdata/pi-durable-v150/scan-cursors.json`. `scan_test.go` replays them against native SQLite with unchanged item and `next` assertions.

Native `Cursor` changed from a base64, query-bound string to the original plain JSON object `{after, order}` (`type Cursor map[string]any`, nil = start/end). As in the original `scanStart`, filters are not bound to the cursor, order mismatches and non-safe-integer `after`/invalid `order` are `StorageRequestError`, and an order-less cursor continues in the scan default. Document cursors read only `after`. Old native tests that asserted query binding or base64 shape were changed to the original semantics; this is a caller-visible API break for Go consumers holding persisted string cursors (none found in this repo or `gi`).

Verification: vet clean; cursor/scan/history/document/request tests pass; full `./durable` passes (965s with `-timeout 1500s`; an earlier run hit Go's default 600s deadline while `TestTaskRecoveryRejectedCascadeExactOwnerVariants` was running, and that test passes count5 in isolation). The 351s→965s wall-time difference is unexplained and not yet profiled. Entry/StorageWrite wire shapes and the 40-test group-commit port remain open.

## Original Conversation/Entry record wire

- Fixture: `durable/testdata/pi-durable-v150/entry-records.json` (3 conversations, 5 entries), captured from original MemoryStorage by `scripts/capture-durable-entry-records.mjs`. Test: `durable/entry_record_wire_v150_test.go`. Red baseline: `parent`/`owner` objects were rejected, and entries emitted native `conversation`/`value` keys.
- `Conversation` and `Entry` now marshal to the original shape in declared key order, with canonical nested map keys. Conversation emits `id`, `parent{conversationId,at}` and `owner{conversationId,taskId}`. Entry emits `id`, `conversationId`, `kind`, `model`, `data`, `head`, `edits` and `byTaskId`. Absent, `null` and `0` data stay distinct through `HasData`.
- The native SQLite envelope is unchanged. `Write` and `storageCheckpoint` decode through method-free `storedConversation`/`storedEntry` mirrors, and the native encoder bypasses the public codec (`isNativeJSONType`). Three internal re-decode sites now use `storedEntry`: two passive submission drafts and the generation checkpoint's `inputMetadata`.
- `Conversation.OwnerConversation` is derived from the owner task on prepare and checkpoint restore. It is never persisted.
- Remaining differences: native message entries keep their payload in `Value`, which the public form emits as `value` only when it is non-empty; the original stores messages in `model`. `Conversation.Name` is not part of the original record and is not emitted.
- Verification: `go vet` is clean. The full `./durable` suite passes in 356s, which resolves the earlier 965s timeout run.

## Original StorageWrite wire slice

`scripts/capture-durable-storage-writes.mjs` wraps the official 1.5.0 `MemoryStorage.commit` while a real original Session runs. `durable/testdata/pi-durable-v150/storage-writes.json` contains 12 batches (24 commands) covering all eight tags, all submission lifecycle branches, conversation/task/session document scopes, and both base and delta document changes. The first nine batches retain the original capture. Additional batches cover empty `requestId`/`reason`, explicit null and primitive details, an empty trace, and task document changes.

`StorageWrite` now imports and exports those commands with unchanged JSON assertions in `storage_public_test.go`. Its Go `Value` must match the tag (`Conversation`, `Entry`, `TaskRecord` or `SubmissionRecord`, by value or pointer). Document commands use `DocumentCreate`, `DocumentContent` and `DocumentCopySource`; the union codec emits numeric or `"current"` copy points without changing the private point representation. An optional `*string` document key distinguishes an empty family key from a singleton. The public submission codec emits direct optional `detail` JSON, never the private `TaskValue` wrapper or compatibility `Value`.

The initial red baseline dropped every command's payload. All 24 original commands now round-trip. Additional controls cover malformed/unknown/mixed unions, empty bases and operation arrays, zero/current copy points, exact large JSON numbers, failed-decode destination ownership, caller marshal authority and depth limits. A fixed native-envelope control retains `op`, scalar owner/ancestry, `conversation`, `value` and the private copy-point object. No SQLite frame/checkpoint schema or persistence path changed.

The first full regression run found three old trace DTO fixtures still importing native `conversation` drafts into the newly public `SubmissionRecord`. These now import original records (`id`, `conversationId`) and explicitly remove the ID when passing the existing ID-less creation API. Trace presence, mutation isolation, malformed-carrier and reopen assertions are unchanged. Final verification: the full `./durable` suite passes in 358.260s; focused `TestPiDurableV150|TestMainSubmissionTrace` tests pass count10/shuffle42 in 8.444s; repository vet and diff checks pass. The original capture reproduces byte-for-byte. Commands use Go1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4`, the project environment and Makefile targets. Raw run logs and overlays were removed after recording these results.

This completes a wire slice, not public storage integration. `Storage` still exposes the native `Apply(Batch)` contract, and `OpenSession` still requires native storage. There is no public grouped `commit(batches, receive, context)` path yet. The message-entry `value`/`model` gap also remains. The new union must be integrated into that storage contract before a drop-in API claim.

### Group-commit baseline after the wire slice

The existing source-170 pending test was compiled through a temporary Go overlay, with its assertions unchanged. Result: expected failure in 1.081s. Independent callbacks b/c did not run before a's held storage call was released, and calls were `[1,1,1]` instead of `[1,2]`. No group-publisher runtime change was made; the full 40-test port remains open. This test holds native append after the core gate is taken, whereas original `BatchStorage` holds before delegation. It therefore remains a narrower native seam baseline; it cannot establish source-faithful overlap of durable ID allocation. See `session-group-design.md` and `group-port-proposal.md` for the required dependency, allocator, receipt-prefix, outcome and publication-scope work.

## Message-entry slice (partial)

- Readers accept both the original v1.5.0 shape (`pi.user` / `pi.assistant` / `pi.tool-result` with `model[]`, see `src/harness/generation.ts:880,1154`) and the legacy native shape (`kind:"message"` with `value`). Helpers are in `durable/message_entry.go`.
- Producers still write the legacy native shape. Switching them to `model[]` caused 37 failures and 1 hang (`TestMainContinueAfterAbortedToolRound`). The cause: native receipts carry `errorCode` and tagged `details`, and `copyMessages` rejects those as an "invalid entry contribution details/error union".
- The red conformance test is parked in `durable/testdata/upstream-main/pending/entry_message_kinds_v150_test.go`.
- Still needed: map native receipts to original message fields (error/details placement) before producers can switch. Existing SQLite stores keep the legacy shape and need no migration.
- Verification: full `./durable` suite passes (344.7s); `make vet` and `git diff --check` are clean.

## Public grouped storage commit

- `Storage` now exposes the original grouped `Commit(ctx, batches, receive) (CommitResult, error)` in addition to the native `Apply(Batch)` contract. `durable/storage_public.go` translates the public `StorageWrite` union to the private native `Write` envelope under the core line and settles each batch as one atomic commit with its own sequence.
- `CommitResult{Stored, Refused}` implements part of the original `{stored, refused?}` contract: an error-valued Durable-check refusal resolves with `Refused` set rather than rejecting, batches before the refused one stay durable, and a backend failure still rejects. Arbitrary original refusal values, including falsy values, are not losslessly represented by the public Go error field (D03). `receive` is a durability receipt (index, sequence), not publication.
- Internal split: `commitPartsAt` is a thin wrapper over `commitResolvedParts`, which evaluates a per-batch resolver while the core line is held, so a document change, retirement or copy resolves its address from the running base image instead of from caller-supplied state.
- Records the native store cannot represent in its own envelope are refused before becoming durable rather than written in a different shape. A public task record for a built-in (`pi.*`) kind is refused because its raw checkpoint contract is not reversible from the public record alone; `Conversation.OwnerConversation` supplied by a caller is discarded, since storage derives ownership on adoption.
- `durable/storage_public_test.go` replays all 12 captured original batches (`testdata/pi-durable-v150/storage-writes.json`) through `Commit`: 12 accepted batches, no refusal and sequences 1..12. Record equality now uses exact `canonicalJSON`, including present-empty `requestId`; `dropEmptyRequestID` is removed (D08 fixed). The fixture also pre-mints IDs, unlike the original raw Storage explicit-ID contract (D28). It checks copy at sequence 3 materialising the pre-delta base value and the retired session document.
- Verification: `durable/storage_public_test.go` and the replay test cover grouped ordering, refusal stopping a call, native refusal of unrepresentable records, native ID ownership and partial original-wire replay. Built-in task refusal and stricter raw ID ownership are open incompatibilities (D09/D28), despite their passing safety controls. `go vet ./durable` was clean.

### Submission fidelity fixes found by the replay

- The original stores an explicitly empty failure `reason` and reads it back empty. Native `nativeSubmission` rejected an empty reason for `unanswered`, so the captured batch was refused. The check now accepts it; the write union already requires the `reason` key to be present for that status.
- `publicSubmission` now treats a present `errorCode` as the reason and falls back to the native status only when the key is absent, so an empty reason no longer reads back as `failed`.
- D29 is now fixed: the continue-submission private validator and typed `SettleSubmission` accept a present empty unanswered reason. Missing/non-string private reasons, missing public-wire reasons and illegal union fields remain rejected. The original captured continue payload round-trips exactly before/after reopen.

### Fixed: present-but-empty `requestId` (D08)

The original keeps `requestId: ""` as a present key and indexes it as a real deduplication key (`storage/memory.ts:430`). `Submission`, `SubmissionRecord` and `Input` now expose `RequestIDPresent`; nonempty `RequestID` implies presence for existing callers. The native bounded encoder emits a present empty string in the existing `requestId` field, and strict native decoding retains that presence. No extra storage field, table/configuration version change or automatic migration is introduced. Legacy absent/nonempty records retain the same JSON shape. Presence already lost by old writers cannot be reconstructed, so legacy absent fields remain absent.

Empty-key lookups distinguish absent records; transaction/Harness admission, lifecycle replacement, status, frame replay and checkpoint replay preserve the key. Native identity and uniqueness guards remain enforced; general raw duplicate-request/update policy still needs the D12 storage review. The 12-batch original replay now compares every record exactly. It still pre-mints IDs and does not establish D28 allocator/bootstrap parity.

### Fixed slice: empty Storage batches (D28)

Each inner empty batch now commits with its own sequence and receipt. Empty outer calls consume no sequence. The original captured first receipt is unchanged; grouped empty batches survive reopen/checkpoint replay, and real SQLite rollback plus later-refusal controls preserve the correct durable prefix. The private envelope uses the existing `writes:[]` array; malformed `writes:null` stays rejected. Older binaries lack the new empty-batch/empty-key semantics and may reject these newly valid records; no existing data is rewritten. D28 remains open for root preinstallation and explicit-ID allocator semantics.

### Three-deviation verification checkpoint

Full durable regression passes in 725.786s with the separate writer prototype excluded by compile overlay. The broader focused selection passes count3/shuffle42 in 99.942s; corrected snapshot/receipt controls plus V150 cases pass count3/shuffle42 in 81.483s. The complete durable/tools suite passes in 21.942s; repository-wide vet, Staticcheck and logging checks pass with CGO_ENABLED=0. The first full run caught nondeterministic native JSON map ordering and an obsolete empty-receipt rejection assertion; both were corrected before the passing full run. See the [deviation register](durable-deviations.md#three-deviation-implementation-checkpoint) for baseline failures, compatibility limits and remaining scope.

### D28 explicit IDs

Raw `Storage.Commit`/`Apply` (not Session-owned) accept unused explicit IDs in the safe-integer domain and raise the allocator in the same atomic frame; replay verifies the recorded high-water value. Session transactions keep reservation. Fresh-root preinstallation is the remaining D28 gap. Verification: see the [explicit-ID checkpoint](durable-deviations.md#explicit-id-checkpoint).
