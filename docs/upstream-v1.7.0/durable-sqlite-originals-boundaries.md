# Durable SQLite originals and storage boundaries

Scope: the unchanged pi v1.7.0 Durable originals `sqlite-commit.test.ts`, `sqlite-facade.test.ts`, `sqlite-migrations.test.ts`, `sqlite-storage.test.ts`, and the SQLite-representable arm of `storage-runtime-boundary.test.ts`, at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. IndexedDB, JSONL, MemoryStorage, and Chord are excluded.

## Byte verification and original run

The exact source checkout at `/work/port-lanes/.schema-equal-v170-source` remained clean. Blob and byte identities:

| Original | Git blob | SHA-256 |
| --- | --- | --- |
| `sqlite-commit.test.ts` | `cf6bb0696d2b285a15d1d85a8b7721034d85eb19` | `f25013c7fc1d70588c8238569b609ec34cc18f9c70a670454c992a531ad846b0` |
| `sqlite-facade.test.ts` | `d6c6995c0666570cc93638bd61fccb1cb798282b` | `e4f4d1180663100314b89e7ca77c6ca543cb1fa082093886e9269aecddc42a70` |
| `sqlite-migrations.test.ts` | `64224808dbe9215de3e46ea4cd9caf6460b6b8a0` | `a1a654e68924bf36e436b25cf02f3c3206b1c9a4d0c76449f5eea3cd6223c8fa` |
| `sqlite-storage.test.ts` | `c9c4e6c5baf9c91f758c71adce9552d6ab17e833` | `0f214d915a1ec79e193f3feb49cb68eb0dc6dae51d1b3879e82d671111300053` |
| `storage-runtime-boundary.test.ts` | `84ef2d51252f0f9d23b9dc788af58c4bcec6e81c` | `11bce7410b047902ea3362487954c1f9ac59ddeca6911a0319ae0542e1747f1b` |

With Node 26.13.0, Vitest 4.1.12, one worker, the unchanged five-file command passed **111/111** in 39.14 seconds. The original run used the existing lock-verified dependency checkout; `GOMEMLIMIT=650MiB` and the shared lock `/tmp/go-ai/v170-focused-verify.lock` bounded the run.

## Neutral native comparison

`storage_sqlite_originals_v170_test.go` translates only public and SQLite-observable contracts. Existing `V152` fixtures are retained where the underlying unchanged originals have identical pinned v1.7.0 bytes and existing Go locators already exercise the contract.

- `TestV170SQLiteFacadePublicSettlementAndAllocation`: ordered receipts, durable prefix, refusal/allocator fence, repeated close, closed reads, file reopen and read-only data.
- `TestV170SQLiteMigrationsPreserveDataAndRollback`: v1 data retention, failed v1→v2 rollback, successful retry, sleeping-row schema acceptance and all five task indexes.
- `TestV170SQLiteStorageReopenAndCorruptionFence`: missing required revision data is rejected on the real reopen path.
- `TestV170SQLiteRejectsNewerSchemaBeforeMutation`: newer schema refusal through the public opener.
- Existing source-derived random corpus `TestSourceV152SQLiteSixSeedReceiptsAndTables`: six 120-round grouped-vs-single comparisons, refusal prefixes, receipts, all source tables and public read snapshots.
- `TestSourceV152SQLiteMigrationFailureCanRetry` and `TestSourceV152SQLiteMigratesVersionZeroWithPrefix`: late DDL rollback and idempotent initialization.
- Existing `TestSourceV152SQLite*`, `TestLegacySQLite*`, ownership tests: indexed writes, batch transport, reopen, prefix ownership and cross-process fencing.

The Go public API does not expose the JavaScript driver wrapper identity, connection prepare hook, async `SqliteDatabase.transaction` return object, Node file creation ordering, physical WAL control in identical terms, or source-graph `node:` imports. Those observations are recorded as partial/nonportable, not as passing through a substitute helper. The runtime-boundary package graph is represented by the production dependency graph only for the SQLite path; JSONL/indexeddb/memory and Chord boundaries remain excluded by scope.

## Unchanged-native baseline

Before production edits, the scoped selector passed. No production gap was demonstrated, so no runtime file changed. The new tests initially had two harness expectation errors (allocator is monotonic by reservation; the public Go task codec rejects an absent task input), which were corrected without weakening original assertions. The migrated sleeping-table check uses SQL as the source migration suite does; it does not route sleeping through an unrelated native Harness codec.

## Focused verification

Base/main: `f6f191ec74dc7620ee274b890d9371b406400758` (exact repository main, which is at the v1.7.0 integration checkpoint).

Environment for Go: `CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3` and the shared lock.

```sh
go test ./durable -run '^(TestV170SQLite.*|TestSourceV152(Upstream)?SQLite.*|TestLegacySQLite.*|TestSQLite130InvalidPrefixBeforeFilesystemEffects|TestMainSQLitePureGoContentionAdmittedCancellationAndCloseOverlap|TestSQLiteCrossProcessOwnership|TestSQLiteDirectoryAliasAndFileSymlink)$' -count=1 -shuffle=42
go vet ./durable
go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./durable
```

The pre-receipt selector passed twice (50.968 s and 50.980 s); after the final corruption-fence addition, the selector passed twice again (55.356 s and 50.880 s). `go vet` and Staticcheck v0.8.2 passed. This is scoped convergence evidence, not full acceptance, profiling, publishing, hosted CI, or a declaration of package-wide parity.

## Limits

- No indexeddb, JSONL, MemoryStorage, Chord, Cloudflare, or JavaScript source-import assertions were ported.
- No test was relabelled as complete where only its public SQLite semantics are representable in Go.
- Full package acceptance and acceptance profiling are explicitly outside this task.
