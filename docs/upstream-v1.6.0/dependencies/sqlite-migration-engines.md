# SQLite engines during a Durable migration

The unchanged pi v1.6.0 sleeping-task migration case passed using SQLite **3.54.0** in Node **v26.13.0**. Its native translation passed using SQLite **3.54.1** from `modernc.org/sqlite v1.61.1` and `modernc.org/libc v1.79.0` in a CGO-free Go **1.27.3** executable. The [receipt](sqlite-migration-engines.json) records the full `sqlite_source_id()` for each engine, the Node executable digest and the Go executable's module build information. Both runs were on Linux arm64.

The [probe](probe-sqlite-migration-engines.sh) runs the original `packages/durable/test/sqlite-migrations.test.ts:48` case from pinned Git `faac585884ae2c013a6b1400f3e9e77f10b34bac`, with the existing Vitest `4.1.12` closure. The [Node observer](sqlite-migration-engine-witness.mjs) forwards the real `DatabaseSync.exec` call and reads the engine identity on the same connection after it creates `tasks_v2`. That connection still reports schema version 1 inside the migration transaction; the original test requires version 2 after it closes and reopens the database.

The [Go translation](sqlite-migration-engine-test.go.txt) reads the engine identity and raw driver from the connection that `OpenSQLite` opened and migrated. It checks that v1 rejects a sleeping row, v2 accepts it, the waiting row's JSON stays byte-identical, all five task indexes appear in exact order, and no `tasks_v2` table remains. The native fixture preserves the original waiting row and adds its owning conversation and allocation records because `OpenSQLite` also adopts the task projection. The original inserts that row directly without adjusting the v1 allocation high-water mark. Passing the native translation does not establish that the latter raw fixture can be adopted by Go.

Both behaviour tests pass. Swapping the runtime identities in a scratch receipt fails the same exact comparison used for the observed values. The probe writes only disposable fixtures, runner files and receipts. The native evidence base was `d0f5e86bf5392e4a8027904f888c6e244c2e0284`.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/sqlite-migration-engines.XXXXXX")
bash "$ledger/probe-sqlite-migration-engines.sh" "$pinned_git_mirror" "$oracle" > "$actual"
cmp "$ledger/sqlite-migration-engines.json" "$actual"
rm -- "$actual"
```

The probe needs the pinned Git objects, the prepared Oracle's local npm files, and the existing Go toolchain and module cache. It serialises verification with the shared lock and grants Node no network access. The receipt reproduced byte for byte on two runs. Three focused existing migration tests passed twice with shuffle seed 42; vet and Staticcheck `v0.8.2` passed for `./durable`.

This slice records two effective runtime engines and one migration case. Other migration cases, database adoption with inconsistent allocation metadata, other platforms, and transitive archive and module loads require separate evidence.
