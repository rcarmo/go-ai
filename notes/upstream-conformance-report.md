# Upstream conformance completion report

Latest bounded packet: source environment error/result/report semantics and public protocol/queued-metadata/reset consumers. Expanded3-shuffle84.733s/static/seven restored controls; original tools42/input-entries11/generation26/inbox37 plus95 separate probes zero skipped. Complete actual-tree baseline24: durable854.904s with48 failed top-level groups;25 other tested packages pass (tools14.876s/public12.266s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree. [Mapping](canonical-builtin-execution.md#environment-errors-and-public-metadatareset-consumers).

Earlier bounded packet: single rendered system sections and source JSON property order; exact HTTP/root/Codex source-first tests,11 semantic controls restored, expanded3-shuffle99.877s/static pass. Original prompt9/generation26/transcript16 plus94 separate probes pass zero skipped. Complete actual-tree baseline23: durable842.839s with52 failed top-level groups;25 other tested packages pass (tools15.114s/public13.570s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree. [Mapping](canonical-builtin-execution.md#single-rendered-system-sections-and-property-order).

Earlier bounded packet: guarded constructor reservations fix the demonstrated Session failure-latch defect; six failure/rollback consumers use actual canonical records/publications. Expanded three-shuffle41.303s, five allocator bypass controls detected/restored, static gates pass; unchanged originals27/37/18 and93 separate probes pass zero skipped. Complete actual-tree baseline21: durable835.764s with53 failed top-level groups;25 other tested packages pass (tools14.923s/public12.659s). No panic, timeout, overlay or exclusions. Integration remains red;89c031e is still the last fully verified tree. [Mapping](canonical-builtin-execution.md#failure-placement-and-guarded-constructor-reservations).

**Overall migration: incomplete. The current worktree fails full durable regression.** The [revised findings and fresh plan](../docs/upstream-v1.5.0/alignment-roadmap.md) replace the accumulated checklist. Historical receipts below retain their original revisions and scope.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `/work/go-ai/durable`, authorised SQLite-only, pure Go. Last fully verified tree remains `89c031ebcdc7374b28240ca09511bfcd7e9f8c22`; subsequent canonical admission checkpoints are committed/pushed but integration-red.

Current: explicitly resumed; step2 active. Full ./... observes durable416.189s/72 failed groups, no panic/timeout/exclusions;25 other tested packages pass. Diagnostic JSON/control packet: three-shuffle28.152s/static/3 semantic faults, original tools42 zero skipped plus91 separate probes. Two newly failing deferred fixtures were subsequently moved to host settings; final exact-data/deferred subset5.769s passes. No full-tree result after those fixture corrections. [Mappings/remaining scope](canonical-builtin-execution.md#diagnostic-json-and-source-tool-controls). Full integration/release acceptance remains red.

Historical verified continuation (89c031e): [Canonical builtin execution](canonical-builtin-execution.md) now runs real Generation2/Tool3 records and migrations through the generic runtime: imported no-model generations, model answers/tool rounds, Tool1/2/3 calls, replay policy, thrown failures and canonical nested available/unavailable calls. The actual full working tree passes26 tested packages (durable458.890s/tools21.525s/public12.579s), six compiled faults, ten shuffled canonical repetitions14.008s, vet/Staticcheck/logging/register and selected original282, zero skipped. Normal Submit/private callers and the complete phase/lifecycle/provider/control assertions still need migration; full alignment remains incomplete.

Previous continuation: [task kind and phase string keys](internal-task-replacement.md#task-kind-and-phase-string-keys). Task kinds and phase names now follow source string-map lookup, without Go’s lowercase identifier/128-byte grammar. Empty, uppercase/spaces, Unicode, NUL and129-byte keys execute through real task creation, reservation, phase transition and terminal settlement. Strict UTF-8/string-budget and state/record checks remain. The new native packet first failed0.023s on all five cases; its independent original Harness probe passes. Ten shuffled repetitions2.313s, four independently compiled grammar-restoration faults and static/logging/register pass. The candidate passes26 tested packages (durable450.325s/tools20.319s/public12.032s); its compiler overlay excludes only the two previously uncommitted canonical builtin RED targets, retaining every accepted assertion. Selected original281 (200 unchanged plus81 probes) and table101 pass zero skipped. Canonical builtin execution and the current working tree are not accepted.

Previous foundation continuation: [generic runtime source replacement and registry construction](internal-task-replacement.md#generic-runtime-integration-and-registry-construction). The candidate passes the previously accepted suite plus its new registry control; two upcoming canonical builtin tests were red at that checkpoint. They pass in the latest actual working-tree run.

Previous continuation: [submission tool/trace/lifetime packets](submission-tool-lifetime.md). A real held-tool boundary, steer-to-run trace link and invocation-bound child lifetime now have literal native source packets. Canonical builtin execution/migrations are the next unfinished boundary. The preceding implementation checkpoint is `6c6758bf41812bf594822fb0b102eeb3c12f9f82`.

## Earlier queue-production checkpoint

- Ported the full atomic busy-steer assertion packet: the host checks Live, writes its own document, admits input, then appends its own entry in one stored commit. Sent IDs, the entire Inbox item and the active run's original inputs match the pinned assertions.
- Replaced private queued input envelopes with canonical `content` and optional `entry` metadata. Block input and explicit-null data retain their original shape.
- Queued passive writes now contain a canonical EntryDraft. The same encoder supplies compaction-write queue production; placement IDs, task attribution and the private Value field are excluded.
- Verified idle input requests progress itself, without Resume or Submission.Wait masking an omitted admission wake-up.
- Verified failed-run continuation inside a caller transaction, its write fence and subsequent host entry.
- Added a definite SQLite failure/reopen control: failure identity survives and no submission, task, entry or run survives reopening. This is not complete arbitrary/uncertain Storage-failure acceptance.
- Verified canonical queued model-visible writes and eventual placement/completion. This fixture holds a provider, not the original tool boundary; that source variant remains open.

The exact input/write shape tests failed before the fixes. Idle progress and continuation already passed and required no speculative runtime changes. A failure/reopen fixture initially tried to close storage already owned/closed by Session; correcting the fixture required no runtime fix. Tests remain in the existing version-neutral transaction suite.

## Submission tool/trace/lifetime verification

Full26 tested packages pass: durable838.557s/tools28.232s/public12.387s; three compiled faults, ten shuffled trace9.393s/lifetime11.248s, tool-bound placement repeats1.794s and vet/Staticcheck/logging pass. Unmodified original submit-in-commit110, telemetry113 and ownership128 cases pass with87 supplements each, zero skips. Earlier full571.063s failed the obsolete missing-conversation error assertion; the corrected test retains absence/bootstrap/reopen assertions. See the linked packet for fixture errors and exact remaining scope.

## Previous internal task replacement verification

Implementation [6c6758b](internal-task-replacement.md) is pushed. The internal canonical setter now supports complete replacement, staged guards/timestamps, deferred committed validation, final owner checks and fork-error precedence. It stays private; production runtime callers have not migrated. Full26 tested packages pass (durable746.181s/tools27.556s/public13.197s), five compiled semantic faults, three shuffled repetitions0.897s, vet/Staticcheck/logging/register and selected original279 pass, zero original skips. Complete builtin/runtime integration, document/family/identity/API/Storage/lifecycle/readonly/assertion audits and pre-release profiling are incomplete. The new source clock supplement initially used an invalid total-commit clock-count assumption; corrected supplemental fixtures isolate setter calls. Original assertions are unchanged. See the linked note for red controls, source lines and precise subsets.

## Previous public Tx verification

Full repository ./... passes26 packages: root0.853s, durable585.852s/tools28.155s and public consumers12.711s. Nine independently reintroduced public API aliases plus marker arity/current-value faults are detected after compilation. Ten surface repetitions0.016s/ten shuffled marker repetitions0.352s, vet/Staticcheck/logging/register and selected original277 (200 unchanged +77 probes) pass, zero original skips. Analysed temporary scripts/logs/overlays are removed. This establishes the tested public Tx method boundary and current-marker shape/value, not complete API/record identity/internal mutation/document/builtin/Storage/release conformance.

## Previous fork/assembly verification

Full repository ./... passes26 packages: root0.707s, durable531.225s/tools26.826s and public consumers12.551s. Ten compiled semantic faults, ten fork repetitions1.827s, history13.704s, controlled builtin progression40x3 cases90.839s and vet/Staticcheck/logging/register pass. Selected original276 (200 unchanged +76 probes), complete original forks14+84 supplements98 and tools42+84 supplements126 pass, zero skipped. Analysed temporary logs/overlays are removed. Publication-record identity, internal canonical setTask/atomic owners, public native-extra Tx methods, full fork/builtin/API/Storage and pre-release acceptance remain open.

## Previous conversation-constructor verification

Full repository ./... passes26 packages: root0.871s, durable525.645s/tools28.155s and public consumers12.505s. Nine compiled semantic faults, ten constructor repetitions1.951s, final ten blind-admission repetitions0.413s, vet/Staticcheck/logging/register pass. Selected original273 (200 unchanged +73 probes), including complete original table12+81 supplements93, pass with zero skipped. Analysed temporary logs/overlays are removed. Publication-record identity, ForkConversation/SetTask and atomic owner packets, untyped/schema timing, complete builtin/API/Storage and pre-release acceptance remain open.

## Previous cycle-error verification

Full repository ./... passes26 packages: root0.843s, durable509.123s/tools27.829s and public consumers12.499s. Ten shuffled constructor repetitions2.566s, two independently compiled cycle-message faults, vet/Staticcheck/logging/register and complete original table12+79 supplements91 pass, zero original skips. Analysed temporary logs/overlays are removed. Publication-record identity, other constructors/builtin producers and broader API/Storage/release acceptance remain open.

## Previous entry-constructor verification

Full repository ./... passes26 packages: root0.807s, durable537.260s/tools27.472s and public consumers12.798s. Eleven semantic faults, ten shuffled constructor/tool repetitions1.658s/6.324s, three final constructor/tool repetitions0.651s/2.045s, vet/Staticcheck/logging/register pass. Selected original271 (200 unchanged +71 probes), including complete original table12 and79 supplements, pass with zero skipped. Analysed temporary logs/overlays are removed. Publication-record identity, untyped extra-field/validation timing, other constructors/builtin producer migrations and full API/Storage/release acceptance remain open.

## Previous public persistence verification

Full repository ./... passes26 packages: root0.693s, durable439.106s/tools19.928s and public consumers12.206s. Eleven semantic faults, ten shuffled options/value/numeric/policy repetitions20.851s, vet/Staticcheck/logging/register pass. Selected original270 (200 unchanged +70 probes) and complete original session-deferred25+78 supplements103 pass, zero skipped. Analysed logs/overlays are removed. Complete persistence lifecycle/ordering, Tx constructor record results, canonical generation/ToolTask/migration, broader API/Storage and pre-release profiling remain open.

## Previous tool callback-value verification

Full repository ./... passes26 packages: root0.667s, durable443.832s/tools20.803s and public consumers12.761s. Six semantic faults, ten shuffled value/recovery/authority repetitions29.581s, vet/Staticcheck/logging/register pass. Selected original269 (200 unchanged +69 probes) and complete original harness-tools42+77 supplements119 pass, zero skipped. Analysed temporary logs/overlays are removed. Source CommitOptions, Tx.appendEntry record results and complete API/worker/runtime/Storage/release acceptance remain open. The native control is source-derived and returns its minted ID; the original probe returns the appended record. This does not accept the still-different native AppendEntry signature or the full original API assertion packet.

## Previous commit callback-value verification

Full repository ./... passes26 packages: root0.729s, durable440.514s/tools20.322s and public consumers12.771s. Seven semantic faults, ten shuffled value/publication/batching/observer repetitions4.450s, related12.414s and three boundary/value repetitions24.927s pass. Vet/Staticcheck/logging/register, selected original268 (200 unchanged +68 probes) and original session-group40+76 supplements116 pass, zero skipped. Analysed temporary logs/AST/fault scratch are removed. Source CommitOptions persistence, ToolAPI callback values, full API/Storage/ordering and release acceptance remain open. The preceding440.940s full run failed two further obsolete zero-sequence probes; corrected controls assert nil callback result and retain exact rollback/metadata checks. No original assertions changed.

## Previous image-content verification

Full repository ./... passes26 packages: root0.841s, durable444.877s/tools20.573s and public consumers11.752s. Eight semantic faults, ten shuffled SDK/native repetitions0.213s/34.278s, vet/Staticcheck/logging/register pass. Selected original267 (200 unchanged +67 probes) and original codemode-models7+75 supplements82 pass, zero skipped. Analysed temporary logs/overlays are removed. Complete image/context/criteria/error/optional unions, canonical generation/ToolTask/migration, worker/API/Storage and pre-release acceptance remain open. D09/D13/D22 current evidence is condensed; historical receipts remain and no disposition closes. The stale reference to exported CommitDeferred is corrected: there is no public source CommitOptions argument, while private deferred entries remain.

## Previous classifier-image verification

Full repository ./... passes26 packages: root0.750s, durable425.515s/tools20.279s and public consumers13.255s. Seven semantic faults, ten shuffled repetitions SDK0.180s/durable13.085s, vet/Staticcheck/logging/register pass. Selected original266 and separate codemode81 pass, zero skipped. Analysed temporary logs/overlays are removed. Complete image/context/criteria/error unions, canonical ToolTask/runtime/migrations, worker/API/Storage and pre-release acceptance remain open.

## Previous classifier-context verification

Full repository ./... passes26 packages: root0.714s, durable423.520s/tools17.580s and public consumers12.533s. Six semantic faults, ten exact-context shuffled repetitions4.771s and three related repetitions10.370s pass. Vet/Staticcheck/logging/register, selected original266 (200 unchanged +66 probes) and original codemode-models7+74 supplements81 pass, zero original skips. Analysed temporary logs/overlays/probes are removed. Complete context/image/criteria/error unions, ToolTask runtime/migrations, worker/API/Storage and pre-release acceptance remain open.

## Previous string tool-name verification

Full repository ./... passes26 packages: root0.675s, durable436.064s/tools20.438s and public consumers12.119s. Seven semantic faults, ten exact-name shuffled repetitions4.580s, three broad repetitions29.773s and three corrected-control/default repetitions7.981s pass. Vet/Staticcheck/logging/register and selected original265 (200 unchanged +65 probes), plus original harness-registry10+73 supplements83, pass; zero original skips. Analysed logs/overlays are removed. Full ToolTask/runtime/migration, extension-name installation, UTF16-name transport, schemas/errors, worker/API/Storage and release acceptance remain open. The preceding441.771s full run failed three obsolete native name fixtures; source-valid empty/slash strings were replaced with invalid-UTF8 or bounded-list guards, retaining every assertion. A first malformed-UTF8 nested fixture reached invalid-result validation; the corrected list guard passes. No original test/assertion changed.

## Previous internal model-tool verification

Full repository ./... passes26 packages: root0.604s, durable418.648s/tools20.143s and public consumers12.467s. Seven semantic faults, ten shuffled focused repetitions11.816s, vet/Staticcheck/logging/register pass. Selected original264 and separate codemode79 pass, zero skipped. Analysed temporary logs/overlays/recaptures are removed. Complete ToolTask runtime/migrations, arbitrary name/error/schema, worker/API/Storage and pre-release acceptance remain open.

## Previous typed-result verification

Full repository ./... passes26 packages, including root0.742s, durable433.467s/tools20.365s and public consumers13.536s. Ten shuffled repetitions pass public SDK0.381s/durable10.498s; six semantic faults, vet, Staticcheck, logging and register pass. Selected reference264 (200 unchanged +64 probes), original codemode-models7 plus72 supplements (79 total) and original compaction110 plus72 supplements (182 total) pass, zero skipped. Analysed logs/overlays are removed. D09/D13 current evidence is condensed to remove superseded contradictions; linked history remains and no disposition closes.

## Previous optional-error verification

Full repository ./... passes26 packages, including root0.699s, durable433.921s/tools24.354s and public consumers12.641s. Ten shuffled presence/trim repetitions11.563s and replay/presence repetitions4.502s pass. Five presence/strict/projection faults plus one broad-authority fault are rejected; vet, Staticcheck, logging and register pass. Selected reference263 (200 unchanged +63 probes) and separate original compaction181 pass, zero skipped. Analysed logs/overlays are removed. The first full run failed unchanged durable replay controls420.884s; the exact library-type fix passes without relaxing caller wrappers. No failing receipt is relabelled green.

## Previous compaction text-decision verification

Full durable431.252s/tools19.986s; ten shuffled targeted repetitions15.268s; three semantic faults, vet, Staticcheck, logging and register pass. Selected reference262 cases (200 unchanged +62 probes) and separate original compaction180 pass, zero skipped. Analysed logs/overlays are removed. Existing hook ownership/dispatch/decline/panic controls remain; complete original hook-input packet and optional provider-error presence remain open.

## Previous model-reference verification

Full durable415.335s/tools18.627s; ten shuffled targeted repetitions7.485s; restored id fault rejected independently by producer/wire and phase-reader controls; vet, Staticcheck, logging and register pass. Selected reference260 cases (200 unchanged +60 probes) and separate original compaction178 pass, zero skipped. Analysed logs/overlays are removed. Other agent-field presence/settings and complete model/API acceptance remain open. No legacy id fallback or stored-data rewrite is added.

## Previous compaction export verification

Full durable430.967s/tools20.341s; ten shuffled targeted repetitions11.855s; four semantic faults, vet, Staticcheck, logging and register pass. Selected reference259 and separate original compaction177 pass, zero skipped. Analysed logs/overlays are removed. Full manual/automatic input presence, actual public-token execution, hook ownership and alarm-only continuation controls retain their assertions. The touched alarm fixture now waits through commit signals rather than polling.

## Previous summary-budget verification

Full durable431.008s/tools20.204s; ten shuffled budget/settings repetitions8.347s; two semantic faults, vet, Staticcheck, logging and register pass. Selected reference259 cases (200 unchanged +59 probes) and separate original compaction177 pass, zero skipped. Analysed logs/overlays are removed. Existing zero/model cap/override/settings/ownership/retry assertions remain. Fractional inputs and complete numeric/API acceptance remain open.

## Previous LiveDoc compaction-key verification

Full durable427.062s/tools15.285s; ten shuffled targeted repetitions10.529s; four semantic faults, vet, Staticcheck, logging and register pass. Selected reference258 and separate original compaction176 pass, zero skipped. Both original request/retry fixtures, including LiveDoc snapshots, recapture byte-for-byte. Analysed logs/overlays/recaptures are removed. Complete-record assertions cover retry, missing-model and cutoff failures; legacy v1/v2 migration remains unchanged.

## Previous compaction result verification

Full durable430.208s/tools20.080s; ten shuffled targeted repetitions19.150s including the subsequently strengthened hook-result assertions; three semantic faults and a previous-production hook baseline rejected; vet, Staticcheck, logging and register pass. Selected reference258 and separate original compaction176 pass, zero skipped. Exact empty/queued/owned/background results failed before production changes; no lifecycle assertions were removed. Full result-run production passed before the additional hook assertions, which passed the subsequent focused repetitions.

## Previous canonical compaction checkpoint verification

Full durable456.191s/tools19.140s; ten shuffled focused repetitions7.998s; five semantic faults rejected; vet, Staticcheck, logging and register pass. Selected reference258 cases (200 unchanged +58 probes) and original compaction110 plus66 supplements (176 total) pass, zero skipped. Exact checkpoint/retry controls failed before fixes. Original request/retry selected-record fixtures retain exact raw task JSON and execute after reopen. Complete compaction result/provider/migration and store interchange remain open.

## Previous compaction request-authority verification

Full durable420.167s/tools19.837s; ten focused shuffled repetitions6.877s; four semantic faults; vet, Staticcheck, logging and register pass. Selected reference258 cases (200 unchanged +58 probes), plus the separately executed original compaction110-case file and66 supplements (176 total), pass with zero skipped. Real SQLite reopen and legacy-input retry controls failed before fixes. Canonical checkpoint DTO/execution and remaining state/ordering/interoperability remain open.

## Previous compaction-policy verification

Full durable417.368s/tools17.347s; ten shuffled focused repetitions16.922s; four semantic faults; vet, Staticcheck, logging and register pass. Selected reference256 cases pass (200 unchanged +56 probes). Existing threshold fixtures now use real model windows/reserves rather than native trigger knobs. Source-derived exact field-set/zero-value, strict threshold/owner and complete automatic-input assertions pass. Legacy raw-input retry fallback and canonical checkpoint execution remain open.

## Previous manual-compaction API verification

Full uncached durable854.735s/tools26.544s; ten focused shuffled repetitions12.790s; four semantic faults; vet, Staticcheck, logging and register pass. Selected reference255 cases pass (200 unchanged +55 probes). Public CompactionOptions is removed;18 caller fixtures now configure policy through actual agent settings. Full task input, instruction presence and pointer ownership are checked without normalization. Complete policy/checkpoint/public API acceptance stays open.

## Previous truncation/lifecycle verification

Full uncached durable849.377s/tools26.940s;100 shuffled targeted repetitions39.619s; two semantic slice/count faults; vet, Staticcheck and logging pass. Following dead fixture-helper cleanup, three full gate-suite repetitions6.941s and static gates pass. Selected reference254 cases pass (200 unchanged +54 probes).

Earlier complete runs failed:855.848s on the repeated-provider-call-ID three-second native wait (25 isolated repeats pass; root cause unknown), then848.604s on the watch-cleanup race and incorrect case680 fixture. The latter two are fixed; the timing caveat remains open, with failure-only progress diagnostics. No failing receipt is relabelled green.

## Previous compaction verification

Full uncached durable829.093s/tools24.860s; ten new shuffled repetitions0.588s; focused compaction/summary/search16.213s; three semantic faults; vet, Staticcheck, logging and register pass. The original compaction file passes110 unchanged cases plus61 supplements,171 total and zero skipped. Exact prompt source bytes independently match the remote commit. Broader compaction selection/checkpoints/recovery, UTF-16 truncation, argument ordering and explicit-empty instruction variants remain unaccepted.

## Previous input-authority verification

Full uncached sequential durable842.589s/tools26.862s; ten focused shuffled repetitions35.080s; four semantic compiler faults; vet, Staticcheck, logging and register pass. The two new source-derived tests have independent original probes. All253 selected reference cases pass (200 unchanged +53 probes). Five legacy Inbox fixture sites and unplaced ownership fixtures were corrected without weakening their assertions.

Compaction admission still consults historical checkpoint input for unplaced work. Complete canonical queue selection, generation/tool execution and migrations remain open.

## Previous string-content verification

- Full uncached sequential durable **830.400s**, tools **26.898s**.
- Ten focused shuffled repetitions, seed42: **15.726s**.
- Four semantic compiler faults rejected: changed string content, omitted timestamp zero, wrong placement clock, dropped compaction string.
- Repository vet, Staticcheck v0.8.2, logging, register and diff whitespace checks pass.
- Original selected execution:192 cases in ten original files plus59 supplemental cases (eight unchanged conformance cases and51 independent probes), all251 pass; no skipped cases.
- Literal captured source records: three conversations/nine entries, including string/array content, empty string, zero timestamp and nested replacement messages; exact public JSON and SQLite reopen assertions pass.

The previous complete run finished in874.059s rather than hanging, and failed one block-only native history assertion. That assertion now requires the canonical string arm. Shorter timeout/interrupted diagnostic runs are not passing receipts. Broader compaction serializer parity, every message/input variant and canonical builtin execution remain open.

## Previous queued-write verification

| Check | Result |
|---|---|
| Full `./durable/...`, uncached, sequential | durable **409.428s**; tools **16.262s** |
| Ten focused shuffled repetitions, seed42 | **115.206s** |
| Repository vet, Staticcheck v0.8.2, logging | Pass |
| Deliberate compiler faults | Three semantic failures: private payload consumption, early terminal suppression, early missing-record lookup |
| Original-source oracle | **189 unchanged cases +51 independent probes**, pass |
| Register | 29 groups; 165 review obligations across 101 source test files; source/tree and link checks |

The reference total comprises 181 tests across nine unchanged upstream files, eight unchanged Storage conformance cases, 35 admission probes and 16 other probes. Those tests run the original implementation; they are not 189 accepted native cases. All 23 original submit-in-commit tests execute, but native counterparts for every assertion are not complete.

The retained source snapshot has no `.git`: retained hashes and the complete test tree are checked, but commit identity is not independently reverified. Verification uses `CGO_ENABLED=0`, `GOMAXPROCS=2`, `-p 1`; no race-detector coverage is claimed. This is ordinary development verification, not pre-release CPU/allocation acceptance. Disposable fault overlays are deleted.

## Remaining completion blockers

| Area | Remaining work |
|---|---|
| Canonical builtin execution — D07/D09 | Normal execution still uses historical private generation/tool checkpoints and submission payloads. Canonical write draft consumption and verified string-content input placement are ported; full input/generation/tool execution, complete message variants, migrations and recovery interoperability remain open. |
| Storage interfaces and persistence — D10–D12 | Complete wrapper/public-interface authority, original SQLite schema/record interoperability, and remaining result/query DTO assertions. |
| Commit/public API — D13 | Original callback-result/options semantics and full exported-symbol mapping. Go Commit still returns a sequence and uses caller-side result capture. |
| Publication/admission — D01–D06 | Exhaustive dependencies, bounded outcomes, caller-continuation turns, deferred persistence and close/error ordering. D04 is not accepted. |
| Submission/lifecycle — D20/D21 | Tool-bound write placement, steer link/non-reparenting and the named ended-tool child lifetime packet now pass. Arbitrary/uncertain Storage failures, full trace callback/event ordering, queued task-bound close/seal precedence and exhaustive resume/scheduling assertions are incomplete. The earlier queued progress/close error-order caveat remains open. |
| Readonly/document/view lifetimes — D14–D17 | Complete mount/collection/cache/reload/lifecycle and observer-dispatch acceptance, beyond the mapped passing identity assertions. |
| Resources, ownership and external viewers — D18/D19/D27 | Complete resource/numeric/ownership/race corpus and read-only concurrent SQLite viewers. |
| Codemode/schema/provider — D22–D24 | Complete worker/restart, dependency/compiler/RegExp and inherited AI/provider acceptance. |
| Final verification — D26 | Full assertion-level crosswalk, complete regression/fault evidence and pre-release CPU, alloc_space and alloc_objects analysis/tuning. |

D08, D28 and D29 are recorded as fixed; other groups contain partial progress, confirmed deviations or unverified obligations. Alternative storage engines remain outside the user's authorised SQLite-only scope (D25). Neither those status labels nor green development suites define a completion percentage.

The next implementation work is canonical builtin execution/migrations; broader submission/trace/queued-close acceptance is still incomplete. The complete register, including causes, evidence and acceptance criteria, remains authoritative: [deviations](../docs/upstream-v1.5.0/durable-deviations.md), [structured gaps](../docs/upstream-v1.5.0/durable-parity-gaps.json), [assertion crosswalk](../docs/upstream-v1.5.0/task-table-assertions.tsv). Detailed slice receipts are in [submission admission](submission-admission.md).

Current checkpoint: `365f5b6` was committed/pushed at the user’s request with known full-regression failures. It is not an accepted candidate; `89c031e` is the last fully verified tree. Close/reopen and final-reset fixture reconciliation has bounded passes; see [working receipt](canonical-builtin-execution.md).

Latest source-first tool-consumer checkpoint: four fixture translations, no runtime changes; expanded three-shuffle20.043s/static/3 semantic faults and original tools42/search6/codemode-describe5 (separate91 probes) pass. Actual full ./... remains red:67 durable groups390.511s,25 other tested packages pass; no panic/timeout/exclusions. New uncertain-intent failure remains unverified. [Mapping and exact limits](canonical-builtin-execution.md#canonical-terminal-tool-consumers).

Latest tool-round checkpoint: fresh host execution-mode read at round start, then fixed pending-call decision; canonical round and ordered three-call abort fixtures. Expanded three-shuffle23.105s/final13.470s/static/4 semantic faults pass; original tools42/structured58 and separate91 probes zero skipped. Full verbose ./... remains red:64 durable groups853.852s,25 other tested packages pass, no panic/Go timeout/exclusions. Prior outer900-second aborted command has no completed durable result. [Exact scope](canonical-builtin-execution.md#live-tool-round-mode-and-canonical-round-consumers).

Latest boundary checkpoint: public same-commit atomic/lifetime consumers and fresh final/postTools host queue modes after hooks; red2.105s, combined3-shuffle29.235s/expanded80.323s/final31.650s/static/5 faults. Original inbox37/submit-in-commit23/structured58 and separate91 probes zero skipped. Full verbose ./... remains red:64 durable groups940.620s,25 other tested packages pass, no panic/timeout/exclusions. Two new queue/reporter fixture failures corrected afterwards; no post-correction full run. [Mappings and limits](canonical-builtin-execution.md#canonical-atomic-boundaries-and-live-queue-modes).

Latest submission abort checkpoint: canonical record/settlement authority and exact first-position inbox splice; source-derived native red0.370s plus independent original-runtime probe. Partial/queued abort consumer uses public records. Expanded3-shuffle37.265s/static/4 controls, original submissions11 and separate92 probes pass zero skipped. Full verbose ./... remains red:61 durable groups835.670s,25 other tested packages pass, no panic/timeout/exclusions. Extra Withdraw API/four legacy consumers remain unaccepted. [Exact scope](canonical-builtin-execution.md#canonical-submission-abort-and-positional-inbox-removal).

Latest model request consumer checkpoint is test-only: current metadata/API with committed reference/options; compact request assertions retain large-header HTTP/security/deduplication checks. Metadata3-shuffle1.513s/combined4.928s/expanded29.804s/static/3 controls, original recovery9/generation26 and separate92 probes pass zero skipped. Full verbose ./... remains red:60 durable groups862.069s,25 other tested packages pass, no panic/timeout/exclusions. Uncertain acknowledgement failure recurs and remains unresolved. [Exact scope](canonical-builtin-execution.md#model-metadata-and-request-reference-consumers).
