# Shared readonly document snapshots

Current and historical typed snapshots now return `ReadonlyValue` across Session, Harness, conversation handles, TaskRuntime, InvocationReader and ToolAPI. This completes another D17 slice; asynchronous document watches, mounted builtin cross-surface identity, version/cache-pressure lifetime and stored-commit dispatch still need acceptance.

## Pinned authority and independent probes

Target: `f9a26365568127461c47a07a37dcb42c140a3ac5`.

- `session/session.ts:594–612` returns the loaded tracker's actual value, including the same-version no-enqueue branch.
- `session/session.ts:744–775` independently materialises historical snapshots; it does not reuse the current address's tracker.
- The new source probes in `notes/oracles/session-admission.upstream.test.ts` verify snapshot/observer identity, unchanged child identity, unchanged required-migration-base identity, zero-retention held identity, and identity after storing an already-adopted hold.

All four new reference probes passed before their corresponding native contracts were credited. Native regressions first failed for mutable copied snapshots, zero-retention held identity, and held-store identity. The Go held-value scenarios use the existing controlled-clock text-document fixture with an infinite persistence window; the reference probes use a numeric counter. Both exercise the real deferred commit and public snapshot paths.

## Implementation

The bounded definition cache owns readonly nodes. Mutable internal transaction boundaries request a detached conversion. Warm public snapshots return the adopted handle directly, retaining the existing cancellation/admission bypass. A migration base stored without edits keeps the materialised handle.

Receipt and memory adoption retain previous nodes, update metadata, then install persistent changed paths before observers run. Synchronous document observations reuse those installed nodes. Historical snapshots return readonly values from independent materialisation.

Held documents own their readonly node separately from idle retention. They keep it at zero retention, and warm reads retain that same node in the bounded idle cache when permitted so storage settlement does not create another logical revision. Cache byte/page limits remain enforced. Adapter projections bypass native readonly cache authority, preserving existing wrapper controls.

Content-oriented legacy tests explicitly call Copy through test helpers. The new identity tests call the public readonly methods directly. This is the sole public typed-snapshot contract, not an optional parallel API.

## Verification

- Focused snapshot identity functions: ten shuffled repeats, seed42.
- Definition/deferred/observer/cache/restart controls: three shuffled repeats.
- Full durable535.625s/tools18.078s; repository-wide vet/Staticcheck pass. The full run preceded only the final infinite-window fixture parameter strengthening; those cases pass the focused gate.
- Reference runner:117 unchanged pinned tests plus9 probes pass. Upstream source/test hashes remain checked and no upstream assertions were edited.

A repeated restart-control run exposed a pre-existing fixture race: `AbortTask` enabled scheduling before a synthetic interruption could be seeded, occasionally replacing already-admitted lifecycle timestamps. The fixture now seeds only the abort mark, as its adjacent rejection control already does, and asserts scheduling remains disabled. All cutoff/abort-handler assertions remain;50 shuffled repeats pass. This is a fixture correction, not a scheduler fix.

## Remaining contracts

- Public asynchronous document watches now return shared readonly revisions; see readonly-document-watches.md for the subsequent slice.
- Mounted builtin view documents need the same materialisation authority across snapshot, observer and view APIs.
- Version switching, active pins under cache pressure, reload/eviction and weak lifetime assertions need complete source coverage.
- Task-bound warm-read admission/cancellation requires its separate source audit.
- Public stored-commit observation still needs canonical changes, dispatch-time membership, publication-turn and waiter/abort ordering. The private stored guard is not that API.
- Acceptance profiling and the full assertion crosswalk remain outstanding.
