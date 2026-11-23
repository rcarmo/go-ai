# Photon input during a Durable image-worker run

The unchanged pi v1.6.0 resize test passed with a Node image worker that loaded **139,647 bytes** of Photon JavaScript and compiled and instantiated **1,880,773 bytes** of Photon WASM. Both observed inputs match the members of the lock-verified Photon `0.3.3` archive. The [receipt](photon-runtime.json) has the exact byte counts and SHA-256 values.

The [probe](probe-photon-runtime.sh) selects `packages/durable/test/images-fit.test.ts:99` at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The case verifies the order and identity of surrounding text, an image resized from 400×100 to 200×50, and the exact note for mapping coordinates back. A [preload observer](photon-runtime-witness.mjs) hashes the bytes returned from Node's ESM load hook for `photon_rs.js` and the bytes passed to `WebAssembly.Module` for the instance that successfully starts. The observer also records the worker's actual PNG reply and both dimensions, so this run cannot pass the witness through the fallback path alone.

The test thread separately compiles and instantiates the same WASM for the image fixtures. The Node ESM hook exposes the worker's glue source; it does not expose Vitest's test-thread glue load. The runner uses the unchanged pinned test and config, Node `v26.13.0`, Vitest `4.1.12` and the verified published AI `dist` prerequisite.

A scratch control appends a 31-byte JavaScript comment and a nine-byte WASM custom section. The original resize test and worker reply checks still pass, but the archive comparison rejects each changed input. The probe never changes the retained package, published archive, or production source.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
actual=$(mktemp "$TMPDIR/photon-runtime.XXXXXX")
bash "$ledger/probe-photon-runtime.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > "$actual"
cmp "$ledger/photon-runtime.json" "$actual"
rm -- "$actual"
```

The probe uses local Git and retained dependencies. It serialises the two focused runs with the shared verification lock and grants Node no network permission. The receipt reproduced byte for byte on two runs. Separately, the existing Go `TestSourceV160FitImages` and `TestSourceV160FitImagesFormats` passed twice with shuffle seed 42. Vet and Staticcheck `v0.8.2` passed for `./durable/tools`.

This receipt covers one current Node image-worker run and its fixture WASM load on Linux arm64. The native implementation uses Go image operations, so the native tests do not load Photon. Other Photon paths, historical loads, and transitive runner inputs require separate evidence.
