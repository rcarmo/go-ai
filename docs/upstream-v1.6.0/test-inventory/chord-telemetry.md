# Chord telemetry: 23 unchanged cases

All **23** cases in the changed `packages/chord/test/telemetry.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures, skips, or todos. The [machine receipt](chord-telemetry.json) lists every name, including the six parameterised malformed-header cases. The file is in the shared-dependency queue at pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`. Both primary packages import the same Telemetry package: AI in `src/telemetry.ts:10` and Durable in `src/harness/telemetry.ts:1`.

The unchanged Chord configuration selects workspace source. The [observer](chord-telemetry-runtime-witness.mjs) records the successful evaluation and exports of pinned Telemetry `memory.ts` and `render.ts` at the Vitest module evaluator. The receipt records the exact source digests separately from the runtime observation; it does not identify those hashes as transformed JavaScript. The same run observed **three actual SQLite connections** creating the telemetry schema. All three returned SQLite `3.54.0` and the same full source ID from Node's built-in `node:sqlite`.

Replacing the renderer's return value with `wrong trace tree` in scratch failed the unchanged relay-trace case and no other selected case. This confirms that the original checks the evaluated renderer's output. No test, upstream source tree, or retained dependency was changed.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/chord-telemetry.XXXXXX")
bash "$inventory/probe-chord-telemetry.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > "$actual"
cmp "$inventory/chord-telemetry.json" "$actual"
rm -- "$actual"
```

The probe stages exact pinned Git source and config, copies the host-compatible retained runner dependencies, and verifies the Vitest archive integrity and exact launcher, CLI, and evaluator members. It also checks the published Chord and Telemetry archive digests; the source run does not load their `dist` modules. Both runs use the shared lock and a fresh network namespace, with no install, credential, or network access. The receipt reproduced byte for byte.

This covers one changed shared-dependency suite, rather than all Chord or Telemetry suites. It does not map these assertions to Go, verify the rest of the runner's transitive inputs, or establish parity on other hosts.
