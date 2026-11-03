## Task trace parents and links

Task records now retain data-only trace parents and links for official upstream v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. Runtime `2fd88f26aa88b3b57d48f5f409809e8d35ca0653` extends submission-carrier runtime `a420998` from documentation baseline `6676882`. Full telemetry and release alignment are unverified.

### Record and backend contracts

`Task.Trace` and its detached `TaskRecord` projection accept an opaque string map or the raw record marker `"root"`. `TaskOptions.Trace=nil` inherits the creating context; an explicit `"root"` stores no carrier. Non-nil `SubmissionIDs` override explicit trace/link options: only the first submission supplies the parent (root when its carrier is absent), and later defined carriers become links. An empty ID slice restores ordinary context inheritance. The same selection binds native generation runs at atomic queued boundaries; explicit continue uses its admitted input order.

`Links` is a pointer to a carrier slice so omitted links and `[]` remain distinct. Empty carriers remain `{}`. Task copying validates the closed trace union, strict string values, JSON budgets and ownership before encoding. Canonical views, storage and public reads detach maps and slices; backend `Value`, callbacks, executable marshalers and span objects never enter task records. JSON payloads gain optional fields; the SQLite schema and module dependencies are unchanged. Older-reader/downgrade compatibility is unverified.

`TelemetrySpan.ExtractContext` restores context-only parents and links from detached maps. Nil requests the backend root; nil results and backend failures use the host root. Extraction happens on the invocation host, outside Session/Harness locks, for every phase. Each live phase receives links throughout the first invocation only; sleeping/reserving again or restarting an already-started task does not replay them. Backend mutation cannot alter persisted maps, and link callbacks run on the live phase rather than an ended submit span. Native callback-panic isolation retains its earlier contract; source exception equivalence has not been established.

Persisted parents require the host backend's extractor. A retained creator handle cannot substitute for unavailable extraction. Legacy raw adapters with no task trace metadata still retain their existing process-local ancestry; their complete source correspondence is unverified. Submission and nested-task test backends now supply data carriers and extraction explicitly, retaining their original parent assertions.

Native generation preparation and dispatch share one handler but emit separate `prepare` and `request` spans. Builtin checkpoint labels `queued`/`prepare-next` map to `prepare`, and `intent` maps to `request`, including next-phase outcomes. Checkpoint spellings and custom task phase names are unchanged. Other builtin phase vocabulary needs further review.

### Source crosswalk

The [transaction selection][transaction] and [scheduler phase wrapper][scheduler] supply the source-derived cases below. Two selected [telemetry scenarios][telemetry] cover inherited/root task traces and a shared queued run. Native fixtures use real production paths on memory, journal and modernc SQLite. They do not execute TypeScript or Vitest.

| Native function (`TestMainTaskTrace` prefix) | Selected source assertions or native control |
|---|---|
| `InheritedAndExplicitRoot` | Source177: omitted/undefined creator trace, explicit root without stored carrier, all three phase parents, completed tasks and settled spans. Go nil represents undefined; native task kind/phase definitions adapt the source tokens. |
| `QueuedFirstInvocation` | Source303: two queued inputs share a run; task parent is the second admission, links contain the third; exactly `prepare,request`, each parent/link context, two provider requests and settled spans. Native legacy placeholder rows require selection by the actual submission ID instead of source scan index 1. |
| `SubmissionSelectionNoCarrier` | Source-derived first-ID-only parent selection with absent first carrier and available later link; explicit caller trace/links must be overridden. |
| `CreatorWithoutCarrier` | Source-derived creating scope without propagation data: no fabricated carrier or accidental creator parent. |
| `RefusalAndReopen` | Native confirmed storage refusal leaves no task/ancestry/extraction; failed Session closes, storage reopens, successful pending task closes/reopens again with a fresh backend. Parent, present-empty link, resumed flag and mutation isolation survive memory-image/journal/real SQLite reopen. |
| `LinksFirstInvocationOnly` | Source-derived links on the first invocation, absent on a real second reservation after sleeping; uses actual scheduler state and a controlled clock. |
| `ExtractionPerPhase` | Source-derived exact callback order: parent/link, then parent/link for two phases of one invocation. |
| `UnavailableExtractionUsesRoot` | Source-derived missing/nil extractor fallback to host root. Native panic-isolation control also preserves task completion and stored carrier. |
| `CodecOwnership` | Native map/slice/JSON/public-view detachment, empty/absent fields, raw root marker, invalid unions/null members/cycles/marshal authority and string budget. |

The inherited/root and queued scenarios preserve their selected assertions. Native storage, task tokens, placeholder records and Go context binding differ from the source setup. Whole-file telemetry and continue reviews stay `pending-review`; no complete-file or cross-package parity is established.

### Baselines and checks

Before production edits, the first three functions failed all nine backend leaves: absent task fields, incorrect explicit-root parenting, missing phase links and only `prepare` instead of `prepare,request`. The refusal/reopen, carrierless-creator and first-invocation controls then failed nine backend leaves; refusal/adoption assertions themselves already passed. The corrected refusal fixture reads healthy storage after the Session fails and reopens before further commits. An earlier attempt read the failed Session, and the first sleeping fixture incorrectly waited for full idleness before advancing its clock; those fixture errors have no implementation-failure credit.

Source review found two further draft gaps before checkpointing. Per-invocation extraction produced `parent,link,link`; the new two-phase oracle failed three backend leaves until extraction moved to every phase. Missing extraction incorrectly used a retained creator; the fallback oracle failed three leaves, while nil-result and panic controls passed unchanged. Both gaps are fixed without changing assertions.

The earlier `TestTelemetry140QueuedBatchLinksOriginalSubmission` failure at `50df975`/`a420998` is retained in [the submission receipt][submission]. The old commit-event collector inspected the outgoing task and missed atomic successors. Task metadata and live phase links replace that collector. The fixture now supplies the propagation data/extractor required by the source, and requires **two exact `[2,3]` callbacks**, one for each source phase, instead of its former single merge-event callback. Two requests, four submit roots, dedup origins and panic isolation remain unchanged.

Final focused validation used Go 1.27.3, `GOMAXPROCS=4`, `CGO_ENABLED=0`, the sourced project environment and project-owned scratch:

* Nine new functions plus 16 older telemetry functions and the affected continue-trace function: `-count=3 -shuffle=42`, **78 top-level passes, zero failures/skips, 4.330s**. Earlier partial regex selection matched only the continue function; it received no broader-gate credit.
* The SDK request-JSON control now includes `ExtractContext` and confirms encoding neither serializes nor invokes it: three passes, 0.005s.
* Vet for root/tests/durable, Staticcheck v0.8.2 with tests for those packages, formatting, diff and protected-prototype comparisons pass. Staticcheck uses an exact physical source copy excluding the prototype. Initial missing inference/transport imports and embedded schema in that copy were corrected; no check was suppressed.

Seven active production-overlay faults fail behavioural assertions with no build failure or skip:

| Fault | Failing leaves |
|---|---:|
| Restore local creator fallback when extraction is missing | 3 |
| Drop task trace/link metadata | 3 |
| Inherit creator for explicit root | 3 |
| Omit the request phase boundary | 3 |
| Replay links in subsequent invocations | 3 |
| Alias carrier maps | 1 |
| Cache extraction once per invocation | 3 |

The protected prototype stayed in place, untracked and excluded. Native v1.4.0 still peels to `ee3076b3659697f1de01b87da964396e069a4389`; published assets were untouched. Dependencies, SQLite driver, QuickJS/wazero, public commit semantics and checkpoint fields are unchanged. Completed continuation and SQLite conformance gates were not repeated. No full suite, race, profiling, hosted CI or release publication was run.

After analysis, 14MB of completed source extracts, mutation/overlay files, copied packages and logs plus 1.9GB of completed Go build cache were disposed with no active Go/compiler jobs. The pinned source mirror and installed dependencies/toolchains were preserved; the protected comparison copy was retained only through the push check.

### Remaining work

The subsequent [builtin task checkpoint](builtin-task-traces.md) covers background-compaction metadata, parallel/sequential model-tool and nested-task carriers, active reopen parenting and Close outcome precedence. Broader telemetry events/outcomes, builtin labels/phases, callback/clock semantics and the complete telemetry corpus still need source oracles. Continue still needs `afterRun`, full mixed boundaries, empty-error presence and restart-limit/report/settings reconciliation. Full schema, storage, provider, catalogue and codemode alignment remain open under the existing plan.

[transaction]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/session/transaction.ts#L449-L509
[scheduler]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/scheduler.ts#L2150-L2248
[telemetry]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-telemetry.test.ts#L177-L345
[submission]: submission-traces.md
