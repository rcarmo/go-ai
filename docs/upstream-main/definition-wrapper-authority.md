# Definition wrapper authority and dependency audit

Definition projections now use the current adapter Snapshot instead of a cached native value. Fifteen failing backend cases exposed stale values in `SnapshotDefinition`, typed watch baselines, retained `DocumentState.Value`, transaction acquisition and unmigrated definition reads. The failures occurred on memory, journal and SQLite without changing the sequence, token or document ID.

## Cache boundary

`Session.definitionValue` checks definition identity and policy first. Only exact native stores can reuse or admit the process-local migration cache. Adapter reads copy their projected value, run any required migration, validate and bound the result, and return a detached copy. They neither refresh nor replace an existing native cache entry nor arm its retention timer.

Native migration-once behaviour remains: switching back to the native store recovers its unchanged warm value without rerunning migration. Adapter migrations run on each read because the adapter supplies no value-generation witness. Stable projected values are also rederived; this slice establishes data/error authority, without establishing source-wide migration callback frequency for arbitrary adapters. No new adapter protocol or public API was added.

`durable/definition_wrapper_main_test.go` covers:

- Two different projections at an unchanged sequence through all four migrated APIs, plus current-version SnapshotDefinition reads.
- Detached caller mutation, authoritative absence and nonfatal adapter request-error projection.
- Transaction migration followed by callback refusal: no projected base is persisted.
- Returning to native storage without contaminating or losing its warm cache.
- Initially wrapped storage with no cache admission/timer, callback errors, invalid migration values and recovery on the next valid read.

The original production code failed 15 backend leaves. After adding the admission/error controls, a disposable overlay restoring the original `document_definition.go` failed all 18 backend leaves. Overlay and downloaded source files were removed after analysis.

## Bounded dependency audit

Reviewed slices at upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`:

- `packages/durable/src/session/keys.ts`: table write keys and the complete `GatedStorage` read mapping.
- `session.ts:425-432`: keyed reads wait for a writer gate; `ALL_KEYS` drains the writer. `session.ts:1090-1138` passes transaction write keys to storage publication; `1262-1288` stores held document changes before releasing holds.
- `transaction.ts:850-905`: held document adoption and write keys. Document content touches address and incarnation keys; creation/retirement also touch scope keys.
- The declaration/assertion locations in `harness-group-commit.test.ts`, including the unpublished same-request submission and pending-owner membership cases, were cross-checked against the existing group-contract review. No new source execution was performed.

These native paths need explicit dependencies before preparation can overlap an unpublished writer:

| Native path | Required dependency or unresolved barrier |
| --- | --- |
| `Tx.currentDocument`, document handles | Incarnation identity/value; staged reads also depend on the transaction overlay. A cache key alone cannot witness an adapter value. |
| `findDocumentState`, typed acquisition/absence | Logical address, including an absent lookup. Creation/retirement must invalidate address and scope readers. |
| `TypedEntry`, `visibleHistory`, context bounds | Entry identity plus conversation ancestry/cutoffs and appends; immutable fork ancestry does not remove the dependency on unpublished conversation creation. |
| `PutTask`, owner validation and scheduler terminal checks | Task records plus live-work/reservation/actual-host membership. A task-ID-only footprint cannot protect a pending owner end from newly admitted descendants. |
| `Tx.finalizeTaskWrites` | Terminal-owner document scopes and new-owner decisions. It conditionally reads candidate state, then enumerates documents for terminal cleanup; it is not an unconditional global scan for every append. |
| `syncGenerationStatuses`, `syncCompactionStatuses` | Changed task state and the affected conversation's live-document address/value; these implicit writes belong in the footprint. |
| `ForkConversation`, `rejectCurrentForkWrites` | Parent/ancestor visibility, document scopes and current-policy parent writes, including same-commit new lifetimes. |
| Submission/request deduplication | Both submission identity and conversation/request key; an absent request must conflict with its unpublished creation. |
| `prepareWatchFrames`, `prepareSubscriptions` | Affected documents for typed frames. Commit subscriptions currently retain full candidate snapshots and enforce whole-snapshot byte budgets, requiring broader publication/dependency handling. |
| `finalizeDocuments` and host callbacks | Base value/version/delta count, selected operations and required migrations/checkpoints. Conflicts must be resolved before a host callback; rerunning callbacks automatically could duplicate effects. |
| Ownership, health, ID reservations, settings/registry/host state | Separate admission/lifetime fences. Storage record keys cannot replace these process-local contracts. |

The current global published-sequence and raw-baseline checks remain in `settlePreparedPublication`/`commitPartsAt`. No keyed recorder, gate, disjoint rebase or retry was added. Native final preparation still validates against a complete candidate; narrowing only explicit point getters would miss implicit writes, negative lookups, scope phantoms and host membership. Wrappers must retain their own Snapshot data/errors throughout any future split.

## Validation

Go 1.27.3, GOMAXPROCS=4:

- New wrappers plus definition/document controls: normal three repetitions, shuffle 42, CGO 0 and 1.
- New wrappers, definition/document and published legacy/state controls: race three repetitions each, shuffle 7 and 42, CGO 1.
- Full root normal tests, vet and pinned staticcheck v0.8.2: CGO 0 and 1. Module verification passes.
- Full durable CGO-1 race, count 1, timeout 1200s: 654.749s, 4,349 named passes (25 under the new wrapper tests), zero failures and zero unfinished tests. Seven exact-worker Codemode process cases skip without the opt-in installation.

Raw JSON events were counted without retaining a log; owned empty credential homes and disposable overlay/source data were removed. No extensive profiling or performance comparison was run under Rui's direction. Suite workloads differ from earlier checkpoints. Dependencies, formats, accepted assets and release refs are unchanged.

Keyed/global overlap, grouped failure/drain/continuation semantics, deferred persistence, complete source test mapping and full semantic acceptance remain open. The source session-definitions test row retains its pending full-review disposition; these native adapter regressions do not complete that source file's review.
