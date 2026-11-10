# Shared readonly conversation views

Public view watches now return shared readonly values. D14–D17 remain open because synchronous conversation observation, other observer surfaces and the complete assertion crosswalk still need work.

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

## Remaining work

1. Implement public synchronous `observeConversation` with source attachment, guard, membership and error/close semantics using the same readonly values. An asynchronous watch is not that interface.
2. Complete wrapper fallback and repeated attach/detach lifetime acceptance. Audit every pinned view assertion; keep source-runtime GC timing differences explicit.
3. Integrate readonly values into document/stored observation and finish host dispatch/waiter ordering (D16/D17).
4. Continue private scheduler dependency, continuation and full durable assertion-crosswalk work. Green view slices do not establish complete upstream alignment.
