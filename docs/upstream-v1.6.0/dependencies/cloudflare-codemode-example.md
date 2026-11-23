# Codemode's Cloudflare example on local workerd

The pinned two-Worker Codemode example passed its documented request on real local **workerd 2026-10-23**. The original agent called the original sandbox over a Durable Object binding to a separate named Worker. It returned HTTP `200`, value `2`, text `n=2`, and one successful `count` callback.

The pinned Codemode package has a Cloudflare example but no dedicated Cloudflare test suite. The [supplemental harness](cloudflare-codemode-probe.capnp) runs both unchanged example source files and the exact README request under `workerd test`. It reproduces the separate-Worker binding and pinned compatibility date; it uses an internal service binding instead of an HTTP listener and does not run Wrangler or deploy either Worker. The Durable package's separate Cloudflare tests were outside this run.

The [receipt](cloudflare-codemode-example.json) identifies the executable, config schema, bundler and QuickJS archive member by exact SHA-256. The probe extracts the Linux arm64 workerd and esbuild binaries, schema, and WASM from their lock-verified local npm archives and compares the selected members to the retained Oracle. It bundles both example entry points with the verified published pi-codemode `1.6.0` package. The config binds the **637,405 archive bytes** as a compiled QuickJS WASM module. A separate witness entry in the sandbox Worker checks that the module imported by Codemode is that same `WebAssembly.Module` and has 450 exports. This records the configured compiler input and the live module identity; it does not recover raw bytes from V8's compiled module.

A scratch control appends nine bytes as a valid named WASM custom section. The same request and result still pass, but the archive byte comparison rejects the changed input and the live module witness reports the new `audit` section. This distinguishes the compiled module used by the sandbox from an unchanged file sitting on disk.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/cloudflare-codemode.XXXXXX")
bash "$ledger/probe-cloudflare-codemode-example.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > "$actual"
cmp "$ledger/cloudflare-codemode-example.json" "$actual"
rm -- "$actual"
```

The probe needs Linux arm64 with `unshare -Urn`, the pinned Git commit, and the retained archives. It runs the verified executables in a fresh network namespace with a scrubbed environment under the shared verification lock. No install, credential, network connection, retained-data edit, or hosted deployment is needed. The receipt reproduced byte for byte twice. Other Cloudflare cases, hosted isolation, and native Go Cloudflare parity were not verified.
