## Runtime-owned document progress

A private `TaskRuntime.commitProgress` lane now admits eligible conversation-document changes into Session memory without storing a task checkpoint. Public `TaskRuntime.Commit` remains immediate. Production generation still updates its partial through the task checkpoint and has not switched to this lane.

Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `8d8bced5bc943718dff5145ee731ba6ed6257e42`; rollback: `4f468999cdafe9cdf3fc1456d8adf92049f4d403`. Native v1.4.0 and assets are unchanged.

### First generation batching oracle

`durable/testdata/upstream-main/pending/harness_deferred_progress_test.go` translates the scenario at `packages/durable/test/harness-deferred-progress.test.ts:100-115`. It retains the two literal assertions: more than twelve memory partials and fewer stored changed-document batches than `memory.length / 1.5`. The counter observes actual storage append batches across all changed documents, including changed whole-value native bases; it does not filter out task-related writes to improve the ratio.

The native provider emits twenty 32-character prefixes of the same 640-character answer. A controlled clock advances 100ms between emissions; publication interval zero and real watch acknowledgements make the samples deterministic. This replaces the source's timed faux-provider stream/default 100ms publication timer. Memory samples come from real conversation watches, selecting changed live-document revisions. A labelled extra control reads Storage directly after the first memory delivery.

On unchanged `4f46899`, CGO1 memory/journal/SQLite each deliver twenty memory partials and store twenty-four changed-document batches; the first partial is already stored. All three backend leaves fail the ratio and extra memory-only control (0.467s). The fixture stays outside default discovery, with no skip or relaxed count. No TypeScript/Vitest execution or complete upstream-body credit. This new baseline and the older generation stored-touch baseline were not repeated after the private lane change; production generation still uses its old path.

### Private lane and lifetime controls

`durable/harness_progress_lane_main_test.go` uses real native task definitions, runtime admission, document acquisition, watches and raw storage. Its original ordinary-Commit adapter fails six timer/Close backend leaves (0.166s): memory writes advance the stored sequence and no persistence timer exists. After that baseline, only the adapter selects `commitProgress`.

`TestMainHarnessProgressLaneMemoryAndFlush` checks memory visibility with an unchanged durable sequence, unchanged task state/checkpoint, a live invocation permit, current scheduler idle metadata, callback-error rollback, cancelled task waiters, timer flush without duplicate watch delivery, and healthy Close persistence. A third mode injects a lower-level append failure: the first native uncertain-settlement/poison cause is identical through the Session guard, a task waiter, re-entrant host report and failed ending; shutdown drops the unstored value. The native poison is the error identity checked here; an append probe's original error is not a confirmed storage refusal.

`TestMainHarnessProgressLaneLifetimeFences` checks ended, durably abort-marked and cancelled admissions before callback execution, plus immediate mixed task-state transitions. No ordinary runtime capability check is bypassed. Anonymous Harness deferred admissions still reject before invoking callbacks; non-native storage wrappers retain their existing unsupported boundary.

Memory publication updates task/idle bookkeeping before notifying waiters. A timer-only storage flush advances scheduler metadata without replaying the logical memory publication. An added idle-image assertion exposed rebuilds reading the stored image on equal-sequence memory adoption; rebuilding from `memoryReadImage` fixes it. The equal-sequence path currently rebuilds the idle index; CPU/allocation cost is unmeasured and belongs in authorised acceptance profiling.

### Focused verification

Go 1.27.3, GOMAXPROCS=4:

* New lane tests plus the changed anonymous-admission guard, six existing `TestMainIdleIndex` functions and eight affected task-scheduler capability/commit/abort/idle/progress controls pass normal `-count=3 -shuffle=42`: CGO1 2.000s, CGO0 1.205s; race CGO1 `-count=3 -shuffle=7`: 6.390s.
* The eight scheduler controls are `PhasesMemoWinnerAndEndedCapabilities`, `NoProgressAndCommittedTerminalPrecedence`, `IdleFrontierAdmissionAtomic`, `BoundAbortSealedOrSelfScopeAdmissionIsInert`, `ActiveProgressRespectsPendingCascadePrefix`, `RuntimeCommittedReadsClockReportsAndWatchLifetime`, `AdoptedOwnMarkKeepsReadsRejectsWritesAndJoinsBeforeFreshAbort`, and `OwnMarkAfterProgressSkipsNextPhase`, each prefixed `TestTaskScheduler`. The guard is `TestMainHarnessPersistencePolicyDynamic`.
* Durable vet passes both CGO modes. Staticcheck v0.8.2 and an additional vet pass include the pending batching fixture in a physical temporary package tree, both CGO modes; they do not execute it. Module verification, formatting, diff and protected-prototype comparison pass.
* Five active faults fail intended backend leaves: immediate runtime progress (9), omitted memory idle adoption (9), stored-only idle rebuild (9), refused scheduler timer maintenance (6), and missing Close flush (3).

Tests/vet overlay both logical and canonical paths to exclude the protected prototype. Staticcheck uses a physical copy without that file. Initial timer/definition/handle/Closed-result adapter compile errors and a missing definition checkpoint callback were corrected before baseline credit. The append-error expectation was corrected to the native uncertain-settlement contract before final checks. No assertion or storage validation was weakened to admit the lane.

Owned source copies, mutations, logs and completed build cache were disposed after analysis. Installed dependencies/toolchains, the pinned source cache and protected untracked prototype are preserved. No dependency, storage-format, coalescer, full-suite, profiling, CI or release changes. Completed observer, retention, lifecycle, deadline, view-memory and generation gates were not repeated.

### Remaining integration

Generation partials still write `generationCheckpoint.Partial`; `syncGenerationStatuses` projects the same durable task write into `pi.live`. Split presentation from durable intent and recover interrupted partials from stored live state, preserving retry charging, abort cleanup, provider-dispatch ordering and waiter/report lifetimes. Only then select the private lane for generation progress and run the unchanged batching/stored-touch oracles. Generic synchronous observers, public shared-value identity and the other eleven-body Harness recovery/tool/summarisation assertions are also unfinished. Full v1.5.0 alignment is unverified.
