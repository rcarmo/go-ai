# Chord JSON copying across an object retry

The unchanged `ref.retry()` document-retention case in the changed `packages/chord/test/host/object.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`. The other 81 file cases were unselected. The [machine receipt](chord-json-retry.json) records the exact case name and pinned file bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-json-retry-witness.mjs) records the evaluated Delta tracker and JSON modules. It forwards the actual `copyJson` call for the scalar inserted into the draft: `"a"` in, `"a"` out. The unchanged original verifies that the replacement object attempt reads the stored document `{items:["a"]}`. The receipt pins the Durable submission path that calls the same copy function before storage. That path did not run in this probe.

Substituting `"b"` for the targeted scalar in the scratch copy function leaves the input and type unchanged. The original fails its exact document assertion at line 1230. The fault run selects the same one original. The upstream checkout, retained installation and test file stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-json-retry.XXXXXX")
bash "$inventory/probe-chord-json-retry.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-json-retry.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one original on one host. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and the rest of the Chord object suite remain outside the receipt.
