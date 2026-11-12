# Primary conversation and entry reads

The primary Storage/Session/Tx APIs now implement the pinned conversation lookup and global/visible entry overloads. This development slice follows `18dfc16`; full upstream and release acceptance remain open.

Target: pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Source contract and API replacements

| Source | Go primary API | Contract |
|---|---|---|
| `Storage.conversation(id, context)` | `Conversation(ctx, id)` | Record or `found=false` |
| `Storage.entry(id, context)` | `Entry(ctx, id)` | Global `{entry, commitSeq}` lookup |
| `Storage.entry(conversationId, id, context)` | `Entry(ctx, conversationID, id)` | Immutable ancestry caps; original owner and commit sequence |
| `Transaction.conversation/entry` | `Tx.Conversation(id)`, `Tx.Entry(id)` | Global committed reads; `ReadAfterWrite` after the first table write |
| `Transaction.entry(token, id)` | `Tx.TypedEntry(token, id)` | Kind-only global narrowing |
| `Harness.entry` | `Harness.Entry/TypedEntry` | Global committed entry, without storage sequence wrapper |
| `Conversation.entries` | `ConversationHandle.Entries(ctx, query, limit, cursor)` | Bound conversation; default descending; ID cursor |

The obsolete `Conversations(Query)`, commit/position `Entries(EntryCursor)`, `VisibleEntry`, Session typed adapter and bound `ScanEntries` alternatives are removed. Callers explicitly request ascending order where their scenario needs it. Public stored-entry lookup returns the original `commitSeq` separately and clears private entry placement fields.

Session reads touch exactly the source keys: conversation; entry; or conversation then entry for visible lookup. Native reads use the published image rather than ahead-of-publication SQLite state. Dynamically installed wrappers remain authoritative, with request/error identity, cancellation and first-failure ordering retained. This does not repair the separate private-core requirement at OpenSession or wrapper Commit bypass.

Entry tokens now accept every tested non-empty string, including mixed case, spaces, punctuation, non-ASCII and names exceeding the former 128-byte regex limit. Storage retains those kinds. Resource budgets still apply. Runtime typed reads return an absent record for a different kind; they previously returned a populated record with `found=false`. Raw history tests now compare stored kinds literally; the old role-to-kind normaliser is removed.

## Test-first failures and assertion coverage

- Initial direct API packets failed on missing primary Conversation/Entry methods and missing global Tx typed lookup. The production APIs replace the conflicting native surfaces.
- The entry-kind control failed on `Mixed Case: note/v1`; the invocation wrong-kind control exposed the populated `pi.user` record. Both now pass. The constructor/storage-kind probe also passes original MemoryStorage and SQLite.
- Switching bound Entries to the public scan exposed a real `limit=0` panic at `history.go`'s negative page index. Shared scan functions now validate page limits, including Session-image paths.
- The literal deep-fork packet retains all ancestor caps, both orders, cursor continuation/order errors, inclusive bounds, current/historical markers, exact global/visible records, original commit sequences and missing/error assertions.
- The literal conversation packet retains ascending opaque pagination, JSON cursor roundtrip and final continuation absence.
- The complete named Session table-read packet now checks exact pre-write records/pages, typed `ReadAfterWrite` failures and the post-write document snapshot. The extended task/scan/head controls remain.
- Ownership controls mutate the committed input and returned nested payload independently. Fresh reads retain the original value. Existing reopen/reclaim history controls now use the primary lookup and its original sequence.

[Assertion crosswalk](../docs/upstream-v1.5.0/task-table-assertions.tsv) distinguishes original cases from source-derived controls. Go's `found` result represents source `undefined`; variadic IDs spell the two source Storage overloads. No native-envelope normalisation establishes these assertions.

## Development verification

- Full durable **414.537s**, tools **18.655s**; repository-wide vet, Staticcheck v0.8.2 and logging pass.
- Ten focused shuffled repetitions, seed42: **11.778s**.
- Seven independent compiler-overlay faults fail semantic assertions: current rather than original sequence; ignored ancestry caps; raw rather than published Session image; conversation-restricted Tx typed reads; populated wrong-kind runtime result; restored native kind regex; omitted entry dependency key. Disposable overlays/scripts are deleted.
- Reference: **152 unchanged source cases +39 independent probes**. This is147 tests from seven original files, five unchanged Storage conformance cases,35 admission probes and four Storage/transaction/kind probes. The retained snapshot has no `.git`; hashes and the complete test/support tree are checked, but commit identity is not independently reverified.

The first full run failed `TestTaskRecoveryCloseSealRejectsFreshMemoAndToolPrefix/tool-output`: queued progress returned `context.Canceled` where the native assertion requires closed/sealed. Twenty focused repetitions and the subsequent full run passed without changing that assertion. This queued progress/close error-order caveat remains open under D20; it has not been repaired or dismissed as source parity.

These are development gates. There is no race coverage or pre-release profiling receipt for this slice. Canonical builtin execution/migrations, submission/document APIs, original database interoperability, D04 caller continuations, complete lifecycle/assertion acceptance and release CPU/alloc_space/alloc_objects analysis remain open.
