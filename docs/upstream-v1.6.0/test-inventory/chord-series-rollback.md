# Chord commit-series rollback on Node SQLite

All **12 unchanged cases** in the `a commit series` group of `packages/chord/test/store/store.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`. The other 44 file cases were unselected. The [machine receipt](chord-series-rollback.json) lists the exact passing names and the file digest from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-series-rollback-witness.mjs) records the successful evaluation of Chord's `NodeSqliteDatabase` and `ChordStore`. In a separate run of the failed-write original, it forwards the real SQLite calls. The connection starts with `BEGIN IMMEDIATE`, has one document row when the second write fails with `no such table: no_such_table`, and has **zero rows after `ROLLBACK`**. The engine is Node's SQLite `3.54.0`; its full source ID comes from the same connection. Pinned Durable source opens through the same adapter. This probe does not run Durable’s own commit logic.

Changing the scratch adapter to `COMMIT` on error leaves the first row behind. The unchanged original then fails when it tries to create the same document again: SQLite reports a unique constraint error. Each targeted run selects one original and leaves the other 55 unselected. The upstream checkout, retained installation and fixture sources stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-series-rollback.XXXXXX")
bash "$inventory/probe-chord-series-rollback.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-series-rollback.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers the selected group on one host. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and the rest of the Chord store suite remain outside this receipt.
