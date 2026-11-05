# Durable semantic port — work in progress

The user explicitly requested a semantically equivalent durable port with the same guarantees. Target remains official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`; starting native revision `028fb76cace523a92fe70ccf60309071f24f2163`. This request authorises source-aligned implementation of the previously paused lifecycle/admission contracts. It does not authorise weakening storage durability, ownership, cancellation, immutable publication or resource bounds. No complete parity result is claimed.

## Source and execution

Exact ai/schema/durable source was fetched under the resolved project run root, `runs/durable-port/pi-f9a26365568127461c47a07a37dcb42c140a3ac5`. It is temporary input, not a committed dependency. Use `source scripts/project-env.sh`, Go 1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4` and Makefile targets. Raw diagnostic files under this run are disposable after their analysis. No other-agent contact, hosted dispatch or release publication has occurred.

## Tested slices so far

### Session-line history reads

Pinned `session-group-commit.test.ts:256` uses five Session-ordered surfaces. The old native translation incorrectly used direct raw Storage for its two line reads; a raw store does not inherit Session job ordering. Current baseline reproduced both direct-read early unknown-conversation errors. This was a test-translation error, not evidence that raw SQLite reads must acquire the Session line.

`session_read_line_v150_test.go` translates all five surfaces faithfully: transaction latest-entry ID, transaction head marker, transaction entry scan, line entry scan and line head-marker read. The initial compiler baseline established missing APIs, not runtime failure. New `Session.ReadOnLine`, callback-scoped `ReadLine`, `Tx.LatestEntryID` and `Tx.LatestHeadMarker` provide the missing capabilities. Reads use the committed Session image, preserve fork ancestry, detach returned records, reject escaped/overlapping capabilities and honour caller cancellation. Five surfaces plus lifetime/cancellation controls pass count3/shuffle42 (0.938s). The earlier case256 translation now routes line reads through ReadOnLine, preserving its original no-early-outcome assertions.

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

The earlier after-run diagnosis was incorrect: the pending fixture omitted upstream's `waitForIdle` before checking the hook. Restoring that wait and the exact bounded raw-history kind sequence passes without changing generation behavior. The promoted `harness_after_run_history_v150_test.go` retains every non-system contribution, mapping only the native stored message/role envelope for assertion comparison. That representation mapping is not evidence of drop-in wire compatibility.

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
