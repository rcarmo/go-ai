## Tool progress and crash recovery

Tool presentation no longer resets the interruption count. Native runtime `083746d996109142152e85427c5143f694a5e1da` fixes the failure demonstrated at `3d8fbc2d24e179fbace2fcf4be8a23c3e0c1f0c0`, against official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`.

### Translated recovery scenarios

All source anchors below refer to `packages/durable/test/harness-deferred-progress.test.ts`; native tests are in `durable/harness_tool_recovery_main_test.go`.

| Source | Native test | Preserved assertions | Unchanged baseline |
| --- | --- | --- | --- |
| 302-339 | `TestMainToolProgressDoesNotResetCrashRestarts` | Each execution stores output of its own before crashing; reopened tool restart count equals1, then2. | Fails all three backends after the second crash:1 instead of2. |
| 341-364 | `TestMainUnsafeToolCrashUsesStoredOutput` | Stored output longer than20 characters is a prefix of emitted output; crash/reopen completes submission; unsafe execution count1; exact stored output plus the source interruption diagnostic. | Passes. |
| 366-394 | `TestMainSafeToolReplayClearsOutputThroughSecondCrash` | Stored progress before first crash; silent second execution has durably removed it; second crash retains removal; final submission done, run count3 and exact `rerun 3\n` result. | Passes. |

The combined unchanged-runtime run took1.326s with CGO1 on memory, journal and SQLite. No implementation change was needed for unsafe output or repeated safe replay. The earlier `TestSafeToolReplayClearsInterruptedOutputAndDetails` uses healthy close; it does not establish the source's failed-Session/repeated-crash boundary.

The source's20ms output loop is replaced by hand-stepped output commands, controlled clock increments and actual conversation-watch acknowledgements. No `Details` or extra checkpoint commit is inserted to acknowledge output. Provider calls, tool dispatch, publication, storage and reopen use production paths. Crash uses the existing failed-Session adapter and joins the host before reopening storage. Native stored `MessageReceipt` entries supply the role/content assertions corresponding to the source's model-message records. The stream is quiescent at crash, so its pre-crash emitted-prefix check plus post-crash stored-value check preserves the source prefix assertion. The unexpected unsafe-rerun fixture returns as the source helper does, exposing wrong counts/results rather than relying on timeout.

### Cause and fix

`Tx.stage` resets an invocation's restart count when `taskMadeProgress` observes a changed checkpoint. Builtin tool checkpoints contain both execution state and presentation. Updating output, or clearing it at safe replay, therefore granted another restart budget.

`taskMadeProgress` now excludes only known builtin-tool presentation fields: output/retention counters, details, diagnostics and duration. Both current raw builtin records and tagged builtin records are recognised. Status changes, unknown fields and execution fields such as arguments, started/abort/error/control/result remain significant. Other builtins and public native-task checkpoints keep their previous comparison rules, including numeric JSON equality. Checkpoints are neither copied nor mutated by the new comparison. Storage format and persistence policy are unchanged.

`TestMainToolPresentationIsNotTaskStateProgress` adds field/removal, combined, state/unknown-field, status, numeric-equality, raw/tagged builtin, non-tool and native-task controls. Initial unit fixture tag names and nested-map representation were corrected before crediting the baseline. The first tagged-only implementation still failed all three recovery leaves because current tool records have no execution envelope; raw builtin recognition and its control were added. No upstream assertion was weakened.

### Focused checks

Go1.27.3, `GOMAXPROCS=4`. The selection contains the four new functions, all five `TestRestartLimit140*` tests, the existing safe-replay/details test and the existing progress/details/sealed-write test. Protected prototype excluded at both logical and canonical paths; Staticcheck uses a physical temporary package copy excluding it.

| Check | Result |
| --- | --- |
| Focused normal, count3/shuffle42, CGO1 / CGO0 | Pass,7.957s /6.438s |
| Focused race, count3/shuffle7, CGO1 | Pass,41.381s |
| Final safe-replay check, count3, CGO1 / CGO0 / race | Pass,1.599s /1.049s /11.718s |
| Durable vet / Staticcheck v0.8.2, both CGO modes | Pass |
| Module verification / formatting / diff / protected prototype | Pass |

Staticcheck found an unused second-run binding; removing the assignment did not change assertions or runtime code. The final safe-replay checks followed that cleanup. Four active overlay faults are rejected: old progress rule17 leaves (three backend failures plus14 field controls), unsafe replay3, retained safe output3, ignoring all builtin state7. These are deliberately failing controls, not current runtime failures. The original corrected unit baseline had13 presentation failures; final raw-builtin coverage adds the fourteenth under the old-rule fault.

### Remaining contracts

This checkpoint tested recovery while progress still used immediate task-checkpoint writes. The subsequent [tool-document integration](tool-deferral.md) establishes the separate memory-before-storage baseline and implements deferred presentation, retaining these recovery assertions. No persistence-policy divergence is accepted.

Source398 summarisation crash/restart and source430 third-party task dependent reads remain next in this file. Broader shared/public identity, synchronous observers/reporting, AI/codemode/schema/support and catalogue/package correspondence remain open. No TypeScript/Vitest execution, all-eleven-body parity, full-suite, profiling, CI or release result is claimed. Full v1.5.0 alignment is unverified. Disposable source copies, overlays, packages and logs must be removed after use; installed dependencies/toolchains, continuing pinned source, protected prototype and native v1.4.0 assets remain protected.
