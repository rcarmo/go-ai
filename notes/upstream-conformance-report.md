# Upstream conformance completion report

**Overall migration: incomplete. Full parity and release acceptance are not established.** This report closes a verified development slice, not the complete upstream port.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `/work/go-ai/durable`, authorised SQLite-only, pure Go. The preceding pushed implementation checkpoint is `04726f3` (request-time model/retry authority); image-resource diagnosis is recorded at `1e0f3ad`.

Latest continuation: public persistence options on all five commit APIs, with deferred memory/raw SQLite, fold/mixed-write, before-admission capture, Number windows and validation/health precedence controls. See [evidence](commit-callback-values.md#public-commit-persistence-options). Complete persistence lifecycle/order, Tx constructor record results, full API/worker/runtime/Storage and release acceptance remain open.

## Earlier queue-production checkpoint

- Ported the full atomic busy-steer assertion packet: the host checks Live, writes its own document, admits input, then appends its own entry in one stored commit. Sent IDs, the entire Inbox item and the active run's original inputs match the pinned assertions.
- Replaced private queued input envelopes with canonical `content` and optional `entry` metadata. Block input and explicit-null data retain their original shape.
- Queued passive writes now contain a canonical EntryDraft. The same encoder supplies compaction-write queue production; placement IDs, task attribution and the private Value field are excluded.
- Verified idle input requests progress itself, without Resume or Submission.Wait masking an omitted admission wake-up.
- Verified failed-run continuation inside a caller transaction, its write fence and subsequent host entry.
- Added a definite SQLite failure/reopen control: failure identity survives and no submission, task, entry or run survives reopening. This is not complete arbitrary/uncertain Storage-failure acceptance.
- Verified canonical queued model-visible writes and eventual placement/completion. This fixture holds a provider, not the original tool boundary; that source variant remains open.

The exact input/write shape tests failed before the fixes. Idle progress and continuation already passed and required no speculative runtime changes. A failure/reopen fixture initially tried to close storage already owned/closed by Session; correcting the fixture required no runtime fix. Tests remain in the existing version-neutral transaction suite.

## Latest public persistence verification

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
| Submission/lifecycle — D20/D21 | Original tool-bound write placement, arbitrary/uncertain Storage failures, trace/link/span propagation, task-bound lifetime/close and exhaustive resume/scheduling assertions. The earlier queued progress/close error-order caveat remains open. |
| Readonly/document/view lifetimes — D14–D17 | Complete mount/collection/cache/reload/lifecycle and observer-dispatch acceptance, beyond the mapped passing identity assertions. |
| Resources, ownership and external viewers — D18/D19/D27 | Complete resource/numeric/ownership/race corpus and read-only concurrent SQLite viewers. |
| Codemode/schema/provider — D22–D24 | Complete worker/restart, dependency/compiler/RegExp and inherited AI/provider acceptance. |
| Final verification — D26 | Full assertion-level crosswalk, complete regression/fault evidence and pre-release CPU, alloc_space and alloc_objects analysis/tuning. |

D08, D28 and D29 are recorded as fixed; other groups contain partial progress, confirmed deviations or unverified obligations. Alternative storage engines remain outside the user's authorised SQLite-only scope (D25). Neither those status labels nor green development suites define a completion percentage.

The next acceptance work is the remaining submission tool/trace/lifetime packet and canonical builtin execution/migrations. The complete register, including causes, evidence and acceptance criteria, remains authoritative: [deviations](../docs/upstream-v1.5.0/durable-deviations.md), [structured gaps](../docs/upstream-v1.5.0/durable-parity-gaps.json), [assertion crosswalk](../docs/upstream-v1.5.0/task-table-assertions.tsv). Detailed slice receipts are in [submission admission](submission-admission.md).
