# Chord contract fingerprints and the shared JSON guard

All **13 unchanged cases** in the changed `packages/chord/test/contracts/fingerprint.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures or skips. The [machine receipt](chord-fingerprint.json) lists their exact names and the source digest from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-fingerprint-witness.mjs) records the evaluated Chord fingerprint and JSON modules. In a separate run of the annotation-insensitivity case, the actual `isJsonValue` call accepts the fixture title `"T"`, and both contract materializations return `729ae3ad64a1877e`. The receipt pins the Durable signal path that calls the same JSON guard. The Durable path did not run here.

Keeping annotations in the scratch normal form leaves JSON validation unchanged but changes the annotated contract's fingerprint to `72a19def00e70d10`. The plain contract keeps its original fingerprint. The unchanged equality assertion at line 57 fails. Each targeted run selects one original and leaves the other 12 unselected. The upstream checkout, retained installation and test file stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-fingerprint.XXXXXX")
bash "$inventory/probe-chord-fingerprint.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-fingerprint.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one host and one changed shared suite. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and other Chord suites remain outside the receipt.
