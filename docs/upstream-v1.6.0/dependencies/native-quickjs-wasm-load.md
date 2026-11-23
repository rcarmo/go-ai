# QuickJS WASM input at native runtime

One `NewEngine` run passed the **637,405 bytes** of `package/quickjs.wasm` from the verified QuickJS-WASI `3.6.2` archive to `wazero.Runtime.CompileModule` and ran the translated inline JSON round-trip case through `Engine.Execute`. The [`SHA-256` and count receipt](native-quickjs-wasm-load.json) records the exact input observed in the successful compile. The [separate Oracle receipt](quickjs-wasm-load.md) identifies the unchanged test from pi v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac`; the Oracle receipt reproduced byte for byte.

The [native probe](probe-native-quickjs-wasm-load.sh) runs the same three scripts in fresh Go test processes against the unmodified engine, the observed engine, and the control. Each execution must succeed, return the exact object `{"a":1,"b":[true,"x"]}`, the string `"plain"`, or an absent value as applicable, and produce no output or calls. A Go build overlay wraps the compiler call in the observed process. The wrapper forwards the actual input and retains a copy only when compilation succeeds; the run requires exactly one such call. It runs the same `NewEngine` and `Engine.Execute` code as the baseline apart from the observer. The archive matches the observed bytes with `cmp`.

The control appends a nine-byte valid custom section to a scratch file and overlays the embedded asset. The same three executions pass, while both the byte comparison and the SHA-256 distinguish the 637,414 observed bytes from the archive. No checked-in source or asset is changed by the probe. The native evidence base was `08b02b9d17d493f1863263b290b0d1a7915505c4`; the run used Go `1.27.3`, `CGO_ENABLED=0` and wazero `v1.12.0` on Linux arm64.

## Reproduce offline

```sh
source scripts/project-env.sh
ledger=docs/upstream-v1.6.0/dependencies
receipt=$(mktemp "$TMPDIR/native-quickjs-wasm-load.XXXXXX")
bash "$ledger/probe-native-quickjs-wasm-load.sh" "$pinned_git_mirror" "$HOME/.npm/_cacache" > "$receipt"
cmp "$ledger/native-quickjs-wasm-load.json" "$receipt"
rm -- "$receipt"
```

The probe requires the existing Go toolchain and module cache, `flock`, `jq`, `openssl` and the retained npm archive. It verifies the archive's SHA-512 against the pinned Git lock and its SHA-256 against the prior Oracle receipt. It locks the focused builds, disables module-network access, and deletes its scratch files on exit. The receipt reproduced on two runs; focused namespace and bridge tests, vet and Staticcheck `v0.8.2` also passed for `./internal/quickjs`.

This receipt covers one native inline module compile followed by three executions. The published portable guest, Node worker, historical builds and other native or upstream dependencies require separate evidence.
