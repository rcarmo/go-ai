# Shared readonly conversation views

Public view watches now return shared readonly values. D14–D17 remain open because wrapper/lifecycle acceptance, other observer surfaces and the complete assertion crosswalk still need work.

## Pinned contract

Authority: `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/src/harness/view.ts` and `test/harness-view.test.ts`.

- Source160: a document-only revision preserves the entries array and unchanged document objects; an append preserves docs identity.
- Source330: concurrent observers share docs. Stopping or failing one observer leaves its peers attached.
- Source525: observer windows differ independently and share docs.
- The source drops its conversation mount after its last observer detaches. Each observer keeps its own window.
- TypeScript readonly types and Chord persistent replay supply the source ownership contract. No runtime freeze/proxy was found in the inspected implementation.

## Public migration

`ConversationView.Entries` and `.Docs` are sealed, comparable `ReadonlyValue` handles. Their concrete handles are private values, so callers cannot overwrite a shared node through an exported pointer. Nested accessors retain this representation. Keys and explicit `Copy` results are detached.

The watch accessor and callbacks use this representation directly. Existing subscriptions own the shared documents: late attachment reuses adopted docs, publication preparation computes one readonly document revision per conversation, and the last subscription's removal releases internal mount ownership. Each observer builds its own entries array while reusing retained entry nodes.

`ConversationView.Copy(limits)` explicitly returns mutable `ConversationViewCopy` data. Legacy content/operation tests request that conversion; public identity assertions bypass the test conversion helpers. Legacy agent event snapshots keep their independent mutable projection.

Public view entries and append operations now use the public record codec, including `conversationId` and excluding native storage `conversation`, `seq` and `position`. A new regression first demonstrated the native-envelope leak. SQLite envelopes retain their existing codecs.

## Verification

Eight `TestMainSharedView*` functions pass five shuffled repetitions (seed42). They cover source160/330/525 identity, empty entries identity, independent windows, peer failure/stop, late attachment, remount, retained entry identity through trimming, readonly authority and canonical entry fields. The individual readonly entry-node lifetime test adds source570's first-note/200-more SQLite scenario with positive retention and subsequent collection controls; existing backing-array lifetime tests remain.

Final full development run passed durable713.829s/tools22.450s; the preceding full run passed376.429s/15.796s. Timing variance is unexplained. The filesystem was subsequently observed at99% usage; clearing this project's30GB completed Go build cache restored35GB free. Installed toolchains, modules, pinned source assets and user data were preserved. No timing causality or profiling result is inferred.

Repository-wide vet/Staticcheck, logging, formatting and diff checks passed. The pinned oracle runner passes95 unchanged upstream tests and5 source probes. Acceptance profiling and release gates are outstanding.

## Synchronous conversation observation

`Harness.ObserveConversation` now uses the same readonly mounts. It delivers synchronously before commit outcomes return, retains its initial `Value`, and treats its context as acquisition-only. Detach suppresses future advance/end calls. Guard failures and end faults are contained and reported; normal Session close ends a synchronous observer `failed` with `session closed`, as `closeEndOf` requires. Watch endings retain their separate contract.

Mount and observer order are explicit: first registration sets mount order, the last owner removes it, and removing its first observer does not reorder a surviving mount. A new membership test first failed with `[peer,first]` from map iteration; registration/mount ordering fixes it. The literal source525 synchronous-observer/window scenario passes. Stored settlement of a memory-only document change does not deliver a duplicate revision.

Final combined verification after the maintenance fix below: durable384.947s/tools18.434s; repository-wide vet/Staticcheck and focused observer/maintenance suites pass. Synchronous observer functions pass ten shuffled repetitions (seed42).

## Deferred maintenance regression

A full run exposed stale prepared storage baselines during withdrawal/recovery. Source `#storeHolds` drains the writer before invoking its selector. Native maintenance selected and wrote holds before earlier receipts were published. It now drains first, then selects all current holds or filters a specific selection against current membership. The deterministic retained/released-hold cases fail the historical code overlay with the exact stale-baseline error and pass the correction. The original withdrawal/recovery test passes30 shuffled repetitions. This closes that reproduced race; full maintenance writer-path acceptance is separate.

## Remaining work

1. Complete wrapper fallback and repeated attach/detach lifetime acceptance, queued cancellation/close races, and every pinned view assertion.
2. Integrate readonly values into document/stored observation and finish host dispatch/waiter ordering (D16/D17).
3. Continue precise private scheduler dependencies, continuation and full durable assertion-crosswalk work. Acceptance profiling and release gates are outstanding.

## Document-watch completion ordering

A later full regression found DocumentWatch.Closed observable while the watch still occupied its Session registration. Termination now acquires observerMu before watch.mu, removes the registration and then closes the completion channel. This matches documentPinned lock order and makes End/Stop/Closed observe completed removal. Existing cancelled task-owner cleanup assertions pass100 shuffled repetitions alongside the source gate/truncation controls; the combined full suite passes849.377s/tools26.940s. This fixes the demonstrated cleanup race, not the remaining D14/D15 lifetime/identity corpus.
