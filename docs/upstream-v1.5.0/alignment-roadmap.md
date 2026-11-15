# Alignment findings and fresh work plan

**Alignment is incomplete. The current worktree fails full durable regression.** This review replaces the accumulated roadmap; historical receipts remain in the linked notes and Git history. It reviews all 29 gap groups and the broader AI/schema/codemode scope. The assertion-level audit is incomplete; the recovery/abort packet below has new runtime checks.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. Canonical migration checkpoints through a5661b7 and the timing/output packet in the containing commit remain integration-red;89c031e is the last fully verified tree. Execution continues under step2. No new integrated acceptance.

## Complete actual-tree baseline

Plan step1 is complete as baseline establishment, not passing integration. Full ./... without overlays/exclusions finishes durable421.579s with95 failing groups and no panic/timeout;25 other tested packages pass (tools9.322s/public10.955s). [Full native failure inventory](../../notes/canonical-integration-baseline.md). Step2 is active. Timing/retained-output fixes pass expanded three-shuffle30.237s,5 semantic faults and static/source checks; exact mappings in the canonical execution note.

## Previous focused packet

Progress cleanup, canonical ID/reset/terminal event fixes and large-image tool settlement have focused regression and semantic fault controls. Canonical progress details/window/attribution also passes. Combined three-shuffle tests32.984s; original inbox37/tools42 zero skipped, separately counted TypeScript probes. Latest full durable attempt377.177s reached93 failed groups then panicked in a now-corrected private tool-output fixture. See [packet mappings and exact failures](../../notes/canonical-builtin-execution.md#progress-reset-and-tool-receipt-packet). That attempt did not complete step1; the subsequent complete baseline above does.

## Evidence baseline (historical review)

| Layer | Evidence | Limit |
| --- | --- | --- |
| Last pushed runtime | `89c031e`: full actual tree, 26 tested packages; durable 458.890s, tools 21.525s, public consumers 12.579s; six semantic faults, ten shuffled repeats, static checks | Selected canonical execution works; normal Submit still used private producers at this revision |
| Uncommitted migration | Submit now produces Generation2 records, canonical Inbox and live `taskId`; targeted fixes include codemode, reset, diagnostics and scheduler cleanup | Later actual-tree runs failed/panicked111.808s and timed out1200.059s in live-partial cleanup; corrected fixture passes, full coverage still incomplete |
| Focused worktree checks | Production paths plus all `TestCodemode120` targets: 60.888s; three shuffled selected repeats: 9.501s; vet, Staticcheck v0.8.2, logging and register checks pass | These passes do not cover all canonical phases or replace full regression/fault/oracle acceptance |
| Original oracle | Selected 200 unchanged cases plus 82 independent probes; separate nested packet 125, zero skips in those runs | Source execution is separate from native equivalence. Full original corpus is not green |
| Broader packages | Recorded full-record catalogue comparison: 1,592 chat, 64 image, 56 classifier; 25 JSON schemas. Bounded validation/OAuth/retry fixes have separate receipts | Metadata equality and selected validation passes do not establish full AI/schema/codemode behaviour |

Sources: [canonical execution](../../notes/canonical-builtin-execution.md), [oracle](../../notes/upstream-oracle.md), [metadata](release-metadata.md), [assessment fixes](assessment-fixes.md). No passing receipt is transferred from an older revision to the red worktree.

## Corrections to earlier findings

- Session/Harness/conversation callbacks return values; public persistence options, canonical entry/conversation/fork constructors and the narrowed public Tx surface are installed. Remaining D13 work concerns complete semantics, identity and other exports, not reimplementing those APIs.
- Generation2/Tool3 definitions and migrations execute canonical records. Production admission migrated in known-red checkpoint365f5b6. Phase-name coverage is not complete phase semantics.
- Source ToolTask executes inside `call` after committing execute intent; `execute` is the recovery handler. Moving ordinary execution to a separate phase would change source behaviour. Nested calls are identified by `input.kind`, not a separate native kind.
- Private checkpoint/envelope fixture failures are not automatically runtime defects. Correct fixtures from pinned assertions, retain all behavioural checks, and rerun to expose any underlying defect. A panic is never a passing result.
- The writer, shared readonly handles, public observers, cold builtin hydration and the 256-job bound are installed. Their remaining audits must not be described as wholly missing implementations.
- Exact AI reference assets and the Chord helper were restored. Cloudflare's original suite passes 36 cases. Current oracle blockers are image-worker memory, original credential/platform skips and worker-specific coverage; the old four-file missing-asset list is historical.
- Missing assertion evidence is classified as unverified. Direct code contradictions and reproduced runtime failures are classified as confirmed. Neither category gives a completion percentage.
- Old afterRun/close direction pauses are superseded by the authorised direct port. Existing data and published assets remain protected; native API compatibility is not a design requirement.

## Immediate regression findings

| ID | Finding and classification | Next proof required |
| --- | --- | --- |
| R1 | Canonical stored-record/stream fixtures now pass bounded recovery/abort assertions. Provider panic/identity, live retry settings and temperature defects were fixed; full recovery acceptance is incomplete | Use canonical TaskRecord state/input and stored signals. Retain poll deadlines, attempt counts, no redispatch, one remote cancellation, partial/usage conversion, error identity and reopen assertions |
| R2 | `events.go` reads `live.run.task` while the new producer writes `taskId`; lifecycle tests miss turn/tool starts and updates. Confirmed integration mismatch | Port exact source event ordering, progress and adjacent result/end assertions through real observation |
| R3 | `SubmissionHandle.Withdraw` still searches private generation checkpoints. Failed-run tests also fail with missing trace submission/owner; their causes are not yet isolated | Replace private-producer assumptions from source; verify queue retention/withdrawal, no placeholder ownership, first-failure and close semantics. Audit the extra public Withdraw/Settlement surface under D13 |
| R4 | Threshold-compaction test expects an answer after failed summarisation; pinned generation prepare rejects a failed blocking compaction. Confirmed expectation conflict | Port the original failed/declined/empty/overflow/threshold cases and preserve retry callback counts. Do not change runtime to satisfy the obsolete expectation |
| R5 | Edited-ancestor HTTP context loses expected section/tool history. Reproduced discrepancy; source-equivalent expected result unresolved | Compare source planSystemEntries/selection and request projection before changing code or assertions |
| R6 | Environment/abort tests expect native aborted envelopes/messages. Reproduced failures with mixed fixture/runtime causes | Assert source unanswered reason, effect fencing, task outcome, committed partial and exact callback errors independently |
| R7 | Scheduler fault/orphan cleanup and deferred decoding were added; focused deferred-fault and held-generic controls pass | Direct canonical fault/orphan/held-child/restart-limit/publication tests, rollback faults and reopen, including no cleanup before owned work ends |
| R8 | Worktree adds native Settlement answer/task inference and retains dual private/canonical paths | Audit against the source public API; remove incompatible convenience contracts rather than treating them as required compatibility |

R1–R8 are a starting inventory. The latest actual-tree command reports66 top-level failure groups before a183.521s terminal-close provider signal panic after redispatch. After-run, tool recovery/cutoff and timer packets have bounded passes; terminal-close/reopen needs source reconciliation. Later suite coverage is incomplete. [Detailed working-tree receipt](../../notes/canonical-builtin-execution.md#uncommitted-production-admission-migration).

## All alignment gaps

The [register](durable-parity-gaps.json) and [detailed evidence](durable-deviations.md) retain each group's sources, affected files, acceptance criteria and dependencies.

| Workstream | Gap IDs | Remaining scope |
| --- | --- | --- |
| Admission, writer and outcomes | D01–D06 | Exact non-document/scheduler read keys, arbitrary failure/own-error boundaries, actual between-part continuation scheduling, capacity/cancellation/cache fast paths and deferred unload/reload/deadline/close interactions |
| Canonical execution and records | D07, D09, D20 | Production generations/tools/nested callers; input/result unions; migrations; provider payload hooks; turn limits; terminate/afterRun; retry/poll/cutoff/abort; queue, failure, orphan, ownership and recovery paths |
| Storage authority | D10, D12 | Arbitrary conforming wrappers at OpenSession; all reads/writes through wrapper authority; full public Storage methods/unions/defaults/scans; raw conformance and exact-prefix adoption |
| SQLite format | D11 | Original schema/migrations and complete database interchange. Determine direct compatibility or an explicitly accepted lossless migration; JSON fixture import alone is insufficient. Preserve existing data |
| Public API and identity | D13 | Complete source-to-Go export map and external consumers; source defaults/errors/presence; publication-record identity; remaining documents, constructor families, callbacks and removal of incompatible extra contracts |
| Readonly values and observers | D14–D17 | Cross-surface sharing, wrapper fallback, asynchronous failures/reentry, warm task reads, version/cache/weak lifetime, cancellation causes, attachment/detachment and dispatch order |
| Bounds and scheduler | D18, D19 | Source numeric/size domain versus native operational limits; pending-edge graph validation; ownership/background/finalisation; indexes, caches and retained scope bounds |
| Telemetry and environments | D21 | Canonical event projection; trace parent/link/phase presence; reporting failures, clock callbacks, environment lifetime and cross-owner capability checks |
| Codemode | D22 | Official guest/worker/import/module wire behaviour, undefined/null, parallel delivery, deterministic replay, interruption/cancellation and complete model/tool integration |
| Schema | D23 | Wire the proven reachable compiler contract only after dynamic-scope, annotations, draft/ref/union behaviour and host RegExp semantics are covered; full diagnostics/cloning/cache/numeric audit |
| AI/provider dependencies | D24 | Full provider stream/message/error/tool/cancellation/retry/session-cache contracts, HTTP/SSE and typed models; exact source schema/metadata/dependency pins |
| Read-only external viewers | D27 | No scheduling/recovery/migration writes; ReadOnlySession failures; concurrent SQLite driver/viewer refresh and safe close without weakening writer locks |
| Bounded fixes retained | D08, D28, D29 | Empty requestId presence, fresh raw root/explicit IDs/empty batches and empty unanswered reasons have historical passing controls. Re-run on the integrated candidate; broader Storage semantics remain open |
| Approved exclusion | D25 | Native Memory/JSONL/IndexedDB availability only. Their shared Storage assertions still apply to SQLite; no other blanket exclusion |
| Acceptance | D26 | All source assertions and support paths mapped; no unapproved skips; exact regression, negative controls, security, profiling/tuning and release evidence |

D04 is still a direct code gap: `session_writer_host.go` has an empty `hop`. D10 still requires private `nativeCore`. D11 still uses native frames/checkpoints rather than the original tables. D27 has no public readOnly option. These were re-inspected during this review; no new runtime test was run.

## Fresh execution sequence

Execution is authorised. Step1 baseline establishment is complete; step2 is in progress. Earlier packet: canonical recovery/abort assertions and runtime refactors have focused/source/static passes, while the integrated baseline is red. Each packet starts with pinned source/tests and a before-fix run. Native APIs and tests are refactored to comply; no native backwards-compatibility requirement applies.

1. **Establish the complete red baseline.** Port panic-prone/private-state fixtures from source first, with canonical reads and signals; retain every behavioural assertion. Run the complete actual tree without exclusions to enumerate all failures. Record revision, test command and cause classification.
2. **Finish the production canonical transition.** Resolve R1–R8, then all generation/tool/nested phase, control, queue, event and recovery consumers. Remove obsolete paths once their replacement assertions pass. Audit new Settlement conveniences rather than extending a second API.
3. **Verify and checkpoint the integrated migration.** Full `./...`, focused repeated/shuffled tests, deliberate semantic faults, original paired probes, static checks, register and external-consumer checks. Commit/push only a verified slice; do not carry an unlabelled red candidate forward.
4. **Finish the public Storage and API boundary.** Complete exports/unions/identity and wrapper authority. Port all Storage conformance through public methods; retain arbitrary refusal/error values and exact candidate/read/write timing.
5. **Align SQLite and viewer behaviour.** Establish the original format/migration contract using original-produced databases; prove rollback/crash/version/ownership handling. Add source readOnly mode and external refresh with no hidden writes.
6. **Close publication and dependency scheduling.** Implement D04's observable continuation bridge and complete D01–D06 assertions through the final Storage boundary. No sleeps, polling or Gosched as ordering proof.
7. **Complete readonly, scheduler and lifecycle audits.** D14–D21: identity/lifetime/cancellation, graph/index retention, hooks, telemetry and environment/cross-owner contracts. Resolve the older close/seal and repeated-call-ID caveats by cause, not repeated isolated passes.
8. **Complete dependency parity.** D22–D24: pi-codemode guest/worker/replay, pi-schema compiler/RegExp/dynamic scope and pi-ai provider/transport/typed-model semantics. Revalidate catalogue, exported schema and dependency provenance after changes.
9. **Close the assertion ledger and oracle limitations.** Review all 165 durable paths (101 tests), all broader main/schema inventory paths, pending tests and worker-specific cases. Resolve the image equality resource failure without weakening comparison, and required credential/platform skips or obtain narrowly explicit scope decisions. Source passes and native passes remain separate columns.
10. **Run pre-release acceptance and measured tuning.** Complete regression, deterministic/shuffle/fault/reproducibility checks, full-record regeneration and corruption controls, vet/Staticcheck/logging, SBOM/licence/vulnerability checks, CPU/alloc_space/alloc_objects profiling. Fix measured outliers, remeasure and delete analysed scratch.
11. **Record the accepted candidate.** Reconcile release evidence, exact source/runtime revisions, exclusions and rollback pointers; clean/synchronised Rui-authored commits. Run hosted CI only for explicit release preparation. Publishing a tag/release remains separately authorised.

Assertion mapping accompanies every step; step 9 reconciles the complete ledger. Document-only review does not promote historical `pending-review` file dispositions. Completion requires no unapproved semantic gap, missing export, unmapped required assertion or required skip within the SQLite-only scope.
