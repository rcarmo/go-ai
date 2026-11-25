# Chord derived-watch failure recovery

All **23 unchanged cases** in the changed `packages/chord/test/state/derive.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-derive-watch.json) lists their exact names and pinned bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-derive-watch-witness.mjs) records the successful evaluation of Chord's `Derive` and `Watch` modules. A separate run selects the busy-listener test and forwards the actual `Watch` calls and listener promise. The source sends value `5`, failure `t threw`, and value `6`, in that order. The listener receives only `5` followed by `6`; both carry a root replacement (`[["r", 5]]` and `[["r", 6]]`). The receipt pins the Durable task-graph import and call to the same `Watch.advance` method. The Durable path did not run in this probe.

Keeping the superseded failure in the scratch `Watch.advance` implementation leaves all three source updates unchanged, but delivers `t threw` instead of `6` as the second listener call. The original assertion at line 377 fails. Each targeted run selects one original and leaves the other 22 unselected. The upstream checkout and retained installation stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-derive-watch.XXXXXX")
bash "$inventory/probe-chord-derive-watch.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-derive-watch.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one host and one changed shared suite; the TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go correspondence and other Chord suites remain outside this receipt.
