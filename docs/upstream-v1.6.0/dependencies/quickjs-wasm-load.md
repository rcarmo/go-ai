# QuickJS WASM compilation: one pinned Oracle case

The [`WebAssembly.compile` witness](wasm-compile-witness.mjs) observed one successful compile from the pinned `packages/codemode/src/wasm.ts` loader. Its **637,405 input bytes** matched `package/quickjs.wasm` in the [verified QuickJS-WASI `3.6.2` archive](archives.md). They also matched the native embedded asset at `10a9f34c60549069026e02930279f9fdf5228a7f`. The [receipt](quickjs-wasm-load.json) records the exact digest, compiler seam and test selector.

The new run selected **inline backend → script execution → returns the script's return value after a JSON round trip** from the unchanged `packages/codemode/test/sandbox.test.ts`. It passed on Node `v26.13.0` and Vitest `4.1.12`. The hook hashes the bytes passed to the compiler and records them only after compilation succeeds. It does not infer a compile from the installed file path.

Appending a valid empty WASM custom section to the scratch copy changed the compiler input by three bytes. The same original still passed, but the provenance check rejected the digest. The control changes neither the source test nor its comparison.

## Reproduce offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
bash "$ledger/probe-quickjs-wasm-load.sh" "$pinned_git_mirror" "$oracle" "$HOME/.npm/_cacache" \
  > /tmp/pi-v160-quickjs-wasm-load.json
cmp "$ledger/quickjs-wasm-load.json" /tmp/pi-v160-quickjs-wasm-load.json
```

The probe verifies the retained compressed archive against the exact pinned lock, stages source from Git and copies only the selected Vitest/QuickJS npm closure to disposable scratch. It serializes the two focused runs with the shared lock. The scrubbed Node environment permits WASI but does not grant Node network permission. One digest rejection on stderr is expected. No installs, retained-input writes or credentials are needed.

This receipt covers compiler input for one Oracle inline run. The [separate native probe](native-quickjs-wasm-load.md) records one observed `NewEngine` compile and the translated case at native runtime. Other cases and backends were outside the selector. Historical loads, worker and remote semantics, and release acceptance require separate evidence.
