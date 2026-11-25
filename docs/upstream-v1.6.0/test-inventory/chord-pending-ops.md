# Chord pending replacements under backpressure

Both unchanged codec variants of Chord's pending-window case passed on Node `v26.13.0` and Vitest `4.1.12`. The other 134 cases in the changed `packages/chord/test/remote/provider.test.ts` were unselected. The [machine receipt](chord-pending-ops.json) records the exact case names and pinned source bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-pending-ops-witness.mjs) records the actual evaluation of the provider and `PendingOps` modules. It forwards calls to the real pending queue and records only scalar replacements. Each codec variant queues the three root replacements `2`, `3`, and `4` in order. The queue returns exactly `[["r",4]]`; the unchanged test checks that the waiting frame is encoded once. Durable's document-hold path imports and calls the same `PendingOps.push` implementation. That path did not run in this probe.

Keeping the first root replacement in scratch leaves the three incoming operations unchanged but makes the queue return `[["r",2]]`. The unchanged binary original fails its exact equality assertion at line 671. The fault run selects this one original and leaves the other 135 unselected. The upstream checkout, retained installation and test file stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-pending-ops.XXXXXX")
bash "$inventory/probe-chord-pending-ops.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-pending-ops.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers these two fixture cases on one host. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and the rest of the Chord provider suite remain outside the receipt.
