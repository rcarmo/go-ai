# QuickJS in the published Node Codemode worker

The published pi-codemode `1.6.0` package ran the pinned namespaced-globals script through its default Node worker, `dist/runtime/worker.js`. The worker returned `[["list","first"],"a",false]` and the callbacks received exactly `[["classifier",null,3],[]]`. The public `/worker` export resolved to the same file the host launched. No package installation was needed.

The [standalone fixture](published-worker-namespaced-case.mjs) extracts the guest script from `packages/codemode/test/sandbox.test.ts:506–523` at `faac585884ae2c013a6b1400f3e9e77f10b34bac` and checks the original result and arguments. The separate [source-worker receipt](worker-quickjs-load.md) covers the unchanged Vitest original; it reproduced unchanged with the shared observer. The archive-backed run uses the published package's public entry in plain Node in place of the original test's source import.

The [receipt](published-worker-quickjs-load.json) records five module loads from the verified archives: `dist/index.js`, `dist/runtime/host.js`, `dist/wasm.js`, `dist/runtime/worker.js`, and `quickjs-wasi/dist/index.js`. The host compiler received **637,405 bytes** matching the lock-verified QuickJS-WASI `3.6.2` archive and passed that module as `workerData.wasm`. The worker instantiated the received object and sent the successful terminal response. The WASM digest comes from the host compiler input; the receiver observation checks object identity within the worker.

Appending a valid nine-byte custom section to the scratch WASM left the public result and callback arguments unchanged. Both archive checks rejected the **637,414 bytes** observed at compilation and handoff. No archive, checked-in asset, or production code was changed.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/published-worker-quickjs-load.XXXXXX")
bash "$ledger/probe-published-worker-quickjs-load.sh" "$pinned_git_mirror" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > "$actual"
cmp "$ledger/published-worker-quickjs-load.json" "$actual"
rm -- "$actual"
```

The probe extracts the two verified local archives to scratch and runs under the shared verification lock with no network permission. The receipt reproduced byte for byte twice with Node `v26.13.0` on Linux arm64. This covers the published default worker on Node for the selected case; Cloudflare, alternate builds, and other transitive loads were outside the probe.
