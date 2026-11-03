# Tool-result hooks at the restart limit

Runtime `9d4af1afcd359b0026a44f5d28de2f2f4e3469da` runs `afterTool` on a builtin tool's cutoff result exactly at `restartLimit`, then skips it past the limit. A hook can change the visible result without changing the failed task ending. Base: `e6fc4af56e7b3f6a919167aaafbd89172425d182`. Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Source and baseline

The pinned source already has the exact three-opening scenario at `packages/durable/test/harness-tool-hooks.test.ts:584`:

1. A tool publishes output/details and blocks; the host dies.
2. At limit1 its `afterTool` receives `restart_limit` and blocks; the host dies inside that hook.
3. Past the limit the result settles without another hook/tool execution. Input is done; diagnostics retain `restart_limit`; the task fails with that reason.

`TestMainCutoffHooksAtLimitNotPastLimit` translates these checks through real memory, journal and pure-Go modernc SQLite. It uses the existing failed-Session/cancel/join/storage-reopen adapter without writing checkpoints or restart counts. The Go hook observes cancellation so the old host can join; the source helper leaves its Promise hanging after kill. Native tool registration, model response helper and receipt-role adapters remain explicit. The fixture preserves the source's assertions, including one hook and one tool execution after the third opening.

Two labelled source-derived functions follow `tool.ts:254–327/758–791`: `NormalizeReplacement` checks hook replacement, a newly installed tool's current four-byte output limit, independent visible `isError=false` and failed ending, details/diagnostics, stored usage, and suppression of failed-task terminate control. `ThrowReportsAndRetainsSlot` checks fallback output, the original error cause and extension/task attribution. The model receipt has no structured-output field; the supplied hook value's absence cannot independently establish the internal drop or nested-call semantics.

All three failed unchanged production: **3 top-level failures,9 backend failures, no skips,10.017s**. At-limit hooks were never called. Initial guessed limit/receipt fields caused build errors and receive no runtime-failure credit. Later diagnostic-block assertions were corrected to inspect the native output block separately from its rendered diagnostics; no production change addressed those adapter errors. The current-limit/stored-usage and attribution checks were strengthened during implementation.

## Implementation

`tool_cutoff.go` rebuilds the slot result before invoking selected hooks. It detaches hook inputs/replacements, resolves current output limits at the salvageable boundary, reports hook errors against their extension and retains the built result on error. Past the boundary it skips hook/tool resolution. No progress commit occurs while the cutoff hook hangs, so another host loss increments the real restart count.

The final result bypasses the old error-code overwrite of output, diagnostics and visible error status. Normal output bounds still apply; usage is recorded. The failed task ignores control/application commits and keeps its original cutoff message/reason. Details fallbacks run before the hook, so a replacement does not silently recover omitted slot details. Harness structured output is dropped before settlement.

A private checkpoint `cutoffMessage` witnesses the failed ending independently of the hook-modified receipt. Validation still requires the cutoff reason, matching ending message and final receipt error flag. Older native receipts without that field retain their diagnostic-message check. `TestToolCutoff150NormalizedEndingWitness` rejects mismatched message/reason/status/receipt-flag cases; the older receipt-envelope guards pass unchanged. Task tags, versions, driver, dependencies and QuickJS/wazero assets are unchanged.

## Focused checks

Commands source `scripts/project-env.sh` and export `CGO_ENABLED=0 GOMAXPROCS=4 GOTOOLCHAIN=go1.27.3`. Owned run paths use `runs/cutoff-tool-hooks/20261103` below the project root. Logical and physical compiler overlays exclude only the protected continuation prototype.

* Three new functions, existing safe-tool cutoff and legacy envelope guards:15 repeated top-level passes/4.783s after the first fix.
* Final **13 functions/count3/shuffle42:39 passes,0 failures/skips,13.202s**. This includes the new cases and witness guard plus affected normal output, hook invalid-result, diagnostics, application-mutation and thrown-error controls. Name/count checks confirm the complete selection.
* After promotion to ordinary `harness_cutoff_hooks_main_test.go`, the three functions plus witness guard pass once more:4 passes/1.188s.
* Durable vet and Staticcheck **v0.8.2 with tests** pass. The static copy excludes the prototype; all1459 Go source files match the validated copy. Formatting/diff and prototype comparisons pass.
* Five effective logical+physical overlays each fail all three backend leaves, with no build error, skip or global timeout: skip hooks at the limit, repeat them past it, force the visible error flag, ignore current output limits, swallow extension-error reports. Mutation data is removed after diagnosis.

The completed recovery baseline and previous27-function gate were not repeated. No full suite, profiling, CGO/race run, TypeScript/Vitest execution, hosted CI, release or asset publication ran. SQLite and QuickJS-in-wazero remain unchanged. Two main-crosswalk evidence cells change. Scoped review updates the restart-failure evidence and adds the hook file as `pending-review`; all existing dispositions/flags and the362/210 corpora/428-path inventory are unchanged.

## Open contracts

Whole-file tool-hooks and restart-failure review stays `pending-review`. Interrupted/aborted/unstarted/abandoned harness results, nested cutoff results, retained-output identity/truncation details, multi-hook/error ordering, large-image integration, and complete DTO/capability fidelity still need pinned oracles. Native string/value comparisons cannot establish the source's reference-identity output-retention condition. Model receipts' extra error-code/rendered-diagnostic shape remains an adapter.

Registry-snapshot timing and generation partial/deferred salvage across all cutoff phases remain the next coupled work. Source429 orderly close in drain and the other after-run/history/orphan/fork/ownership assertions are still open. Full v1.5.0 alignment and broader release acceptance are unverified. The prototype, source mirror, installed dependencies/toolchains and immutable nativev1.4 assets are preserved. After analysis,27MB of completed raw extracts/static copies/mutation/log scratch and1.1GB of Go build cache were removed with no active Go/compiler/static jobs.
