# QuickJS input in the published portable Codemode guest

The published pi-codemode `1.6.0` portable endpoint compiled and instantiated the **637,405 bytes** of QuickJS from the lock-verified QuickJS-WASI `3.6.2` archive. The unchanged native `TestSourceV160CodemodePublishedWorkerNamespacedGlobals` passed through the real subprocess bridge and endpoint. It required the exact result `[["list","first"],"a",false]`, callback arguments `[["classifier",null,3],[]]`, and three deliveries.

The [witness](portable-quickjs-witness.mjs) records the source bytes returned by Node's ESM loader for the published `portable.js`, `wasm.js`, and `quickjs-wasi/dist/index.js`. It hashes the input to `WebAssembly.compile` at the published `loadQuickJSWasm` call and tracks the resulting module object to one successful `WebAssembly.instantiate` in QuickJS-WASI. All five observations match the archive members in the [receipt](portable-quickjs-load.json). The WASM archive member also compares byte for byte with the native embedded asset; the subprocess in this run loads the archive's asset.

The control appends a valid nine-byte WASM custom section to a scratch copy. The same native endpoint test and JS-load checks pass, but the compile and instantiation observations identify 637,414 bytes and both archive comparisons reject them. No production source, retained archive, or checked-in asset changes.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/portable-quickjs-load.XXXXXX")
bash "$ledger/probe-portable-quickjs-load.sh" "$pinned_git_mirror" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > "$actual"
cmp "$ledger/portable-quickjs-load.json" "$actual"
rm -- "$actual"
```

The probe verifies the archive digests, builds the existing native test with Go `1.27.3` and CGO disabled, and uses the shared lock for the build and both focused runs. Its Node `v26.13.0` child has a scrubbed environment and no network permission. The receipt reproduced byte for byte on two runs from native base `4cff3ac164d79b3acb8399e311c342d59c91f6dc` on Linux arm64.

The receipt records the pinned upstream sandbox test's source and lines as the native test's reference. The probe does not run that upstream test. It covers the published portable endpoint in this Node subprocess, not the standalone Node worker topology, a Cloudflare host, historical runs, or every transitive module.
