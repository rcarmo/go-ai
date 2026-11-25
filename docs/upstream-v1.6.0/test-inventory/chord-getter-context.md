# Chord getter cancellation and the shared Context

The unchanged getter cancellation case in the changed `packages/chord/test/host/host.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`. The other 101 file cases were unselected. The [machine receipt](chord-getter-context.json) records the exact case name and pinned file bytes from pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

The [observer](chord-getter-context-witness.mjs) records the live evaluation of Chord's Context and host modules. It forwards the original `withCancel` call and cancellation, then checks the actual signal: the child aborts with the exact supplied `Error("cancelled")` object and the parent stays un-aborted. The unchanged original checks that the other getter succeeds with value `1` and that the factory runs only once. The receipt pins the Durable harness's use of `withAbortSignal`, the same signal-binding helper called by `withCancel`; the Durable path did not run here.

Dropping the caller's reason in scratch still aborts the child but replaces the supplied error with Node's default `AbortError`. The unchanged exact-reason assertion at line 791 fails. The upstream checkout, retained installation and test file stay unchanged.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-getter-context.XXXXXX")
bash "$inventory/probe-chord-getter-context.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-getter-context.json" "$actual"
rm -- "$actual"
```

The probe verifies the generated inventory locator, stages pinned Git source, and compares the retained Vitest launcher, CLI and evaluator with their lock-verified archive. It runs under the shared lock in a fresh network namespace, without installs or credentials. The receipt reproduced byte for byte. It covers one original on one host. The TypeScript hashes describe source inputs, not Vite's transformed output or every transitive module. Native Go and the rest of the Chord host suite remain outside the receipt.
