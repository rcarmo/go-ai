# pi-durable parity closure

Current audited target: pi-durable 1.0.4. [Delta crosswalk](../v104/changed-paths-crosswalk.md), [47-suite corpus](../v104/test-corpus-crosswalk.md) and [final local verification](../v104/local-validation.md) supersede the target version and candidate receipts below. The following 1.0.0 closure is retained as historical scope. Rui authorised native v1.0.4 commit/tag/publication on 6 October 2026; [release audit](../../RELEASE.md) records the outcome.

Native runtime alignment with pinned pi-durable 1.0.0 is complete within the Go API/storage/platform boundaries below. The final candidate passes the repository, race, static and public-import checks. Scoped independent reviews accepted the corrected lifetime, hooks, owned-work handoff, reset, observer, shell, compaction and settings contracts. This is local runtime completion; no blanket independent replay of every TypeScript inner subcase or clean-clone release acceptance is claimed. Published releases and stash `9be4ebbd0c1de275d69bd95a4f1fc3db78f00e74` are preserved. Checkpoint `52d01064100411e62f8528cfa6de36f210f81aab` is the parent of the scoped local commit authorised on 6 October 2026. No push, hosted CI or publication is authorised.

## Reference and evidence

The reference is `/workspace/tmp/pi-mono-v100-exact/packages/durable`, the exact official 1.0.0 runtime. The official 1.0.1 durable inventory is unchanged. The [source crosswalk](contract-crosswalk.md) has 60 unique source rows; the [suite crosswalk](test-crosswalk.md) has 42 unique suite rows. Every named Go test in those tables exists. The older 191-declaration ledger covers task, ownership, lifecycle and table subsets; it is not an inventory of every case in all 42 suites.

Final candidate: isolated copy `final-native-RiBovr`, based on the unchanged checkpoint above. All 290 Go source/test files under `durable/` matched the tested copy before disposal. The earlier 284-file candidate receipts remain historical; they predate the final hook/lifetime/reset corrections.

| Gate | Result and receipt |
| --- | --- |
| Go 1.25.0 repository `make test TEST_FLAGS='-shuffle=on'` | All test-bearing packages passed; durable 51.984s, tools 4.211s; `run-20261006T094128Z-eVEHgw` |
| Go 1.26.6 `make test-race PACKAGES='./durable ./durable/tools' TEST_FLAGS='-shuffle=on'` | Both passed; durable 276.519s, tools 21.266s; `run-20261006T094128Z-MWnA0b` |
| Go 1.26.6 vet, staticcheck and logging gates | Passed |
| Go 1.25.0 `CGO_ENABLED=0 go build ./...` | Passed; the SQLite runtime was exercised by CGO-enabled tests above |
| External public-import consumer, Go 1.26.6 race count3 | Passed 1.124s; `run-20261006T094139Z-NvSAdF` |
| Same external consumer, Go 1.25.0 count3 | Passed 0.207s; `run-20261006T094143Z-rPW9AV` |
| `git diff --check`, unchanged HEAD/origin/stash | Passed |

The consumer used a local module replacement to the frozen copy. The final consumer compiled public HookAPI commit/owned-work and entry-identity callbacks, executed registered task hooks and journal-backed task completion, verified the narrower InvocationReader has no commit authority, and checked escaped-reader sealing. Earlier public-consumer receipts cover typed entries, staged first-terminal settlement, submission operations, no-model admission and journal reopen. The frozen copy and local replacement do not establish clean-clone acceptance. Older receipts and failures are recorded in [validation history](validation-history.md).

## Fixed obligations

| Obligation | Native proof | Evidence |
| --- | --- | --- |
| Bound waits observe queued passive-write settlement, not generation-task completion | `TestInvocationSubmissionWaitQueuedPassiveWriteUsesSettlementPublication` | race3 `060541-MmCtmt` |
| Bound status/abort/wait queued before terminal adoption lose authority | `TestInvocationSubmissionQueuedStatusAbortAndWaitRejectAfterSeal`; `TestTaskSchedulerBoundWriteAttributionAndEscapedSettlementSeal` | race3 `060541-MmCtmt` |
| Submission waiters register on the Session line; arbitrary adopted settlement wakes them; independent caller cancellation removes only its waiter | `TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling` | race3 `060541-MmCtmt` |
| Abort dispositions include raw queued records without inbox items, wrong-scope lookup, queued writes and placed input | `TestSubmissionReferenceStatusAndAbortDispositionScopeQueuedWrites` | race3 `061639-Y96wt6` |
| Harness forwards Session commit, typed document reads/history/state/watch and commit subscriptions without scheduling | `TestHarnessTypedDocumentForwardingHistoryStateWatchAndNoSchedule` | race3 `061639-Y96wt6`; typed-entry forwarding and pending-wait cleanup added, race3 062528-0LrVoE |
| Intent memo survives interrupted host effect; idempotent host deduplicates repeated call; terminal reopen has no replay | `TestTaskRecoveryIntentEffectOutcomeIdempotencyCloseReopen` | memory/journal race3 `061639-Y96wt6` |
| Work created during actual failed-owner drainage is aborted; retained conversation work after terminal owner runs normally | `TestTaskOwnershipLiveFailedOwnerAbortsNewWorkWhileOldChildDrains` | memory/journal race3 `061639-Y96wt6` |
| Active background owner stops root idle/abort traversal; direct owned-conversation abort selects ordinary child only | `TestTaskOwnershipActiveBackgroundOwnerStopsRootIdleButOwnedScopeRemainsBusy` | memory/journal race3 `061639-Y96wt6` |
| Ancestor cancellation preserves an already-marked background task's independent abort and subtree | `TestTaskOwnershipAncestorCascadeIncludesAlreadyMarkedBackgroundSubtree` | race3 `062131-flelAl` |

The initial background test incorrectly required the ancestor to join a marked background owner. Reference `scheduler.ts.#ownedLive` excludes background edges even when marked. Corrected assertions preserve that rule; the failed run `061854-tYN4vf` has analysed CPU/heap evidence.

## Current dispositions

| Area | Current contract and proof |
| --- | --- |
| M2 public lifetime and records | Atomic idle placement, queued/placed/done/unanswered projection, raw staged transaction settlement and shared publication waits are implemented. Typed forwarding and Close/poison cleanup pass focused race3; the final integrated gates include these tests. |
| Task/ownership reconciliation | Reservation-crash pre-dispatch abort, externally cancelled held Open and final marked-owned aborted settlement have direct tests. Blocked/too-old/migration-failed reads remain passive. Rejected-cascade tests retain wakes across append rejection. Old source-only labels identify native admission seams and historical proof gaps; they do not reverse later results. |
| M3 documents/observers/storage | Typed definition/history/delta/watch/view/graph, full adopted-table publication, rollback/reclaim and SQLite process-exit tests pass. Observers use detached bounded handles and exact serial committed frames. Scoped independent review accepted those observer semantics. |
| M3 compaction/settings | Compaction selects candidates before backwards budgeting, excludes the head, uses the preceding assistant and advances to the next assistant boundary. Zero/large budgets and tool-result boundaries pass race3 `073723-ZN0oFg`. Stream retry/compaction and queue/tool modes survive default resolution; pointer and top-level overrides keep precedence. Settings race3 `074512-g8jFX1` and independent re-review passed. |
| M4 environment/tools/subagents | Current cwd, replay safety, ownership/reopen, separate stream decoders, raw invalid-byte spills, callback/timeout/cancellation and truncation have concrete assertions. Shell/truncation race3 `073422-OE1vIJ` passed and scoped review accepted the corrected assertions. The edit fixture suite has 212 independent reference cases. |
| Registry/entries/tool errors/read | Builtin replacement rejection and name-based uninstall survive reopen; task-bound commits assign attribution. Typed entries retain empty arguments and protocol signatures. Thrown diagnostics reach `afterTool`; permitted hook replacement does not change the failed task outcome. Read covers whole-line bytes/lines, empty/zero/trailing-newline/UTF8 diagnostics and path variants. The pinned read source has no missing-file suggestion API. |

The source/suite tables have 60/42 rows and 105/109 named proof pointers. These counts establish a finite inventory and test declaration presence. They do not establish every inner reference subcase. The older 191-identity ledger retains historical descriptions; its current task-case disposition appendix points here. The former "18 partials" shorthand omitted the adopted-own-mark row: 19 task/recovery/ownership rows contain old partial text.

### Final runtime corrections

| Contract | Proof and disposition |
| --- | --- |
| Queued invocation capabilities recheck authority before read callbacks | `TestTaskRuntimeQueuedCapabilitySealAndCallerCancellation`: 21 operations, seal/caller cancellation, memory/journal. Typed current/historical reads now check on the Session line before migration. Late watch baseline hydration retains the reference acquire-then-stop exception. Race3 `083250-Yy8CDn`, extended race3 `092040-MOYMtk`. |
| Runtime hooks carry commit/owned-work authority; prompt/environment readers do not | HookAPI embeds TaskRuntime; InvocationReader exposes committed reads only. External public-import test and escaped capability assertions pass. |
| Hook-created owned work holds the old generation while a conversation-owned successor proceeds | `TestGenerationAfterToolsHookOwnsWorkAndLateEventsDoNotRepeatTurnEnd`, successor/terminate variants; early observers get one turn_end per generation, late observers get none. `TestGenerationHookHeldHandoffReopenDoesNotReplayHooksOrRequests` proves paused Open and final drain without hook/provider replay. Race3 `084812-gLBKYE`, `091057-ISL7Kg`. |
| Answer/model-error settlement occurs in the deciding commit | `TestGenerationResponseHookOwnedWorkSettlesAtDecisionForSuccessAndFailure`; held task still retains owned work and task documents, spend once. Rejection/uncertainty matrices preserve atomic prefixes; old pending-at-Hold expectations corrected. Race3 `085207-srAoHv`, `093612-Kdix36`. |
| Post-tools hooks receive committed assistant/result entry IDs | AfterToolEntries preserves the reference identity surface; existing AfterTools receipts remain a native compatibility callback. Results follow call order, include immediate unoffered receipts, omit faulted calls with no committed receipt and detach ID slices. `TestAgentSelectionAndHooksAreDetachedRequestLocalAndOrdered`; `TestToolControlsUnavailableOrThrowingCallCannotTerminateRound`; race3 `090948-yFZ3LZ`. |
| Bound background abort preserves option and lifetime | `TestInvocationConversationAbortBackgroundOptionsUsesBoundAuthority`; ordinary abort stops at background edges, opted-in abort selects them; ended and queued access rejects. Race3 `092040-MOYMtk`. |
| Post-tools reset completes generation without synthetic assistant | `TestResetIdleAndPostToolsStartsFollowUpInNewContext`; submission is unanswered/reset with no Answer, old generation completed result points to its original assistant. Hold union rejects wrong reset/entry/outcome/status. Race3 `092518-UjMp1Z`, `093405-xM4h5P`. |
| Builtin hooks use the phase registry, refreshing only next phase | `TestGenerationResponseHooksUsePhaseRegistryAndRefreshNextRequest`; registry captures copy maps/orders/installed values under lock. Blocked response uses old hook, next request uses replacement. Compaction/post-tools regressions pass race3 `093941-6gXynb`. |

Scoped code reviews accepted the document authority gate, held handoff/reset/record boundaries, bound background abort and captured phase registry. Final receipt review accepted local native runtime completion within the documented Go boundaries; it was not an independent runtime rerun. Proposed requirements to preserve diagnostics after allowed afterTool replacement or require unanimous addTools/handoff were rejected against exact pinned source. Only termination requires unanimity.

### Release and verification limits

No demonstrated runtime difference remains in the reconciled obligations. Native contexts, immutable values, strict schemas, quotas and platform adapters have the explicit dispositions below. Every JavaScript promise/getter/proxy scheduling placement and nested reference subcase was not independently replayed; mapped declaration counts are not blanket independent acceptance.

Rui authorised a scoped local commit on 6 October 2026. Independent clean-clone and hosted release acceptance have not run. Push, hosted CI and publication need separate authorisation; they are not outstanding runtime implementation work.

## Native equivalents

- Explicit `context.Context`, detached JSON values and transaction handles replace JavaScript promises, proxies and object identity. Admission/ownership checks remain mandatory.
- Native journal framing and native SQLite schema preserve logical transactions/history/reopen; official JSONL/SQLite byte/file interoperability is outside the port's contract.
- Observers deliver bounded, serial committed frames off-line. Chord mounts and shared JavaScript roots become disposable detached reads/subscriptions. Resource quotas are explicit native limits.
- Native task record replacement is authority-fenced. Runtime state changes use `TaskRuntime.Commit`; application code cannot rewrite an active invocation's immutable identity or bypass owned-work drainage.
- Registry snapshots do not execute caller code while capturing registrations. Tests can witness Close at the phase boundary without reproducing a JavaScript getter that triggers Close inside snapshot capture.
- Rejected scheduler writes retry on a genuine wake instead of spinning on a permanently rejecting backend. The wake arriving during a rejected append must still be retained.
- Go test helpers/conformance tests and pprof replace the TypeScript testing/benchmark facade; a second testing framework adds no runtime capability.
- Unix process groups, filesystem capabilities and bounded output implement the local environment. Non-Unix local process execution is unsupported; injected remote environments are portable.

These equivalents do not justify dropping externally visible settlement, ownership, replay, historical or event-ordering behaviour.

## Profiling

Final durable race shuffle `094128-MWnA0b`: cumulative CPU was 44.67% in Session commit, 36.76% in bounded encoding and 31.49% in reference preparation. Allocation totals were 7551.28MB and 122,385,872 objects; commit accounted for 77.93% of bytes and 81.66% of objects. These cumulative stacks overlap. The stress/recovery suite is not a production throughput benchmark.

The remaining cost is strict validation, immutable candidate construction and detached JSON. Shared settlement logic removed duplicate paths and timer polling; additional allocation changes would need equivalent-workload comparison while preserving validation/security. No matching-workload speedup is established. CPU, alloc_space and alloc_objects were analysed, then raw captures and matching binaries were deleted. Completed frozen fixtures, logs and rebuildable caches are disposed at the final safe boundary; concise receipts remain.

Preserve published refs/assets, retained stash and unrelated edits. Only the scoped local commit is authorised; push, hosted CI and release publication require separate authorisation.

## Admission integration — 6 October 2026

Idle-input atomic placement passed focused race3 063440-4Sma8P. The integrated minimum run 063539-bnBZw5 failed: compaction double-counted the placed input, and older fixtures expected no admitted entry before preparation. Compaction now estimates a queued input only when InputEntry is absent; fixtures retain one admitted user entry while asserting zero provider effects/spend. A retry exposed an empty transaction preview passed to storage validation; Tx.current now returns its committed baseline for zero writes. Focused regressions race3 064203-LrEmhT pass. Concise failure summaries for 063821-OFyz6c and diagnostic 064016-nsy8xD remain; raw profiles were analysed and disposed; 064000-wh5GmT matched no tests and the wrapper rejected it. Raw captures were analysed/disposed.

Public record projection retains native persisted envelopes for compatibility; callers use SubmissionRecord or Settlement.Record. Task handoff carries InputEntry without repeating user placement. The final frozen verification cycle above passed with these changes.
