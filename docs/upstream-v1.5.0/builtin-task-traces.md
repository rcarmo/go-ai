## Builtin task trace ancestry

Current follow-up: compaction now uses source pi.compaction in persisted tasks and span names. Existing background ancestry and metadata assertions pass unchanged apart from the kind literal. The receipt below describes an earlier runtime and its former task.pi.compaction token; [the current name/namespace receipt](../../notes/canonical-compaction-kind.md) supersedes that name difference.

Parallel and sequential model-tool tasks now persist their creating phase's carrier. An active phase stopped by Close now reports `outcome=aborted` and explicitly sets OK status. Adopted terminal, waiting and sleeping decisions keep their outcomes. Background-compaction metadata, nested-task carriers and restart parenting already passed unchanged runtime.

Target: official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native runtime: `b6b8d4185a7be3bc9dc1a84811af7925cf5c3791`; baseline: `e46eee70148ecce81e0f19c9708ede09c744b68d` (runtime `2fd88f2`). The earlier [task-trace checkpoint][prior] keeps its original results.

### Source assertions and native controls

All four functions live in `durable/builtin_trace_main_test.go`.

| Function suffix (`TestMainBuiltinTrace…`) | Source and checks |
| --- | --- |
| `BackgroundCompaction` | [Telemetry test 448–476][background]: exact 1000-token model window, 100 max tokens, enabled compaction/reserve300/keep150/background1000, 400-character x/y/z history and declining hook. The compaction phase's parent equals the actual generation prepare context; persisted background flag and carrier match; hook/request counts are both one. |
| `ToolMetadata` | Source-derived [generation tool creation][generation] and [nested tool creation][nested], inheriting the [transaction's creating context][transaction]. Two actual model-tool calls, each making a nested call, in parallel and sequential modes. Persisted carriers equal the live creating parents; owner IDs, two tool/two nested tasks and two requests match. Initial tools belong to request; the later sequential tool belongs to tools. |
| `ActiveReopenParent` | [Telemetry test 478–544][reopen]: close a genuinely active hold phase, reopen storage with a fresh backend and finish the same task. Resumed flags are exactly `[false,true,false]`, all three phases retain the stored creator carrier, the first phase reports aborted/OK, and active deltas are `[1,-1,1,-1]`. Every span ends. |
| `CloseDecisionPrecedence` | Native controls for [scheduler phase finalisation][phase]: 21 live/closing/ended and undecided/waiting/sleeping/terminal combinations. Only an undecided stopped phase becomes aborted; status and duration-metric outcome agree, and repeated finalisation ends once. |

The three integration functions run memory, journal and real modernc SQLite with CGO disabled. Reopen uses the backend's actual storage reopen path. The fresh telemetry backend allocates a distinct process scope and reconstructs its parent from detached data; it retains no original span handle. Extraction and span creation re-enter Snapshot to detect callbacks on the Session line.

Native adapters are explicit: registered compaction and nested-tool tokens are `task.pi.compaction` and `task.pi.nested-tool`; source builtin names differ. Go receipt/entry records, context binding and task definitions replace TypeScript fixtures. Go generation phases use prepare/request; raw model-tool checkpoints still expose an empty phase label. Complete builtin label/phase equivalence is unverified. These controls cover selected assertions and production paths; no complete upstream file/body or TypeScript execution is credited.

### Baselines and checks

The unchanged-runtime Go 1.27.3/CGO0 baseline failed **nine backend leaves, 1.285s**: six parallel/sequential tool leaves lacked persisted carriers; three active-reopen leaves reported interrupted instead of aborted. The background-compaction function passed. Nested metadata, reopened parents and resumed/active sequences passed their assertions. An earlier background-only probe used shell-default Go 1.27.1 and receives no final-gate credit; incorrect draft registration/constant names failed compilation and receive no behavioural-baseline credit.

The first affected regression selection was too narrow and exposed the older `TestTelemetry140BuiltinToolAttributes` fixture's carrierless live-handle assumption. The fixture now supplies immutable carriers and extraction, preserving its tool attributes and both ancestry assertions. The adapted fixture also passes with the original tool implementation overlaid, **0.274s**. Missing propagation data intentionally falls back to host root, as covered by the existing fallback oracle.

Final checks used the sourced project environment, Go 1.27.3, `GOMAXPROCS=4`, `CGO_ENABLED=0` and the protected-prototype overlay:

* Four new functions plus ten affected tool/phase/ancestry/fallback functions, `-count=3 -shuffle=42`: **42 top-level passes, zero failures/skips, 9.359s**. The complete selected function list is recoverable from the test names in this receipt and the gate below.
* Durable vet and Staticcheck **v0.8.2 with tests** pass. Staticcheck uses a physical tracked-source copy plus the new test, excluding the protected prototype. Installed Staticcheck v0.7.0 receives no credit.
* Formatting, diff checks, source-to-validated-copy comparisons and protected-prototype comparison pass. Native v1.4.0 still peels to `ee3076b3659697f1de01b87da964396e069a4389`.

The affected-function regex, in addition to `TestMainBuiltinTrace.*`, was:

```text
TestMainTaskTraceInheritedAndExplicitRoot
TestMainTaskTraceRefusalAndReopen
TestMainTaskTraceUnavailableExtractionUsesRoot
TestTelemetry140BuiltinToolAttributes
TestTelemetry140NestedReplayAndToolAttributes
TestToolExecutionParallelAndSequentialRounds
TestSequentialRoundAbortHasNoLaterTaskAndOneUnstartedResult
TestTelemetry140NativePhaseBoundariesAndReportPrivacy
TestTelemetry140BackendPanicsCannotReplaceTaskOutcome
TestTelemetry140ActiveInvocationBalancedOnClose
```

Seven active production-overlay faults fail assertions, without build failures or skips:

| Fault | Failing leaves |
| --- | ---: |
| Original parallel/sequential tool creation | 6 |
| Original Close finalisation | 10 |
| Force background compaction to root | 3 |
| Force nested tasks to root | 6 |
| Omit parent extraction | 3 |
| Override already-decided outcomes on Close/end | 8 |
| Emit error instead of OK for aborted phases | 10 |

The first mutation attempt mapped only the canonical path while Go used logical `/workspace` paths; it changed no executed source and receives no fault-gate credit. Corrected overlays map both paths. The rejections above come from those effective overlays.

After analysis, 28MB of owned extracts, overlays, logs and static-source copies plus 1.5GB of completed Go build/test cache were removed with no active Go/compiler/staticcheck jobs. Installed dependencies/toolchains and the pinned source mirror were preserved; the protected comparison copy is kept only through the push check.

Only the current telemetry review's evidence cell changes; its disposition stays `pending-review`. Dependencies, SQLite driver, QuickJS/wazero, public commit semantics, task checkpoint fields and native v1.4.0 assets are unchanged. No completed continuation/conformance or earlier task-trace gate was rerun wholesale. No full suite, race, profiling, hosted CI or release publication was run. Broader telemetry outcomes/events, callback clocks, `afterRun`, mixed boundaries, empty-error presence and restart-limit/report/settings still need source oracles.

[prior]: task-traces.md
[background]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry.test.ts#L448-L476
[reopen]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry.test.ts#L478-L544
[generation]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L432-L453
[nested]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/tool.ts#L930-L953
[transaction]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/transaction.ts#L449-L509
[phase]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/scheduler.ts#L2219-L2285
