# Raw builtin task storage

Raw `Storage.Commit` now accepts canonical generation/tool records through the generic task envelope. Storage does not infer builtin input/checkpoint fields or require executable versions. This removes the two remaining kind-based refusal gates; canonical builtin execution still needs its runtime adapters.

Target: pi-durable v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`. The source storage conformance corpus replaces complete records and scans each public state. MemoryStorage stores task records by ID and indexes `state.status`, without builtin definitions. The independent reference probe runs the shared nine-record fixture through original MemoryStorage and SQLite, checks exact records and status scans, then reopens SQLite.

## Changes and test-first failures

- `storage_public.go` no longer refuses `pi.generation`/`pi.tool`. Core task validation also removes the separate kind ban and applies private generation validation only to private checkpoint records.
- Terminal public records map their outcome to the private terminal status while retaining the complete public state union. The old code passed `terminal` into a private status field that accepts only `done`/`failed`/`aborted` terminal forms.
- Task scans use the canonical native envelope's `State.Status`; private terminal records also match the source `terminal` filter. Previously scans compared the private terminal status and returned no terminal records.
- Private scheduler checkpoint access now requires its representation witness as well as its kind. Canonical records follow generic definition/version admission. A future builtin with no installed definition remains blocked, with no private reinterpretation or storage mutation. This native composition safety control exposed a nil-pointer panic after the raw admission ban was removed; it now passes.

`TestV150RawBuiltinStoragePreservesCanonicalRecords` first failed with only the root batch stored and the builtin batch refused. After admission repair, the terminal scans failed at live read, reopen and reclaim/reopen. The final packet retains pending, running, sleeping, waiting, completing and terminal unions; future builtin versions; nested input; null/false/zero/empty-string inputs; opaque primitive/array/null checkpoints; user data resembling private presence wrappers; and held/terminal results/errors. The records are not executed by this storage test.

The native read API still returns private `Task` values. Exact comparisons use the production `CanonicalTask` projection, not a test-specific normaliser. Replacing Storage's read API remains D12. This packet establishes raw admission/storage/reopen and public projection for its records; it does not establish source database format interoperability or canonical execution.

## Verification

- Reference:145 unchanged pinned tests +35 independent probes pass. The new shared fixture passes original MemoryStorage and SQLite, including reopen. The native engine remains SQLite-only.
- Full native durable **418.657s**, tools **18.326s**.
- Raw storage/composition, public storage and builtin publication/progression controls:20 shuffled repetitions, seed42, **43.930s**.
- Independent compiler-overlay faults restoring the core kind ban, removing terminal mapping or suppressing terminal scans each fail the new semantic assertions. Overlays and logs are deleted.
- Repository-wide vet, Staticcheck v0.8.2 and logging pass. Register, formatting and diff checks pass.

Development verification includes no race coverage or pre-release profiling. Full CPU/alloc_space/alloc_objects analysis and measured tuning remain required at release acceptance.

## Next boundary

Canonical builtin records are stored reversibly for these fixtures, but current Harness registrations do not execute source GenerationTask2/ToolTask3 from these generic records. The historical adapters still own normal Harness creation/execution. Replace those adapters through source-derived creation, migration, recovery, publication and reopen assertions. Nested/unoffered calls, complete edge phases, optional presence, UTF-16 raw decoding/storage and original SQLite schema/API contracts remain open. Raw Storage replacement/reference policies also retain native runtime constraints; the complete conformance corpus is not accepted.
