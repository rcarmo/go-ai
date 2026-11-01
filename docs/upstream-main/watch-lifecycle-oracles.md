# Watch lifecycle assertion crosswalk

Four native scenarios pass unchanged production code at pinned upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`. They exercise real document commits, watches and reporting on memory, journal and SQLite. No runtime change was needed.

Source: `packages/chord/test/watch.test.ts`. Native: `durable/watch_lifecycle_oracle_main_test.go`. This crosswalk covers the assertions below; it does not close the whole Chord or durable watch corpus.

| Source test (starting line) | Native evidence and adaptations | Assertions still open |
| --- | --- | --- |
| Keeps acquisition value until start (94) | `TestMainWatchUpstreamValueOracles/acquisition`: exact `{n:0,text:""}` input, three source operation batches, one `{n:2,text:"ab"}` delivery with `s n 2`, `a text "ab"`; acquisition and delivered `Value()` | Immediate empty callback list just after `start()` depends on JS microtask ordering. Go callback execution is off-line but may race the caller's return. |
| Awaits each callback and folds in-flight frames (155) | `/inflight`: exact `{text:""}` input and `a`, `b`, `c` appends; original first-callback gate retained; first `a` precedes the sole next `abc` call with append `bc`; no concurrent callback | No additional scheduling barrier or sleep replaces a source assertion. |
| Idempotent stop and repeated/late start (706) | `TestMainWatchUpstreamLifecycleOracles/stop`: repeated start rejected, two stops equal, registration removed, late start rejected, including never-started watch | Scalar `0` uses `{n:0}`. Native errors are returned rather than thrown; native diagnostic text differs from `Watch is already started` / `Watch is stopped`. Promise identity is untested. |
| Source ends or fails, dropping pending frames (719) | Existing `TestObservationStopCancelCloseNoJoinAndListenerFailure` checks Session close without joining; task-owner ending tests check final drain | No direct standalone source `end()` API. Native Session end tags are `session_closed` / `session_failed`; owner completion drains rather than discarding pending frames. Exact source test unported. |
| Stop does not await/abort in-flight call (734) | `/stop`: source gate retained; commit second value while first is blocked, stop and observe closure before releasing it, then only first callback finishes | Source scalar `0/1/2` uses `{n:0/1/2}`. |
| Acquisition abort, including already aborted (754) | Existing observation cancellation control checks acquired watch closes | Native tag `cancelled` differs from `aborted`; already-cancelled acquisition can return an error without a watch. Repeat `observeAbort` and direct `abort()` have no native handle API. Exact source test unported. |
| Listener throws, reports once and drops pending (772) | `/listener-error`: same `listener failed` error retained in end/report, original gate, pending second value discarded, registration removed; later Stop retains first cause | Scalar envelope as above. Uses actual Harness `OnReport`; standalone Session has no report callback. Arbitrary JS non-Error throws (test at 794) remain separate from native sanitised panic diagnostics. |

The acquisition and in-flight ports preserve the object-valued source inputs. Lifecycle ports explicitly adapt scalar roots to the native object domain. They check error identity, which is distinct from shared mutable frame/value identity.

## Failure calls and retained frame contexts

Source tests at 394–510 keep recoverable failures open, fold pending values/failures to the newest call, retain the previous delivered value and force a root replacement on recovery. Native `ConversationWatch.Start` currently returns a projection error to the commit listener; that terminates the watch. `DocumentWatch.StartFrame` has value/retirement calls but no failed-call payload. These source assertions are unported. Next: translate the failure/recovery scenarios through real projection paths, then evaluate a tagged failure delivery surface without weakening legacy error handling, authoritative adapter reads, DTO budgets or readiness/Close fences.

Source tests at 631 and 666 require a plain call object with no context, and collection of producer payloads even when consumers retain calls. Native `WatchFrame` exports `Context` as well as receiving the callback context separately. Retaining that frame can retain producer context values. Existing detached-value and consumed-slot tests do not establish the source lifetime assertion. Next: test retained native calls with a live positive control and investigate a context-free delivery representation; removing or clearing the existing public context field requires a compatibility disposition. No GC or identity parity is established here.

## Development checks

Go 1.27.3, GOMAXPROCS=4: new scenarios pass ten normal repetitions/shuffle 42 in CGO 0/1 (0.432s/1.213s), five race repetitions/shuffle 7 (2.093s), and durable vet/staticcheck v0.8.2 in both modes. The new scenarios with existing pending-delivery and close/cancellation regressions pass three race repetitions/shuffle 42 (41.720s); module verification passes. Initial setup probes omitted a required checkpoint predicate, a helper's limits argument and JSON number/map normalisation; those were test errors, not runtime failures.

Six original upstream test bodies (the two value cases and four lifecycle cases through listener failure, excluding abort) also passed against the pinned Watch implementation under Bun with a small synchronous Vitest assertion adapter. This was a selected-body run, not full Vitest. The original source end/failure body ran there but has no exact native port.

Two production overlays prove the native assertions reject regressions: keeping only the newest pending frame fails all six value leaves; overwriting an existing terminal cause fails all three listener-error leaves. Overlays, temporary source extracts, assertion runner and logs were removed after analysis. Full-suite, full race and acceptance profiling were not run for this checkpoint.
