# Biggest deviations and path to zero

The largest remaining gaps affect record interoperability, caller-visible ordering and public API semantics. Full upstream alignment is unverified. This review uses native runtime `44a581c37a63ad9452c6729aa4b2d5aaf236a1a2` and pinned upstream `f9a26365568127461c47a07a37dcb42c140a3ac5` (v1.5.0).

## Ranked remaining work

| Priority | Deviation and current evidence | Route to closure |
| --- | --- | --- |
| P0 | **Builtin records break normal stored observation — D09/D16.** A fresh real Harness run publishes generation version1/private queued state, then ends its observer with `listener_error: task record input is absent` on the tool-bearing batch. Assistant/tool publications are lost. Source publishes generation2 and tool3 without ending the observer. Raw canonical generation/tool storage writes are also refused. | Port canonical durable input, checkpoint, version and result unions through creation, phase progress, migration, publication and reopen. Source model-tool input includes `{kind, assistant, callId}`; nested tools have a distinct union. Keep exact report/observer identity. The RED gate below must pass, followed by full model/nested/error/abort/restart assertions. Changing version numbers alone cannot close this. |
| P0 | **Between-part caller continuations — D04.** Production writer `hop` is still empty. Installed storage-scope completion and host dispatch do not let an earlier caller continuation fail the Session before a later grouped publication, as source requires. | Implement an actual continuation/publication scheduling bridge. Port complete source group cases, especially continuations that issue failing reads or fail the Session between parts. Assert no later publication and exact failure identity. Sleeps and runtime.Gosched are insufficient. |
| P0 | **Public Storage/API and database interoperability — D10–D13.** OpenSession requires private nativeCore; writer settlement bypasses wrapper Commit authority. Storage reads expose private envelopes, the SQLite schema differs, and Commit returns a sequence instead of its callback result/options contract. | Separate canonical stored records from execution adapters; implement all source Storage methods and result unions. Admit conforming wrappers through the primary Session boundary, including holds/refusals/scopes before delegation. Replace incompatible primary APIs. Add explicit database-format detection and tested, non-destructive import/migration paths before claiming original-file interoperability. Preserve writer ownership and existing data. |
| P1 | **Read-only external viewers — D27.** Native Session/Harness options lack the source readOnly mode; SQLite owns an exclusive native writer path. | Add viewer admission, no-scheduling/no-recovery-write policy, ReadOnlySession errors, safe concurrent SQLite reads/refresh and correct close. Port the entire read-only corpus: opening/closing must not advance sequences or mutate running work, while externally driven changes remain visible. Do not remove writer locks. |
| P1 | **Remaining dependency/lifecycle/readonly contracts — D02/D06/D14–D21.** Shared readonly handles, snapshots, watches and public observers are installed. Exact scheduler dependencies, wrapper fallbacks, task-bound warm reads, cache/version/GC lifetime, cancellation phases and complete recovery/hook/telemetry assertions are not accepted as whole contracts. | Enumerate and execute the remaining assertions through real public seams and actual signals. Narrow whole-graph barriers only after candidate-state ownership/graph proofs. Use real crash/reopen and GC controls, preserve source numerical/resource domains and verify every late result/close/failure boundary. |
| P1 | **Schema, codemode and provider dependencies — D22–D24.** The reachable-schema pattern-compile prototype is explicitly unwired; host Node/QuickJS RegExp grammars differ. Full worker/import/replay and inherited AI contracts lack complete acceptance. | Finish source compiler reachability, dynamic/annotation paths and host grammar; wire the accepted compiler into validation. Execute exact worker/import/cancellation/replay and provider transport fixtures. Close required environment skips and test source-visible values; no broad Go-adaptation waiver. |
| Release gate | **Assertion-level evidence — D26.** All165 durable paths are inventoried (101 test files), but file dispositions are historical and the assertion audit is incomplete. Native green suites cannot close this. | Map every required assertion to exact native execution, a reproduced failure, or the narrow approved exclusion. Run full regression, shuffle/deterministic/fault/static/security/reproducibility and CPU/alloc_space/alloc_objects checks; tune measured outliers and delete captures. Complete public-consumer and final-candidate checks. |

P0 means a core compatibility boundary, not a measured incidence or implementation estimate. The register contains29 mixed-progress groups:14 confirmed groups,10 unverified areas,3 bounded fixed groups,1 verification gap and1 approved product exclusion. These labels are not a parity percentage or29 independent fixes.

## Fresh builtin acceptance baseline

The new source-derived probe uses an actual upstream Harness, installed tool, scripted provider, submission wait and public stored observer. It establishes:

- Generation: version2, empty object input, initial `{status:"pending", checkpoint:{phase:"prepare",attempt:1}}`.
- Model tool: version3, input `{kind:"model",assistant:<actual assistant entry ID>,callId:"c1"}`, initial `{status:"pending",checkpoint:{phase:"call"}}`.
- Continuous stored observation until the unique failed-with-session-closed ending.

Reference runner:145 unchanged pinned tests plus32 independent source probes pass. Those32 reference probes are not32 passing native ports: the new paired native builtin test is RED. Its six failing assertions include wrong generation version/state, early listener_error, missing assistant/tool publications and the wrong final ending. The runtime task itself settles, so this is a public record/observation incompatibility despite a successful normal tool run.

Run the native acceptance gate from the repository root:

```sh
scripts/check-durable-builtin-publications.sh
```

It exits nonzero on the current runtime. The complete assertions live in `durable/testdata/upstream-main/pending/builtin_publication_contract_v150_test.go`; the script compiles them through a disposable overlay and removes its scratch. It neither skips nor changes expected values. This is a source-derived probe, not a translated named upstream test. Promote it into the ordinary suite once the runtime passes all assertions. The ordinary native suite currently does not execute this test.

## Work already installed

The independent writer/admission queue, document read keys, first-failure/outcome fencing and256-job bound are installed. Shared immutable conversation/document snapshots, watches and observers retain adopted identity. Public stored observers dispatch after completed storage scopes. Cold builtin hydration and LiveDoc v3 are installed; compaction now uses `pi.compaction`; application `pi.*` records execute and round-trip through SQLite. Original message kinds/model fields are produced: the former pending normal-entry shape test passes live and after reopen.

Fresh count3/shuffle42 verification also passes normal entry shape/reopen, opaque checkpoint migration, unknown-phase faulting, canonical compaction publication and raw pi-namespace storage/reopen. Those passes correct obsolete register diagnoses; complete message/receipt and lifecycle coverage is still open.

Last full runtime verification at44a581c: durable412.725s/tools15.198s, repository-wide vet/Staticcheck/logging pass, plus30 shuffled compaction/application/storage/registry controls. This review changes probes, gates and documentation; it does not provide a new full-runtime or release-profile receipt. The retained reference snapshot has no .git: the runner verifies pinned hashes and complete test/support trees, but does not independently reverify commit identity.

## Sequence to zero

1. Freeze canonical record/API assertions first. Use the RED builtin gate to drive the generation/tool/nested model replacement, then remove raw builtin-write refusal and finish public Storage/query/callback contracts. Preserve exact source unions and opaque data; do not route source data through inferred private checkpoints.
2. Finish the authoritative Storage/wrapper boundary and grouped continuation bridge. Run the full failure/deferred-scope/read-dependency corpus through that boundary, with real before-delegation holds and failure-at-entry controls.
3. Add read-only SQLite viewers and finish cache/version/lifetime/cancellation, scheduler and recovery contracts. Review and update the assertion crosswalk alongside each verified slice.
4. Close schema/engine/AI dependency contracts and every required assertion gap. Re-run all integration surfaces after representation/API changes; source tests remain the authority.
5. Run final release verification and measured profiling/tuning. Zero means no unapproved semantic deviation, missing required export, unmapped assertion or required skip, with a clean verified candidate and retained concise evidence. D25 remains the explicitly authorised SQLite-only exclusion; zero within that scope does not restore original Memory/JSONL/IndexedDB constructors.

No external service blocker is identified. The difficult work is internal architecture and verification; no completion date is established. New source review may reveal additional gaps.
