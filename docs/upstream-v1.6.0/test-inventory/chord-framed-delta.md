# Chord framed sources and immutable replay

All **eight** unchanged cases in the added `packages/chord/test/state/framed.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-framed-delta.json) lists the exact case names and suite bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-framed-delta-witness.mjs) records the successful evaluation of the pinned `FramedSource` and `applyImmutable` modules. It forwards the actual replay call and records its input, operations, and return value. The original batches three changes and emits one frame. The observed `applyImmutable` call replays its three ordered set operations on `{a:1,b:1,c:1}` and returns `{a:2,b:3,c:4}` without altering the input. Durable's task-graph and document-view code call the same exported `applyImmutable` function. Their call sites are pinned in the receipt; those Durable paths did not run in this probe.

The control returns the unchanged prior value from the scratch replay function. It leaves the frame operations intact, and the same original fails at its recomposition assertion. The control selects that one case and leaves the other seven unselected. The upstream checkout and retained installation stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-framed-delta.XXXXXX")
bash "$inventory/probe-chord-framed-delta.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-framed-delta.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one host and one changed shared suite; the TypeScript hashes identify source inputs, not Vite's transformed output or every transitive module. Native Go correspondence and other Chord suites remain outside this receipt.
