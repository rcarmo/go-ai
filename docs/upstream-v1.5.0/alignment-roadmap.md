# Biggest deviations and path to zero

The largest remaining gaps affect record interoperability, caller-visible ordering and public API semantics. Full upstream alignment is unverified. The latest verified raw builtin storage slice is based on `26c11fe`; target upstream is `f9a26365568127461c47a07a37dcb42c140a3ac5` (v1.5.0).

## Ranked remaining work

| Priority | Deviation and current evidence | Route to closure |
| --- | --- | --- |
| P0 | **Builtin storage and incomplete projections — D09/D16.** Normal model-tool publication continuity and exact parallel/sequential state progression now pass. Generation2/model-tool3 projections use persisted attempt/cutoff/assistant facts; UTF-16 arguments survive readonly observation. Raw builtin admission, all state scans and SQLite reopen/reclaim now pass. Canonical builtin execution, private reads, nested/unoffered/migration/edge contracts are incomplete. | Replace private durable adapters with canonical ownership through creation, migration, execution, storage, publication and reopen. Retain the now-green tests and add independent nested/error/abort/recovery packets. |
| P0 | **Between-part caller continuations — D04.** Production writer `hop` is still empty. Installed storage-scope completion and host dispatch do not let an earlier caller continuation fail the Session before a later grouped publication, as source requires. | Implement an actual continuation/publication scheduling bridge. Port complete source group cases, especially continuations that issue failing reads or fail the Session between parts. Assert no later publication and exact failure identity. Sleeps and runtime.Gosched are insufficient. |
| P0 | **Public Storage/API and database interoperability — D10–D13.** OpenSession requires private nativeCore; writer settlement bypasses wrapper Commit authority. Storage reads expose private envelopes, the SQLite schema differs, and Commit returns a sequence instead of its callback result/options contract. | Separate canonical stored records from execution adapters; implement all source Storage methods and result unions. Admit conforming wrappers through the primary Session boundary, including holds/refusals/scopes before delegation. Replace incompatible primary APIs. Add explicit database-format detection and tested, non-destructive import/migration paths before claiming original-file interoperability. Preserve writer ownership and existing data. |
| P1 | **Read-only external viewers — D27.** Native Session/Harness options lack the source readOnly mode; SQLite owns an exclusive native writer path. | Add viewer admission, no-scheduling/no-recovery-write policy, ReadOnlySession errors, safe concurrent SQLite reads/refresh and correct close. Port the entire read-only corpus: opening/closing must not advance sequences or mutate running work, while externally driven changes remain visible. Do not remove writer locks. |
| P1 | **Remaining dependency/lifecycle/readonly contracts — D02/D06/D14–D21.** Shared readonly handles, snapshots, watches and public observers are installed. Exact scheduler dependencies, wrapper fallbacks, task-bound warm reads, cache/version/GC lifetime, cancellation phases and complete recovery/hook/telemetry assertions are not accepted as whole contracts. | Enumerate and execute the remaining assertions through real public seams and actual signals. Narrow whole-graph barriers only after candidate-state ownership/graph proofs. Use real crash/reopen and GC controls, preserve source numerical/resource domains and verify every late result/close/failure boundary. |
| P1 | **Schema, codemode and provider dependencies — D22–D24.** The reachable-schema pattern-compile prototype is explicitly unwired; host Node/QuickJS RegExp grammars differ. Full worker/import/replay and inherited AI contracts lack complete acceptance. | Finish source compiler reachability, dynamic/annotation paths and host grammar; wire the accepted compiler into validation. Execute exact worker/import/cancellation/replay and provider transport fixtures. Close required environment skips and test source-visible values; no broad Go-adaptation waiver. |
| Release gate | **Assertion-level evidence — D26.** All165 durable paths are inventoried (101 test files), but file dispositions are historical and the assertion audit is incomplete. Native green suites cannot close this. | Map every required assertion to exact native execution, a reproduced failure, or the narrow approved exclusion. Run full regression, shuffle/deterministic/fault/static/security/reproducibility and CPU/alloc_space/alloc_objects checks; tune measured outliers and delete captures. Complete public-consumer and final-candidate checks. |

P0 means a core compatibility boundary, not a measured incidence or implementation estimate. The register contains29 mixed-progress groups:14 confirmed groups,10 unverified areas,3 bounded fixed groups,1 verification gap and1 approved product exclusion. These labels are not a parity percentage or29 independent fixes.

## Builtin acceptance slice

The former RED real-Harness packet now passes as `TestMainBuiltinCanonicalPublicationsV150` in the ordinary suite. At `1eafca1` it failed six assertions, ending observation early on absent model-tool input. Generation2 and model-tool3 now expose the source initial states/input and retain observation until the unique close ending.

The added progression packet compares exact version/input/state unions for two parallel and sequential tools, with request attempt/cutoff and terminal model results. Adjacent identical states are collapsed; commit granularity and cross-record ordering remain open. A public UTF-16 regression checks task lookup/encoding and immutable stored observation without private carrier leakage. Retained source SQLite close/reopen evidence requires resend of the same request at attempt1.

Reference:145 unchanged pinned tests +35 independent probes pass. [The slice receipt](../../notes/builtin-task-publications.md) records baseline failures, runtime changes, test scope and remaining contracts. Run the ordinary native controls with:

```sh
source scripts/project-env.sh
make test PACKAGES=./durable TEST_FLAGS='-run "TestMainBuiltinCanonical|TestBuiltinTaskPublicProgression|TestReaderUTF16Main|TestM1bCloseJoinsNoncooperative" -count=1 -p 1'
```

[Raw builtin storage receipts](../../notes/builtin-task-storage.md) add definition-free admission, opaque/future records, terminal mapping/status scans and scheduler representation safety. Canonical source builtin execution and complete Storage conformance remain open.

## Work already installed

The independent writer/admission queue, document read keys, first-failure/outcome fencing and256-job bound are installed. Shared immutable conversation/document snapshots, watches and observers retain adopted identity. Public stored observers dispatch after completed storage scopes. Cold builtin hydration and LiveDoc v3 are installed; compaction now uses `pi.compaction`; application `pi.*` records execute and round-trip through SQLite. Original message kinds/model fields are produced: the former pending normal-entry shape test passes live and after reopen.

Fresh count3/shuffle42 verification also passes normal entry shape/reopen, opaque checkpoint migration, unknown-phase faulting, canonical compaction publication and raw pi-namespace storage/reopen. Those passes correct obsolete register diagnoses; complete message/receipt and lifecycle coverage is still open.

Latest full runtime verification for raw builtin storage: durable418.657s/tools18.326s; repository-wide vet/Staticcheck/logging pass;20 shuffled focused repetitions and three independent fault controls pass. This is development verification; release profiling and full acceptance remain open. The retained reference snapshot has no .git: pinned hashes and full test/support trees are checked, but commit identity is not independently reverified.

## Sequence to zero

1. Freeze canonical record/API assertions first. Keep the builtin publication/progression controls green while completing generation/tool/nested durable ownership, then finish canonical builtin execution and public Storage/query/callback contracts. Preserve exact source unions and opaque data; do not route source data through inferred private checkpoints.
2. Finish the authoritative Storage/wrapper boundary and grouped continuation bridge. Run the full failure/deferred-scope/read-dependency corpus through that boundary, with real before-delegation holds and failure-at-entry controls.
3. Add read-only SQLite viewers and finish cache/version/lifetime/cancellation, scheduler and recovery contracts. Review and update the assertion crosswalk alongside each verified slice.
4. Close schema/engine/AI dependency contracts and every required assertion gap. Re-run all integration surfaces after representation/API changes; source tests remain the authority.
5. Run final release verification and measured profiling/tuning. Zero means no unapproved semantic deviation, missing required export, unmapped assertion or required skip, with a clean verified candidate and retained concise evidence. D25 remains the explicitly authorised SQLite-only exclusion; zero within that scope does not restore original Memory/JSONL/IndexedDB constructors.

No external service blocker is identified. The difficult work is internal architecture and verification; no completion date is established. New source review may reveal additional gaps.
