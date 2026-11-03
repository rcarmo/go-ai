## Submission trace carriers

Submission status and storage now retain an optional, data-only trace carrier from the submit span. Runtime `a4209985695d89dd758f5bc2153b39f324e8c622` targets official upstream v1.5.0 at `f9a26365568127461c47a07a37dcb42c140a3ac5`; the native baseline is `50df975bac6861ff567a28a6a22ef99efb4c6e96`. Full telemetry and release alignment are unverified.

### Source and native representation

Upstream [submission admission][admit] injects the submit span's carrier into the new record after request-ID lookup. [Submission fields][fields] accept an optional `TelemetryCarrier`; the separate [task trace][task] also accepts `"root"` and has trace links. The telemetry package [defines carriers][carrier] as opaque string records. Carrier keys and IDs have no library-defined interpretation.

The selected [continue trace scenario][scenario] requires a root submit span, exactly one caller link, type `continue`, result `placed`, stored status trace equal to the submit context, a `prepare` phase parented by that context, and settled spans. `TestMainContinueRootTraceAndAdmission` now checks those observations through the actual submit and generation paths. Callback/object identity adapts the source's Chord contexts; the status carrier contains the backend's actual opaque strings.

The Go backend supplies its immutable propagation snapshot in `TelemetrySpan.Context`, separately from process-local `Value` and callbacks. `Context` has `json:"-"`; ordinary AI request JSON never contains it. Durable admission snapshots the map outside Harness/Session locks, then records it as `Submission.Trace`. Public `SubmissionRecord.Trace`, transaction reads, snapshots and storage queries return detached maps. No span object, callback, private `Value`, guessed ID or synthetic carrier is persisted.

`Trace` is a pointer to `goai.TelemetryCarrier`: nil means absent and a pointer to an empty map preserves `{}`. Its JSON decoder requires string values, including rejecting null members. This native type fence has separate controls; broader malformed/untyped submission semantics have not been reconciled. The optional record field changes JSON payloads without changing the SQLite schema. Older reader/downgrade compatibility has not been verified.

Builtin generation preparation spans now publish `phase=prepare`. The internal checkpoint stays `queued`; custom task phases named `queued` retain their own label. Other builtin phase vocabulary and full task-wire equivalence need separate review.

### Baselines before fixes

| Probe | Unchanged behaviour |
|---|---|
| Source356 persisted status plus source-derived DTO ownership/reopen | Two functions failed four memory/journal leaves, 0.205s: no serializable status or stored trace. An optional reflected field setter kept the old SDK compilable; assertions inspected real production records. After the field was added, the fixture switched to ordinary typed assignment. |
| Empty versus absent carrier and non-string DTO values | Two functions failed seven leaves, 0.007s: an empty carrier disappeared and unknown trace fields bypassed decoding. The absent control passed. |
| Literal upstream `prepare` label | The updated source356 assertion failed both backend leaves, 0.132s, after carrier persistence already passed. The native label was still `queued`. |

No production change was made for the passing absent control. Initial expanded-fixture errors are excluded: SQLite reopen incorrectly called public Close on Session-owned storage, and the new passive-entry control omitted its required native object `Value` (its `Data` was also corrected to an object). The corrected fixture respects the already-closed ownership fence and uses a valid native entry.

### Focused checks

All commands used Go 1.27.3, `GOMAXPROCS=4`, `CGO_ENABLED=0`, the sourced project environment and isolated project-owned scratch. The protected continuation prototype stayed in place, untracked and excluded by logical/canonical Go overlay; Staticcheck used an exact physical package copy without it.

Nine trace functions passed three repetitions each across two final runs: **27 top-level passes**. Eight participated in the affected 25-function run; the final public empty/absent control passed separately in 0.111s. The functions are:

* `TestMainContinueRootTraceAndAdmission` and `TestTelemetryCarrierDataOnlyAndStrictDecoding`.
* `TestMainSubmissionTraceDTOOwnershipAndReopen`, `EmptyAndAbsent`, `RejectsNonStringCarrier`, `PublicCaptureDedupAndReopen`, `Rollback`, `CustomQueuedPhaseKept` and `PublicEmptyAndAbsent` (all with the `TestMainSubmissionTrace` prefix).

The DTO, public input/write/dedup/reopen and rollback functions use memory, journal and real modernc SQLite. They verify request deduplication leaves the original carrier and commit sequence unchanged, backend/public-map mutations cannot alter stored data, settled traces reopen without a live telemetry backend, and rollback publishes no submission/request. SDK controls check strict atomic decoding and exclusion of backend data from request JSON. No platform-wide SQLite or full storage acceptance is established.

The affected run used `-count=3 -shuffle=42`: **72 top-level passes and three failures** across 25 functions (tests package 0.006s, durable 3.033s). Sixteen older functions passed all three repetitions. The single older failing function is documented below; the affected gate as a whole FAILED. Completed continuation and SQLite conformance gates were not repeated.

Vet for root/tests/durable, Staticcheck v0.8.2 with tests, module verification, formatting, diff and protected-prototype comparisons pass. The last added test received a fresh durable vet/Staticcheck check. Dependencies, module manifests, the SQLite driver and QuickJS/wazero implementation are unchanged. No full suite, race, profiling, hosted CI or release publication was run.

### Active fault probes

Each overlay changed real production code, retained the prototype exclusion, ran the eight trace functions available at that point, and failed behavioural assertions without a build failure or skip. The later public empty/absent function was not part of these executions.

| Fault | Failing leaves |
|---|---:|
| Do not capture the backend carrier | 5 |
| Return aliased status/transaction trace | 3 |
| Return aliased snapshot trace | 6 |
| Drop typed transaction-created trace | 4 |
| Drop final passive-write trace | 3 |
| Restore builtin `queued` telemetry label | 2 |
| Rename every custom `queued` phase | 1 |
| Use ordinary string-map decoding, allowing null members | 2 |
| Omit empty carriers | 1 |

### Queued-link failure at a420998 (historical)

The later [task trace checkpoint](task-traces.md) replaces the collector described below with persisted task metadata and per-phase extraction/links. Its focused gate passes; these earlier failures and limits retain their original runtime scope.

`TestTelemetry140QueuedBatchLinksOriginalSubmission` fails unchanged at the exact committed baseline `50df975`: four normal/panic memory/journal leaves, 0.315s, with two requests, four submit roots and **zero links instead of `[2,3]`**. The current candidate gives the same failure in each of three repetitions. This is an existing implementation gap, not a passing regression gate.

`queuedInputLinks` searches for newly merged inputs only on `tx.byTask`. Atomic final boundaries can instead start a different queued generation task in the outgoing task's transaction; the collector misses that successor. Required trace links must attach to the successor's live trace, without invoking a backend under Session/Harness locks or adding links to an already-ended submit span. Simply relaxing the test or relabelling the creator would not preserve those contracts.

At this checkpoint, the next bounded oracle was source run-start parent/link selection in [transaction assembly][assembly], including carrier-backed input grouping, the caller without propagation data, failed adoption, and a real reopen. Native task `trace`/`traceLinks`, root-sentinel behaviour and adapter extraction across restart were unported at `a420998`; the linked task receipt records their later scoped implementation. The current `Context` snapshot API closes submission storage/status only; it does not restore a backend span from persisted data. Other open work includes `afterRun`, full mixed boundaries, empty-error presence, restart-limit/report/settings, callback/clock behaviour and the complete upstream test bodies. Whole-file continue review stays `pending-review`.

After analysis, 54MB of owned source extracts, overlays, copied packages and logs plus 1.7GB of completed build cache were disposed with no active Go/compiler jobs. A temporary protected-file comparison copy is kept only until the push check finishes. The pinned source mirror, installed dependencies/toolchains, protected prototype and native v1.4.0 refs/assets are preserved.

[admit]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/submissions.ts#L229-L243
[fields]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts#L488-L503
[task]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/types.ts#L395-L407
[carrier]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/telemetry/src/index.ts#L9-L16
[scenario]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L356-L392
[assembly]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/transaction.ts#L449-L471
