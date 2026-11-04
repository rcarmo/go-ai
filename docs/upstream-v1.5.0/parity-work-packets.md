# Parity work packets

These packets split the v1.5.0 backlog into bounded assignments a model can execute without the chat history. Complete one packet at a time. Investigation packets produce implementation decisions; they do not grant permission to change paused runtime contracts.

## Shared brief — include with every assignment

- Repository: `/workspace/projects/go-ai`. Read its `AGENTS.md` and the relevant source before editing.
- Sole upstream target: `f9a26365568127461c47a07a37dcb42c140a3ac5` (official v1.5.0). Mirror: `/workspace/tmp/go-ai/cache/upstream-pi/pi-repo`. Mirror HEAD differs; always use `git show <pin>:<path>` or `git grep <pin> -- <path>`.
- Start state when written: `HEAD=7d40389`; `origin/main=c422344`. Verify both before starting. Never reset someone else's changes. Existing local changes and later revisions supersede these starting refs.
- Pure Go: source `scripts/project-env.sh`, export `CGO_ENABLED=0`. SQLite/modernc is the sole backend; QuickJS runs in wazero.
- Focused tests only. No full suite, profiling, hosted CI, release or push without separate authorisation. No other-agent contact unless Rui explicitly authorises it. Do not start a child model merely because this document exists.
- Preserve untracked `durable/continuation_prototype_main_test.go` unchanged. Exclude it from compiler input with a Go overlay. Do not add it, move it, alter Git excludes or silently compile it.
- All scratch beneath resolved `$PROJECT_TMP_ROOT/runs/<packet>/`; caches use project environment settings. Delete raw logs, overlays, fault copies, binaries and fixtures after analysis. Keep concise conclusions in source/docs.
- Never use `git rebase`. Commit only assigned files as `Rui Carmo <rui.carmo@gmail.com>` after setting local and global identity. Do not use blanket `git add -A`.
- Preserve pinned assertions. A missing native surface is an implementation gap, not automatic exemption. Label faithful tests, adaptations, serial approximations, failures and unexecuted assertions separately. Do not weaken security/ownership/cancellation/numerical contracts to get a pass.
- Read `durable/testdata/upstream-main/pending/README.md` before running a pending fixture. Expected failures remain outside the normal package build until the source assertion passes unchanged.

## Reproduction harness

Run from the repository root. Set `PACKET` to the packet ID and `PENDING_FILE` to one file listed below (or leave it empty). The overlay adds only that pending file and excludes the protected prototype. This is setup, not an instruction to run every oracle.

```sh
. scripts/project-env.sh
export CGO_ENABLED=0
export PACKET=p00-contract-map
export RUN="$PROJECT_TMP_ROOT/runs/$PACKET"
mkdir -p "$RUN"
printf 'package durable\n' > "$RUN/prototype_stub.go"
export PENDING_FILE='' # e.g. harness_after_run_history_test.go
bun -e '
const r=process.env.RUN;
const replace={"/workspace/projects/go-ai/durable/continuation_prototype_main_test.go":`${r}/prototype_stub.go`};
if(process.env.PENDING_FILE) replace["/workspace/projects/go-ai/durable/zz_packet_pending_test.go"]=`/workspace/projects/go-ai/durable/testdata/upstream-main/pending/${process.env.PENDING_FILE}`;
await Bun.write(`${r}/overlay.json`,JSON.stringify({Replace:replace}));
'
# Replace TEST_REGEX with the exact packet test names. Quote alternation inside TEST_FLAGS.
make test PACKAGES=./durable TEST_FLAGS="-overlay $RUN/overlay.json -v -run 'TEST_REGEX' -count=1 -timeout=45s"
# After changes, run the packet's bounded regression set, then:
go vet -overlay "$RUN/overlay.json" ./durable
```

A failing pending oracle is baseline evidence, not a successful implementation gate. Capture full timeout output before diagnosing it. Never pipe a test command through `tail` and infer success from the pipeline exit status. Use bounded timeouts, failure-safe hold release and joined workers. After useful results are recorded, delete only this packet's inactive scratch directory.

## Required hand-back

Return: starting/ending revision; exact source assertion; files changed; baseline result; fix and contract effect; focused results with names/counts/duration; effective fault check if practical; unresolved assertions; cleanup confirmation. Update only the assigned crosswalk row's evidence until a complete assertion review justifies changing its disposition. No whole-file parity from a selected pass. Stop and report a concrete blocker instead of inventing an API or silently accepting an approximation.

## P00 — Contract map and decisions (active, investigation only)

**Goal:** make the first implementation assignments safe and unambiguous.

1. Read P01–P04's pinned source and native paths below, plus each pending fixture completely.
2. Create a small table per issue: expected source order/state/error, measured native behaviour, likely cause versus uncertainty, legacy tests that encode conflicting behaviour, proposed migration, focused acceptance list, approval required.
3. For afterRun, trace checkpoint/hook selection from the first generation through tool completion and answer. For close, trace closing flag → invocation signal → abort handling → task outcome → requeue → reopen.
4. Identify the exact decision Rui must make for the two paused runtime changes. Recommend a source-aligned option and state which native tests/contracts it changes. Do not ask Rui to diagnose the code.
5. Produce `docs/upstream-v1.5.0/parity-contract-decisions.md`. Keep it concise and actionable; implementation packets below should link to the decisions once accepted.

**Done:** each P01/P02 conflict has a source-backed proposed resolution and a named acceptance set. **Stop:** no runtime changes in this packet; a source/fixture discrepancy must be resolved before requesting approval.

## P01 — Tool-round answer afterRun (implementation after P00 approval)

- Source: `packages/durable/test/harness-after-run.test.ts` around 694; `packages/durable/src/harness/generation.ts` around 864/893. Verify actual paths/anchors in the pinned tree.
- Native: `durable/generation.go` (`finishWithAfterRun`, `finishGeneration`, hook selection), `durable/generation_handoff.go` (`handoffToolGeneration`), generation checkpoint definitions found by symbol search.
- Pending file: `harness_after_run_history_test.go`; test `TestMainAfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction`.
- Baseline: hook is never invoked after the model tool round; later history assertions are not reached. Handle-update semantics matter: modifying `DocumentHandle.Get()` output alone does not stage a write.
- Work: carry/recompute the correct selected hook state into the post-tool answer path, preserving first/answer/last boundaries, hook cutoff and summary timing. Match source answer-before-compaction semantics. Do not blindly copy stale hook state into unrelated successor runs.
- Acceptance: pending oracle unchanged, `TestMainAfterRunChildRestartLimitCompletesGeneration`, `TestMainAfterRunHookReadsCommittedEntryBoundaries`, `TestMainCutoffHooksAtLimitNotPastLimit`, `TestMainAfterRunRunWithoutRecordedFirstEntryAnswersAsFirst`, and relevant handoff tests discovered from changed symbols. Start count1; repeat only this set count3/shuffle42.
- Bounded history: the pinned `TaskRuntime.entries` contract (`packages/durable/src/types.ts:328`, `harness/scheduler.ts:2828`) exposes committed, fork-aware history with inclusive entry-ID bounds and ordered pagination. Source694 uses it inside `afterRun`. Port this existing API and its semantics; it is not an invented scan. Native `HookAPI` embeds `TaskRuntime`, but that runtime currently lacks the corresponding range scan. Keep the ordered-kind assertions unverified until the port and oracle pass.
- Boundaries: generation/handoff, the source-required hook history surface and narrowly affected tests/docs. No close/group/view/schema work. Promote the oracle only after all portable assertions pass. The P01 runtime pause still applies.

## P02 — Close signalling, abort fencing and requeue (implementation after P00 approval)

- Source: `harness-after-run.test.ts` source429 and `harness-group-commit.test.ts` source758, plus pinned close/scheduler implementation found from those tests.
- Native: `durable/harness.go` (`Close`), `durable/task_scheduler.go`, `durable/close_requeue.go`, `durable/session.go`, invocation cancellation and outcome-settlement helpers found by tracing callers.
- Run pending files separately: `harness_after_run_close_drain_test.go` / `TestMainAfterRunCloseInDrainResumesInterruptedAttempt`; `harness_group_commit_close_in_flight_test.go` / `TestPendingGroupCommitCloseSignalsInvocationDuringCallInFlight`.
- Baseline: running drain is not signalled at the required close boundary; abort callback does not run; a phase can commit while closing and finish instead of leaving an interrupted attempt for re-execution. In-flight storage still drains before close returns.
- Work: fence the interrupted attempt and signal hosts at the pinned boundary without cancelling an already admitted storage settlement. Reconcile abort-handler outcomes with requeue. Preserve checkpoints, restart budget and crash recovery.
- Acceptance: both pending oracles separately and together with `TestClose140RequeueDoesNotConsumeRestartBudget`, `TestClose140RejectedRequeueLeavesCrashRecovery`, `TestMainGroupCommitChildEndsBeforePhaseWhenMarkUnpublished`, and existing explicit-abort/recovery tests selected from changed paths. Verify reopen and no late stale-attempt commit; test rejected/uncertain requeue storage paths.
- Stop: do not convert every cancellation into `faulted` or requeue already decided outcomes just to satisfy the oracle. Explain necessary legacy-test changes before making them.

## P03 — Opposite-wait cycle admission

- Source: `packages/durable/test/harness-group-commit.test.ts` source226 (#11141) and the pinned dependency/admission cycle implementation it calls.
- Native: `durable/task_definition.go`, `durable/task_scheduler.go`, `TaskRuntime.MemoCandidate`, waiting state `On`/policy, ownership/dependency validation located by symbol search.
- Pending file: `harness_group_commit_wait_cycle_test.go`; `TestPendingGroupCommitRefusesSecondOfTwoOppositeWaits`.
- Baseline: both opposite waits commit; neither task completes; refusal list stays empty during the bounded observation window. Native can express this case; do not relabel it unportable.
- Work: find the earliest valid cycle-refusal boundary using the effective dependency image, including pending intent where appropriate. Preserve legitimate acyclic waits, allSettled/failFast and cancellation/ownership semantics.
- Acceptance: retained oracle unchanged; two-node and longer cycle refusal; acyclic chain; shared dependency without a cycle; foreign-owner rejection; abort during a wait. Reuse reconciled task-ownership/group controls and document newly derived tests separately from source tests.
- Stop: if fixing this depends on concurrent admission P05, show the exact dependency; do not add a timeout-based deadlock detector as a substitute for admission refusal.

## P04 — Refusal versus uncertain settlement (investigation before implementation)

- Source: `packages/durable/test/session-group-commit.test.ts` source414/441/461/486/680 and native storage-group contracts.
- Native: `durable/storage_group.go`, `durable/session_receipt.go`, `durable/session_publication.go`, `durable/session_group_commit_main_test.go`.
- Pending file/test: `session_group_commit_refusal_test.go` / `TestPendingGroupCommitRefusalFailsTheSession`.
- Critical fixture limit: this pending fixture exceeds `MaxWrites` and is rejected before admission. Source414's predicate refusal occurs inside a storage call. Its failure is evidence about the native rejection path, not proof those mechanisms must share a latch policy.
- Work first: build a classification table for pre-admission invalid input, confirmed backend refusal/rollback, uncertain settlement and adoption failure. Trace each error through storage, Session latch and observer ending. Identify a faithful in-call refusal injection if the native surface supports it.
- Acceptance design: earlier part remains durable, failed/later parts do not publish, correct later outcomes fail, original cause retained where required, reopen coherent. Preserve `TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError` and distinguish its native uncertain-outcome classification from source error identity.
- Stop: do not make all invalid user commits poison the Session to pass an unfaithful oracle. Implementation starts only after this mechanism review; group-part cases may depend on P05.

## P05 — Session group publisher (design, then staged implementation)

- Source: pinned `session-group-commit.test.ts` in full, especially170,414–815,920; relevant `session/session.ts` line/admission/publication code.
- Native: `durable/session.go`, `session_publication.go`, `session_receipt.go`, `storage_group.go`, `session_deferred.go`; existing `TestInternalSQLiteGroupPostCommitReceipts` and `TestMainSessionGatesJobsWaitForPublication`.
- Existing limitation: Session serialises writers and calls the storage group path with one batch. Storage multi-batch support alone does not implement concurrent Session admission.
- Design note: `docs/upstream-v1.5.0/session-group-design.md` distinguishes source's serial callback execution from its overlap with in-flight storage, maps the native global baseline and records unresolved dependency, prefix-image, cancellation and Go delivery-scope questions.
- Split implementation: (a) explicit prepared-job/part state and cancellation ownership; (b) admit independent callbacks serially while a prior storage call is held; (c) batch queued parts with distinct sequences; (d) adopt/publish exact per-part images in order; (e) refusal/failure fences and continuations; (f) deferred ordering/backpressure.
- First faithful acceptance: source170 callbacks for b/c run while a is held, no premature publication, storage call shapes `[1,2]`, three distinct increasing sequences, completion order a/b/c. Do not count the existing serial approximation as this pass.
- Later acceptance: no later part publishes after failure; cancellation before/after storage admission is classified correctly; source920 starts only255 additional jobs behind the first, then all300 complete and line counters reach zero. Publication scope/JS scheduling adaptations need explicit design decisions.
- Done per stage: focused tests, independent per-part receipt validation, no leaked jobs/held locks; leave unsupported stages open. No unreviewed parallel Session mutation.

## P06 — Synchronous document observers and Harness wiring

- Source: pinned `session-observers.test.ts` and `session.ts` observer guard; harness view/task-graph observer callers.
- Native: `durable/observation.go`, `session_sync_observers.go`, `session_local_observers.go`; `docs/upstream-v1.5.0/session-observers.md` maps current gaps.
- Public `ObserveDefinition` on Session, Harness and conversation handles wraps the scheduler-capable synchronous document guard. It returns detached Go values; public readonly identity and task-runtime-owned attachment remain open. The separate private stored-commit lane still rejects scheduler-backed Sessions. Asynchronous `DocumentWatch` is a different interface.
- Design: `docs/upstream-v1.5.0/document-observer-design.md` maps acquisition/pin guard, registration atomicity, value/ops on memory and stored adoption, per-change detach checks, retirement/recreation, callback errors and ownership. The private standalone source77/87/105 slice and scheduler-backed controls are implemented; they do not establish public observer or identity parity. Source `types.ts:1094–1116` already forbids synchronous observer callbacks from calling the Session or blocking. Enforce and document that restriction rather than treating re-entry as an open source contract.
- Stage acceptance: source77/87/105, then132/162, then241/254, then469/496/520. Keep JS rejected-thenable and WeakRef timing gaps separate. Preserve stored-lane source389/403/414/427 tests; wire Harness only with scheduler publication-order tests.
- Stop: no asynchronous-watch substitution and no shared mutable maps without P07's ownership decision.

## P07 — View identity and mount lifetime (design before runtime change)

- Source: pinned `harness-view.test.ts` source160/274/330/525/570.
- Native: `durable/view.go`, `view_publication.go`, `view_window.go`, `view_gc_oracle_main_test.go`, `view_identity_oracle_main_test.go`.
- Pending file: `harness_view_shared_docs_test.go`; three functions: `TestPendingViewObserversShareDocsIdentity`, `TestPendingViewSource160UnchangedMapIdentity`, `TestPendingViewSource525WindowDocsIdentity`.
- Known cause: `Value()` and callback deliveries deep-copy public maps. Source requires unchanged-map reuse and cross-observer shared docs. `TestConversationWatchTranscriptDeltasDetachedAndOffLine` intentionally permits mutation of detached returns; these contracts conflict.
- First deliverable: propose a source-aligned immutable/shared representation or a clearly specified API migration; inventory affected callers and legacy ownership tests. Do not bypass copies in place and expose writable aliases.
- Implementation acceptance after decision: pending map-identity assertions unchanged; exact source525 window contents keep passing; unrelated commits remain silent; listener failures isolate peers; last observer releases mount and remount builds the required new identity. Empty-array JS object identity and individual EntryRecord GC need explicit representation/adaptation decisions.
- No more copy-pointer probes as a substitute for the design decision. Existing Go backing-array GC evidence is useful but establishes a narrower lifetime contract.

## P08 — Watch bounds and deferred-view delivery (design before limit changes)

- Source: pinned `session-watches.test.ts` source183/202/229 and `harness-view.test.ts` source128, plus session-deferred-commits tests.
- Native: `durable/watch_pending.go`, `delta.go`, `json.go`, `types.go`, `session_deferred.go`, `view_deferred_memory_main_test.go`, `view_generation_persistence_main_test.go`.
- Measurements: native `maxDeltaOperations=4096` and default `MaxMembers=4096`; distinct-op folding overflows at4097. Source202 uses10001 keys; native rejects that value at the JSON container limit. Source128's historical persistence witness must be rechecked against current code before proposing a fix.
- Work: separate transient pending-op limits, document container limits, encoded-byte quotas and storage frame limits. Compare source bounds and estimate resource impact. Specify bounded alternatives preserving cancellation, ownership and replay correctness; do not globally raise limits without analysis.
- Acceptance: source183 same-field folding, source229 excludes in-flight frame, source202 root replacement with all10001 keys when representation permits, native quota failures remain bounded, held changes persist at unload/reload/deadline/close, view frame/storage-touch witness reconciled.
- Preserve `TestMainWatchPendingDelivery`, `TestMainWatchPendingNativeBounds`, operation replay and retirement/recreation controls; explicitly reconcile changed threshold expectations.

## P09 — Crosswalk triage into new work packets

- Inputs: `docs/upstream-v1.5.0/upstream-tests-f9a26365.tsv`, `dependency-tests-f9a26365.tsv`, `upstream-review-f9a26365.json`, README and per-topic receipts.
- Process one subsystem at a time. For each file, enumerate actual pinned test assertions, compare native tests/implementation, locate missing assertions and classify them. Prioritise wrong outcomes/data loss/security over identity/diagnostic/performance details.
- Output a small packet per unresolved cluster using this document's shared brief, source/native paths, baseline, acceptance and stop conditions. Update evidence with exact scope; do not bulk-mark unchanged/historically tested files as passing.
- Done for an assignment: a named bounded subset fully reviewed with implementation tickets for gaps. The362-path counts are inventory status, not a percentage of parity; avoid meaningless completion estimates.

## P10 — Remaining durable contracts (split before assigning implementation)

Use P09 to create separate packets for: restart/retry charging; dependency/continuation ordering; submission/turn limits; tool-hook ordering; cross-owner export; environment/telemetry; codemode integration. The two-source-test [hook/task history packet](hook-history-packet.md) identifies the missing runtime range scan and its acceptance boundaries; P01's runtime pause remains in force. Start from the existing per-topic v1.5.0 receipts and pinned test files; derive the native edit/test set by symbol tracing. Preserve selected passing controls and label any source-internal API adaptation. Do not assign this whole list as one coding task.

## P11 — Schema contracts (split before assigning implementation)

- Native starting points: `schema_strict.go`, `schema_validation*.go`; schema crosswalk under `docs/upstream-v1.5.0/`.
- First packet: recover the prior `multipleOf` pause reason and conflicting native tests; compare pinned source numerics before choosing a rule. No threshold widening or approximate numeric cache keys.
- Subsequent independent packets: recursion/non-JSON values, patterns, cloning/alias/cache identity, diagnostics. Each needs exact failing source assertions and focused positive/negative controls before edits.
- Stop if the desired rule is unknown; produce a source-backed contract decision, not a generic rewrite of validation.

## P12 — Provider, credential, transport and catalogue (split before assigning implementation)

- Start with `docs/upstream-v1.5.0/release-metadata.md`, `cmd/check-model-catalog/main.go`, catalogue regeneration commands referenced by that receipt and the Makefile, provider adapter files found through the package inventory. Existing1712-record/25-schema checks cover selected metadata contracts only.
- Separate packets for each provider wire contract and for shared cancellation/reader retirement/credential isolation. Specify mock transport fixtures; no real paid provider calls or secret disclosure.
- Acceptance: exact request/response semantics at the pinned target, isolated credentials, cancellation closes owned resources without retiring unrelated readers, current catalogue regeneration deterministic under its documented semantic contracts.
- No broad provider rewrite from the file inventory alone; P09 must first identify a bounded mismatch and test.

## P13 — Acceptance (blocked on authority and resolved implementation scope)

After Rui authorises broader verification: run applicable full-suite/security/fault/regeneration/build gates. Pre-release CPU/heap allocation profiling must inspect CPU, alloc_space and alloc_objects, address material outliers and remeasure equivalent workloads. Preserve concise results and remove raw captures after use. Define the complete command matrix from the then-current Makefiles and reviewed contracts, not historical receipts. No release/parity declaration with unexplained failing oracles or silently waived API gaps.
