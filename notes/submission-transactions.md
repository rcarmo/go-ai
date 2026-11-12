# Submission creation and assembly

Submission transactions now use the pinned creation map and ordered change list. Placement and settlement select their current record during assembly, before document predicates. This replaces the native immediate-mutation flow; it does not establish full runtime parity.

Target: pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. Previous checkpoint: `c818ce1`.

## Ported source

- `Transaction.createSubmission` at `src/session/transaction.ts:513–523` first requires the conversation, then mints and copies a complete record. Go `CreateSubmission(SubmissionCreate)` now returns `SubmissionRecord`, not an ID. Callers use the returned `.ID` when they need only identity.
- `SubmissionCreate` omits caller-supplied identity. `SubmissionSettlement` contains only terminal status, answer, reason and detail; it cannot replace entry, type, request metadata or identity. `SubmissionRecord.Value` is removed, including from handle status results. Public callers can no longer supply or inspect the historical builtin payload through these types.
- A caught failed creation still sets the source table-write fence. A missing conversation fails before minting an ID. Creation detail and trace are copied; the returned record and committed record retain the original input.
- `placeSubmission` and `settleSubmission` at `transaction.ts:526–540` append owned changes without looking up their target. Missing-record and invalid-transition errors occur after the callback during assembly. Caller table reads remain committed and fenced; staged records are not exposed through getters.
- `applySubmissionChange` at `transaction.ts:71–83` is ported directly: terminal records stay unchanged; placement requires queued and turns input placed or write done; an answer requires placed. Changes resolve against all creations in the transaction, then committed state. The first terminal result survives later changes. Existing terminal records produce no rewrite or sequence advance.
- Ordered creation/change assembly at `transaction.ts:1032–1040` is installed before document checkpoint predicates. Changes may precede their target's creation. Unpersisted/foreign answer IDs do not acquire the old native visibility policy.

The native SQLite envelope still holds historical builtin input/result data. Assembly retains that private data when updating an existing envelope, without exposing it in public records or accepting it through creation/settlement. Direct canonical builtin execution and original database format interoperability remain separate work. Internal trace selection now reads the staged creation map rather than the old write list; this is not a caller table-read bypass.

## Tests before implementation

New controls failed against the previous runtime:

- Missing placement/settlement records were looked up inside the callback.
- Creation accepted a missing conversation until later validation and minted an ID.
- Creation returned an ID instead of the complete record.
- A terminal continue was rejected by placement before the terminal guard.
- A valid opaque answer ID was rejected as not visible.

The unchanged original `harness-submissions.test.ts:228–266` current-record settlement case now has a literal native packet inside the consolidated transaction suite. It preserves queued-input/write answer rejection, missing-target rejection, same-commit created-record settlement and first-terminal-wins assertions. The extended existing ownership/null-detail/lifecycle controls remain alongside it.

Source-derived controls cover failed-write fencing, complete creation records and absent native fields, future-creation lookup, terminal no-op sequence behaviour, opaque answer IDs, and submission validation before document predicates. Three independent reference probes exercise creation/copy/fence order, deferred errors/terminal no-ops, and later creation/predicate order.

Native-only tests were reconciled explicitly:

- Caller-assigned ID, compatibility `Value`, and settlement `Entry` inputs are no longer expressible by the public argument types. The surface test checks their absence. Strict-JSON authority coverage uses the source detail field instead of a private payload field.
- Valid foreign/unpersisted creation and answer references now succeed. Raw/native wire validation still rejects malformed payloads; it is not claimed as source admission policy.
- Public status payload checks now inspect canonical fields. Private-envelope checks stay on explicit snapshots/legacy settlement envelopes. No role/payload normaliser establishes source acceptance.
- Transition failures now use the original missing/not-queued/not-placed error text and assembly timing instead of asserting native `StorageRejected` classification.

## Development verification

- Full durable **406.961s**, tools **17.201s**.
- Ten focused shuffled repetitions, seed42: **9.995s**.
- Repository-wide vet, Staticcheck v0.8.2 and logging pass.
- Seven compiler-overlay faults fail semantic assertions: early target lookup, omitted assembly, disabled terminal guard, ignored creation map, restored answer visibility, omitted failed-write fence, and document predicates before submission validation. Disposable overlays/scripts are deleted.
- Reference: **166 unchanged source cases +46 independent probes**. This is158 tests from eight original files, eight unchanged Storage conformance cases,35 admission probes and11 Storage/transaction probes. Hashes and the complete test/support tree are checked; commit identity is not independently reverified because the retained snapshot has no `.git`.

The first full run exposed the internal trace selector's stale write-list lookup. Reading its own staged creation map fixes that dependency; the subsequent full run passes. The earlier queued progress/close error-order caveat remains open under D20.

No test files were added for this slice: related assertions remain in the version-neutral transaction/submission suites. These are development receipts, without race coverage or pre-release CPU/allocation acceptance. Complete Harness admission ordering, canonical builtin execution/migrations, document APIs, callback result/options, D04 continuation turns, lifecycle/readonly/viewer coverage and full assertion/release gates remain open.
