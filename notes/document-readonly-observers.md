# Readonly document observers

Public `ObserveDefinition` now returns sealed `ReadonlyValue` handles and delivers `DocumentCall`. D16/D17 remain open for cross-surface materialization/cache identity, exact cancellation causes, lifetime/version reload and stored-observer dispatch.

## Pinned authority

`f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/test/session-observers.test.ts:132–159` requires the retired call's value to be identical to the attachment value. It also checks absence on a new attachment after retirement and a whole-value replacement on recreation. Source Session guard/adoption code decides callback order and ownership.

An initial same-commit native retirement/recreation probe failed the identity assertion because public baseline and callbacks copied maps. The current `TestMainPublicDocumentObserverRetirementIdentity` strengthens that probe to the literal two-commit source scenario, including count2, absent reattachment, count9, replacement operations and retirement identity. The initial failure was a source-derived identity probe, not execution of the entire unchanged upstream scenario.

## Implementation

- `DocumentObservation.Value` is readonly. `DocumentCall` has the source value-call fields: kind, value, ops, retired.
- Active observations of the same incarnation/version share their current readonly node. Publication computes one node per document change and shares it across public observer guards.
- Persistent replay preserves untouched child nodes. Retirement carries the prior node; recreation installs a whole new value.
- Attachment captures the initial public value under the Session line. Spent guards clear their current readonly node without retaining a separate initial-value reference.
- Mutable data requires explicit `Copy`. Private trusted guard controls keep their existing internal WatchFrame adapter; the public API has one readonly contract.

The shared revision cache currently spans active synchronous document observers. Current/historical typed snapshots now share readonly materialisation with synchronous observers; see readonly-document-snapshots.md for that subsequent slice. Asynchronous document watches now share prepared readonly revisions; see readonly-document-watches.md. Full source cache/reload/eviction identity is not established.

## Verification

Public document observer functions pass ten shuffled repeats (seed42), including stored/deferred publication, retirement/recreation, self/peer detach, error/close, queued cancellation and task-owner retirement. The new persistent ownership control checks shared baseline/revision identity, unchanged child identity, late attachment and detached-copy isolation.

Full native development run: durable401.912s/tools18.420s. It preceded the final source132 test-strengthening/comment changes; the strengthened cases pass the focused gate. Repository-wide vet/Staticcheck pass.

The reproducible runner includes all22 unchanged pinned session-observers tests:117 unchanged upstream tests +5 focused source probes pass. Upstream tests validate the reference contract; their success alone does not establish Go equivalence. No upstream source or assertions were edited. Acceptance profiling, fault/lifetime completeness and full release verification remain outstanding.
