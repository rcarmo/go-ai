# Tool presentation persistence

Runtime `decbd9d4419d0580c6b84e6832b07d7893573444` publishes tool progress through the runtime-owned document lane. Rollback: `bcb2dee79e851f62d0698b6beb44328a6b1ce1d8`. The target is official upstream v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Source and baseline

`packages/durable/src/harness/tool.ts:609-662` publishes output, details, diagnostics and dropped counts into the matching live tool slot using `PROGRESS_COMMIT`. Its `fromSlot` helper at1004-1020 reconstructs interrupted output from that slot and omits empty text. The three recovery scenarios at302-394 in `harness-deferred-progress.test.ts` were already translated in [tool-recovery.md](tool-recovery.md).

Three new source-derived functions in `durable/harness_tool_deferral_main_test.go` check:

- memory visibility before storage, unchanged task checkpoints, then timer, dependent-read or clean-close persistence;
- a crash before the first stored output: unsafe execution runs once and the recovered receipt contains only the interruption diagnostic;
- ordered memory output events followed by a dependent-entry callback barrier, without replaying output at storage.

These are source-derived scenarios with native watch/clock/failed-Session adapters. They do not execute complete additional upstream test bodies or TypeScript. The corrected unchanged-production overlay fails all15 backend leaves in1.181s. Both logical and canonical source paths are overlaid; the protected prototype stays in place and is excluded. Entry-ID reservation and compile errors in initial fixture attempts are excluded from that baseline. The first-store expectation is unchanged: the implementation also needed to remove a fabricated empty text block before the diagnostic.

## Runtime changes

`ToolAPI` keeps its bounded local snapshot for finalisation and publishes presentation fields to `pi.live.tools` through `TaskRuntime.commitProgress`. Each mark leaves task invocation state unchanged. Public `Runtime.Commit` remains immediate.

Slot reads require the conversation document, generation owner, task and call ID to match. IDs must be integral and representable without truncation. Retired, foreign and completed slots cannot replace the checkpoint fallback. Legacy checkpoint progress remains readable.

Task-table updates rebuild tool slots from logical memory. A sibling's completion therefore preserves held output. Safe replay clears the live slot and legacy checkpoint fields in its immediate start transaction before executing again. Unsafe recovery reconstructs only stored progress. Empty interrupted output yields diagnostics without a synthetic empty contribution.

Agent events compare logical slot values; task writes retain start/update/end handling and existing ordering. Storage flushes of an already published value produce no second update.

Two additional functions in `durable/tool_presentation_main_test.go` cover legacy/owner/call/task/scope/retirement/oversized-ID fences and a real two-tool run whose first completion must preserve its sibling's held output. These are native implementation controls.

## Existing fixtures

The firehose/memo and noncooperative-abort fixtures now obtain progress from the live document while retaining memo, abort, truncation, escaped-API and terminal-receipt assertions. The adapted firehose fixture passes unchanged production in0.464s.

Two older generation fixtures already failed before this tool change: one waited for a checkpoint `partial` append indefinitely; the other lacked the live document and waited for `checkpoint.Partial`. They now use the same stored prefix and detached-value assertions through the live document, with proper conversation/builtin setup. Unchanged-production controls reproduce the original timeout/failure; all three adapted M1c fixtures pass that production overlay in1.255s. No generation runtime change was needed.

## Focused validation

Go1.27.3, `GOMAXPROCS=4`, `scripts/project-env.sh`, protected-prototype overlay:

| Check | Result |
| --- | --- |
| Tool/replay/restart checks, count3/shuffle42, CGO1 / CGO0 | Pass,49.365s /51.433s |
| Same checks, race/count3/shuffle7, CGO1 | Pass,321.509s |
| Lifecycle/turn/M1c/generation-event checks, count3/shuffle42, CGO1 / CGO0 | Pass,14.184s /14.075s |
| Same event checks, race/count3/shuffle7, CGO1 | Pass,76.431s |
| Durable vet / Staticcheck v0.8.2, both CGO modes | Pass |
| Module verification / formatting / diff / prototype content / v1.4.0 peeled ref | Pass |

The tool pattern is `^(TestTool|TestSafeToolReplay|TestMainTool|TestMainUnsafeTool|TestMainSafeTool|TestRestartLimit140)`. The event pattern is `^(TestHarnessLifecycleEventsAndReentrantListener|TestHarnessTurnEventOrderingAndToolSnapshots|TestM1c.*|TestMainGenerationAgentEventsBeforeStorage)$`. Staticcheck uses a physical source copy excluding the prototype, including all new files. The first broad tool race attempt exhausted its300s process timeout while still executing valid work; the completed600s-bound run above supplies the race result. An early event run exposed the obsolete generation fixtures described above.

Seven active faults are rejected: immediate progress storage15 backend leaves; missing generation-owner fence1 unit leaf; missing safe-replay document clearing3; missing document-progress events3; discarded sibling presentation3; missing stored-progress reconstruction3; retained empty interrupted block3. Tests retain their original output, error and count assertions.

The canonical release review and generated362-row test crosswalk change one evidence cell. Dispositions, change markers, the210-row dependency queue and historical review files are unchanged. Disposable source copies, overlays, physical packages and logs are removed after analysis; installed dependencies/toolchains, the continuing pinned source cache and protected prototype are preserved.

## Remaining work

Source398 summarisation crash/restart and source430 actual third-party task dependent live-document reads need translation. The dependent read here is a public Harness commit, so it does not close source430. Full eleven-body coverage, shared public values, tracker/WeakRef, synchronous observer/reporting semantics, cross-package/schema/support/catalogue correspondence and authorised acceptance CPU/allocation profiling are unverified. No full-suite, profiling, CI, release or full-alignment result is included.
