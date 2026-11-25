# Chord text codec and shared Schema validation

All **12 unchanged cases** in the `the text codec` group of `packages/chord/test/link/message.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`. The other 160 file cases were unselected. The [machine receipt](chord-text-codec.json) lists the exact passing names and the file digest from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-text-codec-witness.mjs) records the successful evaluation of Chord's `checkMessage` and pi-schema's `compile`. A separate run forwards the live `compile().check` call: the text codec decodes a fixture with numeric `old: 1`, and the compiled schema rejects that decoded value. The receipt pins the AI and Durable tool-validation imports of the same compiler. Their tool paths did not run in this probe.

Allowing numeric `1` in the scratch schema preserves the decoded value but flips the validator result to `true`. The unchanged assertion at line 688 fails because the boundary no longer rejects the fixture. The two targeted runs each select one original and leave the other 171 unselected. The upstream checkout, retained installation, and test file stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-text-codec.XXXXXX")
bash "$inventory/probe-chord-text-codec.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-text-codec.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers the selected group on one host. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and the rest of the Chord link suite remain outside the receipt.
