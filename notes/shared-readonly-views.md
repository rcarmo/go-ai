# Shared readonly conversation views

D14–D17 are open. `ConversationWatch.Value()` and callbacks still deep-copy mutable `ConversationView` fields. Observers have separate document projections.

## Pinned contract

Authority: `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/src/harness/view.ts` and `test/harness-view.test.ts`.

- Source160: a document-only revision preserves the entries array and unchanged document objects; an append preserves the docs object.
- Source330: concurrent observers share docs identity. Stopping or failing one observer leaves its peers attached.
- Source525: observer windows differ independently, share docs, and retain entry object identity through trimming.
- `ConversationViews` keeps one mount per conversation until its last observer detaches. Each observer keeps its own window.
- The source uses TypeScript readonly types and Chord's persistent `applyImmutable`. No runtime freeze/proxy enforcement was found in the inspected implementation.

## Implemented groundwork

`1064250` adds persistent immutable operation replay. It preserves unchanged child nodes; it does not connect them to conversation views.

`durable/immutable_json.go` adds logical JSON encoding for `ImmutableValue`. Before this change, the native bounded encoder and `encoding/json` returned `{}` for a frozen object. The new tests first demonstrated that failure. Encoding now traverses private nodes directly, without a detached whole-tree copy, and charges the same logical node/depth budgets as ordinary JSON. Byte/member limits and rejection of caller marshal authority are tested. Cloning a frozen input produces detached mutable JSON.

These are native integration regressions, not translations of the complete upstream view tests. Public sharing assertions still need the production watch path.

## Next integration

1. Replace public copy-based view payloads with readonly nodes while retaining canonical conversation/entry wire shapes. Keep legacy event projections separate where their contract differs.
2. Install one shared conversation mount. Apply document changes once; retain docs identity for entry-only revisions.
3. Give each observer its own persistent entries node and bounds; preserve retained entry nodes on append/trim.
4. Exercise initial `Value()`, callbacks, later attachment, independent stop/failure, overflow/reset, deferred memory publication and bounded cleanup. Keep existing resource/operation assertions.

## Verification

Final development run: full durable399.178s/tools18.031s; repository-wide vet/Staticcheck, logging and diff checks passed. The pinned oracle runner passed95 unchanged tests and5 source probes. This is development verification; acceptance profiling and the complete assertion crosswalk are outstanding.
