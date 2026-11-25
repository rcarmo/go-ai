# Chord read plans on Node SQLite

All **21 unchanged cases** in the added `packages/chord/test/store/read-plan.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-read-plan.json) lists their exact names and pinned bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-read-plan-witness.mjs) records the successful evaluation of Chord's `ReadPlan` and `openNodeSqliteDatabase`. It forwards an unchanged `EXPLAIN QUERY PLAN` statement to real `node:sqlite` and records the SQL, parameters and returned steps. SQLite **3.54.0** searched the query's own table (`t`) by `team` first, then the joined table (`p`) by `id`. The same connection provides the engine's full source ID. Durable also calls `openNodeSqliteDatabase`; the receipt pins that call site separately. This does not mean Durable uses Chord's `ReadPlan` or that its adapter ran here.

Reversing the `CROSS JOIN` inputs in scratch leaves the match condition and parameters intact. SQLite then searches `p` first, using its `by_app` index, and the unchanged own-table-first assertion at line 278 fails. The control selects that one original and leaves the other 20 unselected. The upstream checkout and retained installation stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-read-plan.XXXXXX")
bash "$inventory/probe-chord-read-plan.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-read-plan.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one host and one changed shared suite; the TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go correspondence and other Chord suites remain outside this receipt.
