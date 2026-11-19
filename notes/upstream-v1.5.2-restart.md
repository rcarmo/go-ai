# Upstream v1.5.2 restart

The working target for resumed parity work is [pi v1.5.2](https://github.com/earendil-works/pi/releases/tag/v1.5.2), commit `b4afdddf44e059c67ccf5097e5317d913521b470`. The tag resolves to that commit, GitHub lists the release as stable, and npm publishes pi-ai, pi-durable and pi-codemode at 1.5.2. Use that exact release, not moving `main`. The v1.5.0 register and baseline 39 remain historical evidence; this does not establish a v1.5.2 baseline or change the acceptance scripts' pin.

## What changes the plan

The release notes mostly cover CLI session-directory behavior, outside this port, but [the tag diff](https://github.com/earendil-works/pi/compare/v1.5.0...v1.5.2) is larger than the notes imply. Direct tree comparison found 121 changed Durable path entries, 84 AI, 7 Codemode, 19 Schema and 1,037 Chord. These are file counts, not counts of behavior changes or port obligations. Chord's relevant subpaths are in scope; its unrelated apps and CLI are not. Pay particular attention to:

1. **Durable persistence and recovery.** Port the SQLite grouped-commit tests first: exact receipts and refusal prefixes, address replacement, and reopening. Then measure native statement growth against the source contract. Work on the public Storage bootstrap and database compatibility together so storage-layer changes do not discard history. Carry forward Generation2 and Tool3 safe/unsafe restart assertions on real SQLite fixtures; classify older native tests against the new source rather than assuming they are obsolete.
2. **Public runtime changes.** Keep the Storage admission and cold-read work already landed. In addition to `Tx.submission`, v1.5.2 adds conversation-level compaction settings and changes thinking budgets and WebSocket connection options. Reconcile those at their actual public boundaries; do not combine them with a package-wide rename pass.
3. **Dependencies and acceptance.** Inventory changed tests and relevant Chord imports before switching the repo-wide release gate. Pin exact published model assets and test dependencies; pay particular attention to provider request shapes, schema equality and Codemode. Continue focused tests only; defer a full run to a designated sweep. Keep original runs, native ports and fault controls separate.

## First resumed slice: committed submission lookup

The unchanged new case in [`harness-submit-in-commit.test.ts`](https://github.com/earendil-works/pi/blob/b4afdddf44e059c67ccf5097e5317d913521b470/packages/durable/test/harness-submit-in-commit.test.ts) submits an input, waits for settlement, and reads the submission and its transcript entry inside a later commit. It also checks that an unknown ID is absent. The v1.5.2 public interface now includes `Tx.submission`; the existing implementation had been private in Go.

[`TestSourceV152TxSubmissionReadsPlacedInput`](../durable/transaction_submission_v152_test.go) failed before the runtime change (0.408s). The port exports `Tx.Submission` and routes the existing abort consumer through it; the table-write fence and lookup behavior are unchanged. The test checks exact `done`, conversation ID, `pi.user`, and missing-ID results through `Harness.Commit`. Four focused Go groups, shuffled twice, passed in 2.513s. One compiled control detected dropping the entry link. Vet, Staticcheck, logging and diff checks passed.

Unchanged v1.5.2 originals passed: `harness-submit-in-commit` (24), `sqlite-commit` (12), `harness-tasks-recovery` (25) and `harness-tools-recovery` (10), no skips. The Harness tests ran with the pinned source and the published 1.5.2 pi-ai provider JSON (44 files from the integrity-checked npm tarball), not the live-generated catalogue. The SQLite source result is a reference run, not a native grouped-commit acceptance result. No full Go suite ran.

## Grouped SQLite transport and refusal prefix

Pinned `sqlite-commit.test.ts` exercises incarnation replacement in either write order and bounds statement growth for groups of one and 150. The native port was red in 4.215s: it returned the wrong refusal reason and issued 152 SQL statements for 150 small batches (5 for 3).

The public commit now preserves the accepted replacement, refuses the third incarnation with the source reason, and omits all later batches. The Go tests check the receipt, current address and absent IDs both live and after reopen. Native frame storage writes compact groups in one `json_each` insert with a 512 KiB argument cap. Large frames bind exact blobs directly in reusable power-of-two statement shapes below 100 parameters. Tested native groups of 1, 3 and 150 each issue 3 SQL statements including transaction boundaries; 3 and 150 execute identical SQL. Tested oversized compact groups split by payload size, and `EXPLAIN QUERY PLAN` finds no stored-table scan in the exercised compact inserts. This is not the upstream eight-statement layout or upstream SQLite file compatibility: those require the canonical schema and its indexed checks. The six generated upstream random cases and full schema query-plan corpus still need native mapping.

Focused Go group, rollback, uncertain acknowledgement and crash/reopen tests passed in 21.822s (two shuffles). Three compiled fault controls detected and were restored: per-frame SQL, stale refusal text and a doubled array cap. All 12 unchanged v1.5.2 SQLite source tests passed again; vet, Staticcheck, logging and diff checks passed. No full suite.

## Read-only SQLite connection

A `SQLiteOptions.ReadOnly` connection now opens an existing native file in SQLite `mode=ro`. It does not acquire or release the writer's ownership lock or issue schema or configuration writes. It rejects allocation, commit and reclaim. When SQLite reports an external change, it reloads the checkpoint and frame tail from one SQL snapshot. The focused viewer follows a writer across a commit, a reclaim and another commit; the writer remains exclusive after the viewer closes. An absent path is not created.

Red before implementation: 0.252s (the viewer tried to acquire the writer lock and created a missing database). Focused two-shuffle: 6.320s. A stale-prefix fault control was detected and restored. Vet, Staticcheck, logging and diff checks passed. All 5 source `harness-read-only` tests passed; that source result is not native Harness acceptance. The native Harness still needs read-only admission, scheduling and cache policy. Original SQLite schema compatibility remains open.

## Read-only Harness

`Options.ReadOnly` now separates inspection from startup recovery and scheduler activation. The Harness reads the current SQLite image, opens an existing root without changing it, and returns `ReadOnlySession` for the exercised writes and scheduling operations. A commit with no writes remains valid. Closing a viewer does not requeue the driver's tasks or publish wake notifications.

The running-driver test uses a real blocked model call, checks the transcript, `LiveDoc`, the running task, paused inspection and inactive wake, then sees a later writer commit from the same viewer. An empty database stays empty. The first test was red in 0.341s because Open attempted recovery. Focused two-shuffle: 5.609s; two compiled controls detected and restored (recovering on viewer open and serving a stale image). All 5 unchanged v1.5.2 source tests ran again, including the original JSONL-only case (not a native backend). Vet, Staticcheck, logging, the retained v1.5.0 register check and diff checks passed. The upstream schema and the native proof of writable recovery after a viewer close remain open.

## Inspecting an upstream-produced SQLite file

A read-only open can now read selected canonical records from the original SQLite schema, including exact stored entry sequences, running tasks, request-indexed submissions and historical document deltas. The native viewer reads one SQLite transaction and does not apply migrations. A writable open of an upstream file refuses before creating native tables, rather than silently overlaying two formats.

The [upstream producer](../durable/testdata/pi-durable-v152/generate-sqlite-viewer.mjs) creates the fixture using public v1.5.2 `Storage.commit`. The resulting schema-v2 database regenerated byte-for-byte (`c64fb90105d18dfc0b79f6224beb87a1e70a8219a2759383dc28cd246d39ce8f`). The unmodified Go backend rejected the file (0.018s); the focused viewer/codec/reopen regressions passed twice in 2.237s. Compiled fault controls detected and restored dropping stored deltas and permitting writable admission. All 83 unchanged upstream SQLite storage tests passed. Static checks passed. Native writable migration, durable SQL parity and v1-produced fixture acceptance remain open; the read-only projection is not a waiver for them.

## AI request options and tool-only assistant messages

The v1.5.2 output clamp reserves the larger of 4,096 tokens and 15% of estimated input, then applies the capped fallback when the proportional reserve leaves too little output. The seven source cases are ported through the public Go clamp; the fixed-only implementation was red for the proportional and near-full cases (0.021s).

OpenAI completions now sends explicit `null` for tool-only assistant messages by default. A model can require `""` without inserting an extra turn; the separate after-tool-result switch still inserts the extra turn. The three Go cases inspect the real HTTP request, including the presence of `content`. The default was red (0.034s). The model generator now carries the optional field, but the pinned model schema and catalogue have not been regenerated for 1.5.2.

Four packages compiled; the selected Go tests passed twice in `tests`, OpenAI completions and Responses. The two unchanged upstream files passed all 10 cases. Fault controls detected and restored a missing proportional reserve and an ignored non-null switch. Vet, Staticcheck, logging and diff checks passed. No full suite.

## Codemode reference and guest output changes

`ResolveCodemodeSchemaRef` now URI-decodes local pointer segments before JSON Pointer unescaping, preserves the returned object identity and rejects malformed escapes without fetching anything. Declaration expansion uses the same resolver. The unchanged v1.5.2 prelude extracted from the pinned source prints `NaN`, `Infinity` and `-Infinity` in console output and throws `NaN` with the source error message. Its embedded asset hash and SBOM provenance now reflect 1.5.2; the WASM asset is unchanged.

The focused resolver and production QuickJS output tests were red on the escaped-space lookup and `null` output, then passed twice (Durable 2.038s, QuickJS 0.753s). Both deliberate faults were detected and restored. The unchanged upstream declarations and sandbox files passed 175 cases, zero skipped. Vet, Staticcheck, logging, guest SBOM self-tests and diff checks passed. Broader Codemode lifecycle and replay parity remain open.

## Schema equality

The public `SchemaEqual` now shares the existing structural comparator used by `const`/`enum` validation and coercion. It distinguishes null, absent keys and the in-memory `SchemaUndefined` value. Undefined does not enter the JSON wire. Every source equality case and an undefined-identity control passed in the focused Go run. The fault control detected dropping undefined equality. The unchanged source equality and coercion files passed 50 total cases; this is not native acceptance for the new bigint and undefined coercions. Vet, Staticcheck, logging and diff checks passed.

## Writing and migrating existing upstream SQLite files

Writable opens now preserve the upstream schema instead of refusing it. The accepted prefix writes only original tables and metadata in one SQLite transaction, and emits receipts only after commit. A failure in a later SQLite insert can roll back the entire otherwise valid group without receipts. For version 1, the writable open runs the task table rebuild that admits `sleeping`; a read-only open leaves the schema at version 1. The version-one database uses the original first migration and a seeded canonical running task. Its bytes reproduced (`decc4c4dc839f446446b8a9556488af17e48eb461f518c9b15f5473a3254bf3c`).

The existing-schema writer was red at open (0.034s); the version-one test was red at the missing migration (0.041s). Focused writer, rollback, viewer and migration tests passed twice in 4.081s. An independent v1.5.2 Node Storage reopened a Go-written database and read the exact entry, request-indexed submission and current/historical document values. Fault controls detected and restored suppressing the migration and misrouting revision inserts. Vet, Staticcheck, logging and diff checks passed. Fresh Go files still use native frames; the upstream-schema write branch does not yet batch SQL to the upstream statement bound or port the generated six-seed corpus. Native durable reservation still differs from upstream minting before commit. These remain open, not accepted deviations.

The original-table writer now also batches compact row inserts through one bounded `json_each` argument per table and binds large rows in power-of-two groups. On the original schema, 1, 3 and 150 compact document-delta/entry parts use the same seven connection calls, including transaction boundaries. The exercised query plans contain no stored-table scan. This Go path validates in memory and therefore is not the reference's exact eight SQL statements and indexed check reads. Early testing also found a missing physical base for newly created current-only documents; the patch writes that base and prunes pending/stored earlier revisions on replacement. The focused grouped, migration, refusal and rollback batch passed twice in 4.354s. The prune fault was detected and restored. Full random/statement oracle coverage remains open.

## Conversation compaction policy

Per-conversation policy is now persisted on the agent, replaced or cleared without discarding the model, and validated before any stored change. Manual, threshold and overflow compaction consume the same field-wise policy: conversation model override, Harness model override, conversation general, Harness general. A conversation-level `enabled: false` takes precedence over the Harness model's enable flag unless the conversation's own model override supplies `enabled`.

The new tests assert the four exact summary request caps (400, 200, 100, 400) at the model stream boundary, threshold enable precedence, no summary on disabled overflow, and durable clear/invalid-value behavior. The startup red (0.823s) lost the model and policy. Focused Go tests passed twice in 21.294s; omission and enable-precedence controls were detected and restored. All 114 unchanged upstream compaction tests passed. The older extension-section-order test also failed on the unmodified parent; it is not a regression or accepted result for this slice. Broad agent patch/child inheritance coverage is still open.

## System section order through entry persistence

The `Entry.model` decoder now retains key order from source `sections` objects without emitting a native `sectionOrder` field. Durable prompt replay and provider transcript rendering share JavaScript's array-index-first enumeration and preserve insertion order for other keys. This fixes the older provider request that reordered an explicitly selected pair of extensions and makes the existing patch, historical fork and reopen group pass again.

The new public Entry roundtrip and the old provider case were red together (0.627s). Focused root/Durable regressions passed twice (0.032s/2.492s). Two controls detected dropping order when decoding and dropping index-first ordering. Unchanged pinned prompt/registry tests passed 19; vet, Staticcheck, logging and diff checks passed. No newer full baseline was inferred.

## Storage without native dump methods

`Storage` no longer requires `Snapshot` or `Documents`. A decorator implementing only the source-facing methods can open a populated Session: its scans supply the current validation image, and exact entry reads supply the stored entry sequences. The greatest visible sequence is only a lower bound until Storage supplies a new receipt. A current scan is not revision history; delegated historical definition reads resolve the visible entry, then the exact address and incarnation at its commit sequence.

The interface and scan test was red in 0.180s, and the historical consumer was red in 0.112s. Focused native regressions passed twice in 9.687s. The fault control detected using the scanned image as history. The unchanged v1.5.2 tables, documents and forks files passed 53 tests; vet, Staticcheck, logging and diff checks passed. Native snapshot fixtures opt into the optional native projection explicitly. The current-image scan bridge is still eager; source lazy loading, complete Tx acquisition and historical fork-copy semantics remain open.

Delegated fork selection now scans the historical entry owner's document scope at the entry's exact commit sequence and the immediate parent's current scope. The public batch still contains `document.copy`, with its exact source ID and point, not a rewritten base command. A private, detached historical value supports Tx staging and receipt replay without inventing history in the current projection. The stored child copies the old incarnation even after replacement at the same address. The new consumer was red in 0.145s; focused fork, copy, agent patch and reopened public Storage tests passed twice in 49.913s. A replay fault control detected dropping the historical read. Unchanged source forks passed 14; vet, Staticcheck, logging and diff checks passed.

On the original SQLite schema, minting now advances only the open handle's allocator. A refused group leaves on-disk `next_id` and `next_seq` unchanged; the first accepted group stores the high water including all candidates minted during that open. This matches source `Storage.mintId` and metadata settlement. Existing native frame files retain their reservation history. The original-schema probe was red at a write on mint (0.070s), focused allocator/write/rollback tests passed twice in 4.591s, and forcing the old mint write was detected and restored. Vet, Staticcheck, logging and diff checks passed.

### Original SQLite migrations

The original-table writer now applies the pinned `createSqliteMigrations` statements before loading records, inside its opening transaction. Version zero with a prefix initializes both migrations. Version 3 rejects with `SQLite schema version 3 is newer than supported version 2`. Version-1 viewers still leave the schema untouched. A supplemental late-DDL fault (index name collision after the task table replacement) proves that version 1 and its running task survive and that a writer can retry after removing the collision. It is not a port of the original custom-migration injection API.

The two version cases were compiled red in 0.120s. Focused original-schema storage and migration tests passed twice in 2.396s. Dropping migration 1 and removing the future-version check were each detected and restored. The unchanged `sqlite-migrations.test.ts` passed all 4 cases; the extracted asset reproduced byte for byte. Vet, Staticcheck, logging, inventory and diff gates passed.

Fresh public `OpenSQLite` still defaults to the native frame schema. A temporary original-table cutover passed create/reopen but exposed task and crash regressions, including loss of terminal tool usage and failure to replay some built-in final states. The default was restored; migration coverage does not imply that existing native databases can be rewritten or that an original-table database can yet persist every Harness state. Frame-specific count and injected-failure tests are not original SQL assertions.

Public `Storage.Commit` now stages document command multiplicity per part before looking up the target incarnation. Duplicate retirement or content on absent document 101 returns the corresponding source diagnostic after accepting the earlier conversation batch, whose receipt and exact row remain available. This translates the duplicate-command assertions on original-table SQLite; it does not port the StoreStorage adapter or its statement-fault matrix. Compiled red: 0.128s. Focused Go SQLite, copy/fork history and storage conformance passed twice in 22.491s; disabling the content check was detected and restored. The unchanged SQLite and StoreStorage commit files passed all 34 cases. Vet, Staticcheck, logging and diff checks passed.

The public tool argument parser now normalizes optional nulls using `prefixItems` for the tuple prefix and `items` for the tail. The supplemental pinned compiler probe and Go parser both remove the optional non-nullable keys, preserve the nullable key, and leave the input untouched. The Go test was red at a spurious `"drop":"null"` (0.025s); the focused schema and public validation regressions passed twice, and the prefix-ignored fault was detected and restored. Unchanged `coerce` and `coerce-typebox` together reported 268 passed and 2 upstream skips; neither skip is accepted as parity. Vet, Staticcheck, logging and diff checks passed.

Draft-7 `additionalItems` now constrains validation without coercing the tail of a tuple. The pinned v1.5.2 pi-ai argument parser produced the exact rejection for `[1,2]` with a string tail schema; both pinned schema compilers (v1.5.0 and v1.5.2) also rejected it at `/a/1`. Those outputs replace the incorrect v1.5.0 supplemental positive expectation; a string tail still passes. Compiled Go red 0.029s; focused root and public conformance passed twice, and restoring tail coercion was detected and reverted. Static and diff gates passed.

### OpenAI prompt-cache key policy

Completions and Responses now honor the pinned `promptCacheKeyMode` metadata independently of retention and session-affinity headers. Real local HTTP requests cover the Responses four-case matrix and the added Completions cases: stable proxy identity, automatic fallback, retention `none` precedence, disabled-key/long-retention independence, missing identity, and the 64-code-point clamp. The model generator preserves the field; this does not regenerate the published catalogue. Compiled reds: Responses 0.030s and Completions 0.050s. Focused provider and generator tests passed twice; both mode-bypass controls failed and were restored. The unchanged prompt-cache originals passed all 31 cases. Vet, Staticcheck, logging and diff checks passed.

### Published model catalogue

The catalogue generator and its regeneration gate now use the published pi-ai 1.5.2 tarball (SHA-256 `d5715efa834c23bfdf10a042b49df4ee169cc99000bf16d7998fab2d533b45f8`). The schema-v6 reader checks the released 43-provider manifest byte digest, each provider digest, and the independently rebuilt structure digest. The generated chat, image and classifier files reproduce byte for byte; the full-record comparator matched 1,643 chat, 66 image and 64 classifier records. Current catalogue totals and Azure Astra's context/tiering checks were updated from those exact records, not from a live provider catalogue. The v1.5.0 manifest remains readable by the offline generator.

The old manifest gate was compiled red (0.010s); focused catalogue and generator tests passed twice, and the unchanged runtime/strict generator originals passed 45 cases. The unchanged-shape manifest corruption and wrong provider files were rejected; the regeneration comparator's negative self-test caught metadata drift. Vet, Staticcheck, logging, source inventory and diff checks passed. This repins the model regeneration gate only, not schema-format assets or the repo-wide acceptance register.

### Reopen after document retirement

A writable Harness can now reopen original SQLite files containing retired document records with no current value or version. The validation rule admits only identical carried-forward tombstones; it does not relax fresh document validation or native frame checkpoint decoding. Historical revisions remain separate and readable. The translated current-only SQLite case verifies revision counts 11 → 1 → 2 → 0, and a supplemental writable reopen also reads the retired rewindable fixture at its exact earlier sequence.

The reopen probe was compiled red in 0.192s. Focused original-schema, history, checkpoint and storage regressions passed twice in 23.218s; both clone and validation fault controls were detected and restored. The unchanged SQLite storage file passed all 83 cases. Vet, Staticcheck, logging and diff checks passed.

The previously observed held-result and tool-usage failures also reproduce on the unchanged native frame backend. They do not establish cutover regressions. The missing current value on retired rows is independently reproducible on an original-schema database. Fresh schema default and the remaining task/crash cases still require separate evidence.

The translated task-time recovery case is green without a runtime change on the original SQLite layout. After an explicit first-apply signal, closing at 3,000 and reopening at 9,000 preserves `startedAt=2000` on the pending record, with no `endedAt`. The resumed task completes with its original input and checkpoint, `startedAt=2000` and `endedAt=9000`. Focused recovery/time tests passed twice in 2.677s; changing persisted `startedAt` was detected and restored. The unchanged task-recovery original passed all 25 cases. This adds acceptance for one translated scenario, not the crash/restart corpus.

### Default layout for fresh SQLite files

Fresh `OpenSQLite` namespaces now create the original v2 tables through the pinned migrations. Existing frame namespaces still use the frame reader and writer in place. Detection follows SQLite's ASCII-case-insensitive identifiers, so opening an existing prefix with different case cannot select an empty namespace and hide its rows. The fresh-schema probe checks unprefixed and prefixed files, exact first ID and receipt, absence of frame tables, and a read-only reopen. A separate unmodified Node Storage consumed a Go-created prefixed file and read the exact entry, sequence and next ID.

The fresh-schema and case-sensitive detection probes were compiled red (0.411s and 0.245s). The focused cutover batch passed twice in 38.888s; both old behaviours were detected and restored. Legacy frame packing, injected frame faults, old config/replay and private legacy-task fixtures still run their original assertions against explicit legacy namespaces. They do not count as tests of the new schema. Two allocator reopen assertions now use the source rule: minting without a later accepted part does not advance the persisted cursor. The unchanged SQLite migration originals passed all 4 cases; vet, Staticcheck, logging and diff checks passed. Exact upstream SQL, the six-seed native corpus, and integration-wide acceptance remain open.

### Original SQLite row packing

The default SQLite backend measures the pinned 8 Ki row threshold and 512 Ki JSON array cap in UTF-16 units, not UTF-8 bytes. It pools compact rows before binding larger rows in power-of-two groups under 100 parameters. Pending task, submission and document updates collapse to their final row per ID before packing, so size-based ordering cannot resurrect superseded values.

The public-commit translation checks the original 100 × 7,000 compact workload, both quote-heavy bind workloads, receipt sequences and stored record data. Supplemental cases catch mixed-size and Unicode sizing differences and verify final task state after reopening. Compiled red: 2.402s. Focused shuffled regressions passed twice in 6.619s; byte sizing and dropped last-write controls were detected and restored. The unchanged SQLite commit file passed all 12 cases. Vet, Staticcheck, logging and diff checks passed. This does not yet satisfy the source's total statement count or native six-seed comparison.

Original-table SQLite commits now preserve the database error itself on a confirmed rollback or failure to begin. These failures do not emit receipts, become a logical refusal, or poison a writer whose transaction never committed. Uncertain commit and unconfirmed rollback still poison. A supplemental statement-fault probe demonstrates exact Go error identity, rollback of earlier table writes and successful retry on the same writer. The real trigger probe checks the driver error and reopen. Compiled red: 0.116s; focused shuffled regressions passed twice in 11.329s. The discarded-identity control and static gates passed. The pinned indexed check-read fault matrix still needs a direct native translation.

### Indexed SQLite commit checks

An accepted group now checks its touched ID types, document metadata, latest delta versions and current addresses against SQLite in the same transaction before writing. These indexed queries reject a stale in-memory projection instead of appending a delta to a changed base version. An injected ID-check read failure rejects with its original identity, rolls back without receipts and permits a same-writer retry. These checks currently cover staged accepted parts; the original also reads keys of refused and unvisited parts before staging. That broader validation ordering and copy-source SQL are still open.

The default-backend translation measures eight transaction statements for 1, 3 and 150 parts, excluding BEGIN/COMMIT as the source recording does, and compares the SQL texts across sizes. The 100-entry compact workload measures nine; the address-replacement workload exercises all four check reads, upsert and revision drop, with no stored-table scans in any batched plan. Compiled red: 0.733s. Focused shuffled regressions passed twice in 18.863s and the final SQLite-only batch twice in 10.213s. Removing the check reads was detected and restored. Vet, Staticcheck, logging and diff checks passed. Native random corpus and full integration acceptance are not established.

### First native random-corpus divergence

The reproducible seed-6 prefix keeps three exact source input/result pairs. Raw Storage no longer treats entry conversation and head IDs as Session-owned foreign keys; Tx still checks links and visibility for its own entries. The accepted third round returns its exact receipt, and all three entries survive an original-layout viewer reopen unchanged. Compiled red: 0.118s. Focused Tx, Storage and SQLite checks passed twice in 12.729s; restoring raw referential checks was detected. The fixture regenerated byte for byte from an instrumented copy of the pinned test, without modifying the original.

The diagnostic recording reached all 720 source rounds. Native replay still diverges: four seeds reach the source test's opaque terminal outcome (`{"kind":"returned","value":1}`) that Go's typed task codec does not decode. Only this literal seed-6 prefix is accepted; native six-seed table and snapshot parity are still open.

Public Storage now preserves the pinned record-claim diagnostics, including an ID already owned by a stored record and an immutable ID repeated in the same batch. The translated logical-refusal case checks the accepted prefix, exact receipt and stored entry, absent suffix, and duplicate-ID diagnostic. Compiled red: 0.123s. Focused shuffled regressions passed twice in 14.390s; removing the source diagnostic check was detected and restored. Static gates passed.

Default SQLite public Commit now opens its transaction and checks all requested keys before staging any part. The same SQL transaction writes the surviving prefix or exits unchanged; no second preflight runs after staging. A query fault on a would-be refused group rejects before any logical refusal, and a successful prefix still preflights the unvisited suffix without storing it. Compiled red: 0.114s; focused shuffled regressions passed twice in 28.524s. Omitting the unvisited suffix was detected and restored. Static gates passed. The internal legacy frame path still has its separate commit behaviour; source SQL copy resolution remains open.

### SQLite copy-source reads

Accepted copy parts now read their source document and revision range from SQLite in the committing transaction. Earlier accepted parts flush before that lookup. The query and staged view must agree before the writer publishes either receipts or data. The source-read fault probe witnesses the flushed entry inside the transaction, then verifies the original error, no receipts and no durable prefix. A successful group reads a preceding delta at the current point and the original base at the earlier point; both targets survive reopening.

Compiled red: 0.111s. Focused shuffled regressions passed twice in 16.395s, and removing the SQL source lookup was detected. The unchanged SQLite storage suite passed all 83 tests. Vet, Staticcheck, logging and diff checks passed. This still does not establish source-read ordering for a copy already rejected by staging, or replace the in-memory validation projection.

### Opaque terminal task records

Raw Storage now preserves a terminal outcome object without a typed status discriminator. A record accepted by the upstream random corpus round-trips through public Commit, SQLite and a reopened reader with unchanged JSON, without fabricating a typed task result. Standalone task-state decoding and runtime transitions still reject opaque outcomes.

Compiled red: 0.014s. The focused shuffled SQLite and task-recovery batch passed twice in 22.340s. Removing opaque storage decoding was detected; vet, Staticcheck, logging and diff checks passed. Diagnostic replay of the 720 source rounds now decodes these outcomes and reaches the next divergence: the source accepts raw task and document owner IDs that may refer to records not present in the store. That discrepancy is not resolved by this checkpoint.

### Six-seed SQLite receipts and tables

The default backend now replays all 720 rounds of the original random input stream. Tests compare every count, refusal presence and receipt with the recorded source results. After every round they compare all eight SQLite tables between grouped and separate commits and require no stale current-only revisions. The pinned recording reproduces byte for byte (gzip SHA-256 `d280c7d6fcbe1b152e2dfc1550ff0fb2dd00e62807b2db7c9b97e2df8ff8769b`). The instrumented copy is a fixture generator, not a substitute for running the unchanged upstream test.

The mismatches were unresolved raw owner links, creation and retirement in the same part, and map iteration changing the stored task JSON. Raw Storage now preserves these links without requiring the linked rows to exist; Session still owns its referential checks. An empty lifetime works in either command order and does not reserve its address. SQLite encodes task rows deterministically. The previous native negative fixture now tests a genuinely malformed task (missing input); the corpus and focused reopen test cover the source's acceptance of unresolved links.

Compiled reds: raw owner links 0.092s/0.112s, empty lifetime 0.244s, table equality 1.866s. Focused shuffled regressions passed twice in 64.072s. Reintroducing random task-row encoding and rejecting the empty lifetime were each detected. Vet, Staticcheck, logging and diff checks passed. This packet does not compare the native public read snapshots against the upstream snapshots, nor prove the repo-wide acceptance gate.

### Six-seed record reads

All 720 seeded rounds now compare the public conversation, task and submission scans and exact entry lookups with snapshots captured from the pinned memory oracle. The selected snapshot fields use exact decoded JSON equality, including absent lookups, record fields, and commit sequences. The fixture reproduces byte for byte (gzip SHA-256 `5f526e5925f78fe6b2b632f81b001aa74c9b70468d18acffc8753fd304b4ce72`). The interrupted entry-sequence fault was reproduced after workspace restart and removed. Focused shuffled Go checks passed in 25.362s, and vet, Staticcheck, logging and diff checks passed. Transcripts, submission request lookups and document snapshots are not covered by this packet.

The six-seed native replay also compares all ascending conversation transcripts and submission request lookups with the original snapshot results, including the exact request reason for an unknown conversation and absent request keys. The source fixture regenerated byte for byte (gzip SHA-256 `7218fe06147c69e35c1dd50ce7879060cf5ca47aa8b8ae6fd1abbcc585570fa1`). The existing implementation passed the new translation, and replacing the request key was detected. Focused Go checks passed in 31.747s; vet, Staticcheck, logging and diff checks passed. Document snapshot fields remain separate.

### Six-seed document reads

The native six-seed replay now checks every public snapshot field in the pinned random corpus: current and historical document content, scope pages and address results, in addition to records, transcripts and request lookups. The six final snapshots also check document reads at every accepted sequence. The exact document fixture regenerated byte for byte (gzip SHA-256 `7621d0b9046ce3e4a9bdf47c21ccf44771d584b17ebb97504bf57ce0eff76adb`).

Current-only history requests now report the incarnation ID in the same typed request reason as the source. The translated test failed on that reason before the change; restoring the old reason was detected. The focused shuffled Go batch passed in 45.822s. Vet, Staticcheck, logging and diff checks passed. This completes the mapped six-seed SQLite corpus, not repo-wide parity; copy-refusal ordering and broader viewer/recovery remain open.
