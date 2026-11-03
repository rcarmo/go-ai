## Answered runs and after-run work

`GenerationHooks.AfterRun` now receives committed run history and can request ordinary owned tasks. Inputs settle with the answer; the live run stays busy through finish/drain, then ends and places queued boundaries. Runtime: `e716c0b6025819c5d5b19c5dff8f46e2ab18e2b8`. Target: official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`; baseline: `5a0863808249fbc50a844b010f6303b8f89ce3ca`/runtime `b6b8d41`.

The [historical baseline](after-run-baseline.md) keeps its six failures/three passing controls and full 29-declaration source index. Selected lifecycle assertions pass now. Full after-run convergence is unverified.

### API and stored phases

`RunSummary` contains reason, first entry, answer and last entry. `AfterRunResult.Tasks` holds `RunEndTask{Task, Input}` requests. Each token supplies a kind; the phase-local installed definition supplies its version, initial state and execution. Builtin kinds are rejected. Native mapped builtin names are checked individually so application definitions under `task.pi.*` remain valid.

The answer's commit appends the answer, stores model usage, settles inputs, clears live generation/tool presentation and retains the live run with `afterRun={}`. A detached summary is stored in a nonterminal `finish` checkpoint. No terminal hold is decided yet, so the generation can still own children.

Finish reselects hooks under its current phase agent. It validates requests off the Session line, initialises each valid request once, reports rejected requests together against their extension and keeps valid siblings. One commit creates the children and records their IDs in live state and the `drain` checkpoint. Private `Tx.createTask` shares ordinary ownership, JSON, trace, minting and staging fences; its prepared-input branch avoids repeating the off-line initializer. Public `Tx.CreateTask` keeps its original behaviour.

Drain waits for the requested tasks' terminal states and host returns. It ends the run with the original answer, applies queued boundaries and clears memos. Other ordinary owned work can still hold the generation's terminal receipt under the existing task policy. A successful terminate round supplies the tool-calling assistant as answer and its newest result or handoff reset as last. Stop after an answer completes that run without failure. Close during finish preserves the committed answer and permits re-asking on reopen.

Private generation checkpoints gain `afterRun` and `afterRunTasks`; existing task tags and versions are unchanged. The typed API replaces the declaration-only adapter, which was removed. `durable/harness_after_run_main_test.go` contains the promoted assertions and new controls.

### Assertion map

All function names below start with `TestMainAfterRun`. Source lines refer to [the pinned test file][source]. Integration functions use memory, journal and real modernc SQLite with CGO disabled.

| Function suffix | Source and scope |
| --- | --- |
| `AnswerTasksHoldRun` | 181–219: input done at answer; one ordinary owned child; empty live inputs; busy wait; exact first/answer/last; cleared run/failure; child result last; completed generation retains answer. Native extra committed-entry read and two-request controls. |
| `NoHookImmediateEnd`, `EmptyHookEnd` | 251–275: no-hook answer ends in its commit; empty hook is asked once and creates no tasks. Original setups are independent functions. |
| `RejectedRequestsKeepValidSibling` | 538–615 selected representable cases: unknown/builtin/non-JSON/throwing/bad-start/valid requests; ordered aggregate diagnostics; one report per failing hook; valid sibling runs; throwing hook retains its cause. The source's third, malformed-list extension cannot inhabit Go's typed task slice and has no runtime assertion here. |
| `InstalledDefinitionInitialOnce` | 617–653: installed version2, one terminal child, installed initializer called once, stale initializer never called. A re-entrant Snapshot verifies off-line initialisation. |
| `TerminateSummary` | 221–247: finish and handoff preserve summary identities, one requested child and normal run end. Native one-request/no-redispatch control. |
| `StopAnswered` | 478–514: real native conversation abort during hook or child; answered input stays done and generation completes without runFailure. |
| `QueuedBoundary` | Selected 278–350 assertions plus native controls: held tasks queue two steers/one follow-up; reject input/continue while busy; immediate note; writes precede user inputs; exact resulting order and three hooks/requests. The model-contributing write uses external admission; the source child's same-transaction submit and per-input `whenIdle=reject` are unported. |
| `CloseInFinishReasks` | Native orderly-close portion of1140–1182: same summary asked twice, one child, one model request and completed reopened generation. Source's initial crash, third opening and40-character held-progress stream are untested here. |
| `BuiltinNames` | Native namespace guard: source `pi.*`, mapped builtins and unrelated application names. |
| `HookReadsCommittedEntryBoundaries` | Native control for the committed-entry reads the source fixtures rely on: the hook reads `first`, `answer` and `last` through public `Entry`, and their roles stay user/assistant. Native `HookAPI` has no history scan, so source `allEntries` ordering and post-compaction history are not covered here. |
| `HookHistoryKeepsRunStartHiddenByMidRunCompaction` | **Known-failing** source 694 fixture, pending in `durable/testdata/upstream-main/pending/harness_after_run_history_test.go`: it asserts the hook can still read the committed run start after a mid-run compaction hid it from the active context, and that the summary sits inside the run's committed range. It fails earlier — the hook is never invoked for a run whose answer follows a model tool round, while the pinned contract evaluates `afterRun` hooks on the answering path (pinned generation.ts864). |
| `ContinueAfterFailureSummaryUsesFailedFirst` | Selected portion of 857: after an unanswered run the continue summary keeps that run's first entry, with answer and last from the resumed answer. The native cause is a provider error rather than source `turn_limit`; the failed run itself is not asked. |
| `MissingDefinitionStopOrphans` | 785: reopen without the extension keeps the answered run busy, the real child inspects `blocked`, public `WaitForIdle` stays unsettled, and abort plus idle settlement orphans the child with outcome `orphaned` while the committed answer stays done. |
| `HooksDeselectedBeforeRecoveryFinish` | 882: the hook is asked once, finish with no installed definition ends the run without re-ask or dispatch, and live state is cleared. |
| `ForkStartsIdleWithoutParentQueue` | 903: forking at the committed answer while after-run is gated starts idle, copies neither the active run nor the parent inbox, and stays ownerless. |

Adapters remain explicit: Go receipts plus message roles replace source entry tokens; native registered task tokens differ; the terminate fixture installs its used tool and supplies native result content. Selected identity/lifecycle assertions receive credit; complete source fixtures/bodies do not. The strict-JSON rejection retains native `StorageRejected` text (`durable: rejected: unsupported JSON value`); its JavaScript function-specific wording is unported. HookAPI retains the existing native invocation capabilities and fences; full source capability-surface equivalence is unverified.

### Baseline and checks

Before runtime changes, the new validation/installed-definition functions failed **six backend leaves in0.246s** under the declaration-only adapter: no valid task/reports and zero initializer calls. The previously saved baseline was not repeated. The first implementation run passed four functions; validation failed three leaves because the fixture had guessed the wrong existing `StorageRejected.Error()` prefix. Correcting that native diagnostic adapter passed all five functions in1.050s.

Terminate, Stop and orderly-close controls passed1.099s. Queue-control drafts used an undefined text helper, omitted the required native entry object and expected a different queue grouping with only one steer. These fixture errors receive no runtime-failure credit. The corrected two-steer source ordering passes0.605s without changing runtime queue policy.

All final commands source `scripts/project-env.sh`, export `CGO_ENABLED=0 GOMAXPROCS=4 GOTOOLCHAIN=go1.27.3` and exclude the protected prototype:

* Nine new plus twelve affected existing functions, `-count=3 -shuffle=42`: **63 top-level passes, zero failures/skips,14.811s**.
* Source-defined diagnostic error type preserves capitalisation after Staticcheck's style diagnostic; only the validation function was rerun, three passes/.511s. No assertion changed.
* Final mapped-builtin guard and validation functions: six passes/.495s. The added namespace unit function makes ten new functions in the current file.
* Fresh durable vet and Staticcheck **v0.8.2 with tests** pass. Staticcheck uses a physical source copy excluding the protected prototype; no suppression was added. Formatting/diff, validated-copy and protected-prototype comparisons pass. Nativev1.4 still peels to `ee3076b3659697f1de01b87da964396e069a4389`.

The twelve affected existing functions were constructor/input ownership, strict codec, public task ownership/sealing, strict budgets, builtin abort-mark receipts, ordered detached hooks, on-yield/queued-user precedence, held-hook reopen, unanimous tool controls, both late-owned holds and after-response-owned boundary admission. These checks exercise changed paths; completed trace, continuation and storage-conformance gates were not repeated wholesale.

Post-promotion focused run (main worktree, project run directory, protected prototype excluded only in the compiler input): `MissingDefinitionStopOrphans`, `HooksDeselectedBeforeRecoveryFinish`, `ForkStartsIdleWithoutParentQueue`, `HookReadsCommittedEntryBoundaries` and `ContinueAfterFailureSummaryUsesFailedFirst`, `-count=1` — **five passes, zero failures/skips, 0.510s**. After the failing 694 fixture was moved out of the package file into pending, the same five functions were rerun: **five passes, 0.491s**, and `go vet` passes with the pending oracle compiled through the overlay. The pending oracle itself: **one failure**, 0.143s, at the first assertion (hook not invoked for the tool-round run); no assertion was weakened and no skip was added. Staticcheck v0.8.2 with tests passes on a physical source copy excluding the protected prototype; gofumpt is unavailable on this host, and `gofmt -l` reports no file needing formatting.

Seven effective logical+canonical overlays each reject **three failing backend leaves**, with no build failure, skip or global test timeout:

| Fault | Oracle |
| --- | --- |
| Omit answer hold | Answer/task lifecycle |
| Use stale request definition | Installed version/initializer |
| Initialise twice off-line | Initializer count |
| Discard valid siblings on rejection | Valid task/report aggregation |
| Create conversation-owned child | Ordinary generation ownership |
| Retain answered live inputs | Answer settlement/live state |
| Replace first with answer | Exact run summary |

After analysis, 28MB of owned source extracts, overlays, fault logs and static-source copies plus1.6GB completed Go build/test cache were removed with no active Go/compiler/staticcheck jobs. Installed dependencies/toolchains, the pinned source mirror and protected prototype were preserved; its comparison copy is retained only through the push check.

### Open source work

The [cutoff checkpoint](restart-cutoffs.md) now passes selected source429 crash/no-reask and757/955/983/1019 recovery assertions, with ordinary faulting, answered queue preservation and normal lowered-limit controls. Source429 orderly close in drain, foreign ownership and the remaining29-file assertions need source reconciliation. New confirmed divergence: native does not invoke `afterRun` for a run whose answer follows a model tool round (source 694 requires one hook call there); the tool-round handoff carries no afterRun state into the successor checkpoint and the post-tool answer commit does not evaluate the hook selection, so no assertion of the history contract can pass until that path is reconciled. The fixture stays pending with its retained assertions. The source 957 case that gives a run without a recorded first entry its answer as first is not portable: it deletes the live run document's `first` inside a hook, and native public `Tx` exposes only `CreateTask`, so the runtime fallback at `durable/after_run.go:129` has no public-API path to be exercised without expanding the API; it stays unverified rather than tested through storage manipulation. Post-compaction history ordering is also unportable: the source uses `api.entries(minEntryId..maxEntryId)`, which the native hook API does not expose. The native orderly-close control does not establish held-progress equivalence. Coupled builtin tool result-hook/phase assertions and complete DTO fidelity also need review; full after-run convergence is unverified.

Whole-file disposition stays `pending-review`; only its current evidence cell changes in the362-row crosswalk. Dependencies, SQLite driver, QuickJS/wazero, nativev1.4 assets and public Commit are unchanged. No full suite, race, profiling, TypeScript/Vitest execution, hosted CI or release publication was run. Full v1.5.0 alignment is unverified.

[source]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-after-run.test.ts
