# Chord contract schemas: nine unchanged cases

All **nine** cases in the changed `packages/chord/test/contracts/schema.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-contract-schema.json) lists their exact names and pinned test bytes. This shared-dependency suite exercises pi-schema's runtime compiler, also imported by the primary AI and Durable tool validators.

The [observer](chord-contract-schema-witness.mjs) records the successful evaluation of Schema's `compile` and `bytesCheck` exports in the unchanged Chord configuration. The recorded SHA-256 values describe the pinned TypeScript source, not Vitest's transformed JavaScript. Removing the upper byte-length check from the scratch dependency made the unchanged `Uint8Array` bounds case fail because it accepted three bytes where the schema allowed two. The unmodified test file was used for both runs.

The original declaration case also launched the verified native **TypeScript 7.0.2** executable. The observer saw the archived launcher hand off through `process.execve`, the compiler exit `0`, and the actual **5,796-byte** declaration before the original deleted its fixture. The declaration contains `Method<` and no import that reaches inside `node_modules`. The receipt includes the compiler, fixture and emitted declaration digests. This is a real declaration check for that fixture; it does not type-check the whole workspace or turn Vitest-only `expectTypeOf` assertions into compiler evidence.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-contract-schema.XXXXXX")
bash "$inventory/probe-chord-contract-schema.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/chord-contract-schema.json" "$actual"
rm -- "$actual"
```

The probe verifies the pinned Chord source and the retained Vitest and TypeScript archive bytes. It runs in scratch under the shared lock and a fresh Linux network namespace. The original creates a symlink inside its fixture, so the probe uses kernel network isolation instead of the Node filesystem permission mode that refuses `symlinkSync`. It needs Linux arm64 with `unshare -Urn` and no installs, credentials, or network access. The receipt reproduced byte for byte.

This covers one changed shared suite and its declaration fixture. Other Chord and Schema suites, complete transitive archive verification, other hosts, and native Go correspondence remain outside the receipt.
