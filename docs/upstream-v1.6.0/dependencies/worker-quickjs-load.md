# QuickJS transferred to the standalone Node Codemode worker

The unchanged pi v1.6.0 namespaced-globals case passed through a real Node worker thread. The host compiled **637,405 bytes** matching the lock-verified QuickJS-WASI `3.6.2` archive. The worker instantiated the module it received and returned `[["list","first"],"a",false]`. The original test also checked the exact callback arguments `[["classifier",null,3],[]]`.

The [witness](worker-quickjs-witness.mjs) hashes the bytes passed to `WebAssembly.compile` in the pinned host's `loadQuickJSWasm`. It checks that the host passes that module object as `workerData.wasm` to `new Worker` and that the worker successfully instantiates the object it receives. The receiver-side observation checks object identity within the worker; the byte digest comes from the host compiler input. The worker's ESM loader returned **1,560 bytes** of `src/runtime/worker.ts` matching pinned Git and **115,939 bytes** of the QuickJS-WASI wrapper matching the archive. The host observed the worker's successful terminal message.

Appending a valid nine-byte WASM custom section to the scratch input left the same original and result checks passing. The compile and handoff observations identified **637,414 bytes** and both archive checks rejected them. The [receipt](worker-quickjs-load.json) records the exact digests and events in both runs.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/worker-quickjs-load.XXXXXX")
bash "$ledger/probe-worker-quickjs-load.sh" "$pinned_git_mirror" "$oracle" "$HOME/.npm/_cacache" > "$actual"
cmp "$ledger/worker-quickjs-load.json" "$actual"
rm -- "$actual"
```

The probe stages the original test, source and Vitest config from `faac585884ae2c013a6b1400f3e9e77f10b34bac`. It copies the retained runner dependencies and extracts the QuickJS package from the verified local archive. No install, network permission, source patch or production instrumentation is required. Both selection and control run under the shared verification lock. The receipt reproduced byte for byte with Node `v26.13.0` and Vitest `4.1.12` on Linux arm64. The separate Go inline namespaced test passed twice; it does not exercise this Node worker.

This receipt covers one standalone worker from pinned TypeScript source. It does not cover the published `dist` worker entry, the portable subprocess, Cloudflare, or the runner's transitive module loads.
