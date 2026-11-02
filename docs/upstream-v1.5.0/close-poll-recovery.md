## Clean-close and deferred-poll recovery

The held-generation clean-close and deferred-poll crash scenarios pass unchanged native runtime `2387014a690da8110d723067d99dfe4271ff0454` (documentation baseline `911eca61391c9955c78209514eb589f98b89c16f`). Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/test/harness-deferred-progress.test.ts`.

### Source assertions and native paths

| Source | Native test in `durable/harness_close_recovery_main_test.go` | Checks |
| --- | --- | --- |
| 210-226, clean close | `TestMainHarnessCleanCloseStoresHeldGeneration` | Infinity persistence window; observed partial longer than40 characters; healthy close stores the last memory partial; reopened submission finishes; first assistant entry is exactly that partial with `aborted` stop reason. |
| 228-248, deferred polling after crash | `TestMainHarnessDeferredPollAfterCrash` | Stored live deferred handle; crash and reopen; clock1000 ->61000; resumed submission is done with `deferred answer`; exactly one deferred fetch. |

The streaming provider is hand-stepped, not wall-clock paced. Watches, Harness shutdown, task recovery, durable handles and provider dispatch all use production paths. Memory is quiescent before clean close; the provider cannot append more text during cancellation. Crash uses the existing failed-Session adapter, joins the old host and reopens actual storage, rather than calling healthy close to flush it. All scenarios cover memory, journal and SQLite (SQLite requires CGO).

Additional native controls require the partial to be unstored before close, the generation to remain pending with zero restarts after close/reopen, no request redispatch or early poll, and request attempt1 after recovery. The remaining final answer and cleared live presentation are also checked after clean-close recovery. These controls supplement, rather than replace, the source assertions.

Existing `TestClose140RequeueDoesNotConsumeRestartBudget` covers repeated generic task closes; it does not bind the budget invariant to a held generation partial. Existing `TestDeferredGenerationPollCheckpointReopenNoRedispatch` covers healthy close, not the source's crash. Those earlier tests were reconciled, not rerun. The new tests reuse the existing controlled clock, real watch and reopen helpers without changing them. No production code changed.

### Focused results

Go1.27.3, `GOMAXPROCS=4`; protected prototype excluded by logical and canonical Go overlay paths. Selection: `^TestMainHarness(CleanCloseStoresHeldGeneration|DeferredPollAfterCrash)$`.

| Check | Result |
| --- | --- |
| Unchanged runtime, CGO1, count1 | Pass,0.349s |
| Normal, count5/shuffle42, CGO1 / CGO0 | Pass,1.737s /0.984s |
| Race, count3/shuffle7, CGO1 | Pass,6.759s |
| Durable vet / Staticcheck v0.8.2, both CGO modes | Pass |
| Module verification / formatting / diff / protected-prototype comparison | Pass |

Staticcheck uses an exact temporary package copy excluding the protected prototype; installed `/workspace/bin/staticcheck` is older and was not credited. Three active overlay faults each fail all three backend leaves: omit held-document close flush (stored text and recovered aborted answer lost), omit running-task close requeue (restart charged), duplicate deferred fetch (count2 instead of1). An outdated requeue mutation anchor was rejected during setup and corrected before execution. These are deliberate faults, not failing unchanged-runtime baselines. No TypeScript/Vitest execution, all-eleven-body parity, full-suite or profiling result is claimed.

### Corrected next scope

The earlier plan and generation receipt incorrectly called source228 an abort-during-flush scenario. It is deferred-response crash recovery. No such abort assertion was found in this file, and these results grant no abort-during-flush credit. Abort ordering remains part of the broader contract review and needs an exact source anchor before translation.

The three source302-394 [tool-recovery scenarios](tool-recovery.md) now pass, including the repaired restart accounting. The subsequent [tool-document integration](tool-deferral.md) adds its separate failing baseline and deferred presentation while preserving those recovery assertions. The subsequent [source398/source430 translations](summary-dependent-recovery.md) pass unchanged production, including a real third-party Runtime.Commit and atomic storage-batch assertion. Broader public shared values, observers, AI/codemode/schema/support and catalogue/package correspondence remain open. Full v1.5.0 alignment is unverified.

Owned source copies, test overlays, copied packages, logs and completed build cache are disposable after use. Preserve pinned upstream source needed by continuing alignment, installed dependencies/toolchains, the protected prototype and native v1.4.0 refs/assets. No dependency, storage-format, release, publication or CI change is included.
