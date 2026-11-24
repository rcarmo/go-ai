# The specification-usage TypeScript check

The unchanged `spec-usage.test.ts` type-checks with TypeScript **7.0.2** when the focused program includes the pinned IndexedDB source file that is also a root of the workspace program. The inherited options stay unchanged: `lib: ["ES2024"]`, `types: ["node"]`, and NodeNext resolution. The IndexedDB file explicitly requests the DOM library, so the workspace's declared options do not mean its actual program excludes DOM.

The [probe](probe-spec-usage-types.sh) first enumerates the file graph of the unmodified root `tsconfig.json` at pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`. It finds the unchanged test, `packages/durable/src/storage/indexeddb.ts`, and `lib.dom.d.ts`. This step lists files and does not check the full workspace. It then compares `--showConfig` and verifies that the two focused programs inherit the same compiler options. Their only root difference is the pinned IndexedDB file, which starts with `/// <reference lib="dom" />`.

| Program | Result |
| --- | --- |
| Original test and its import closure, with the three needed tracked ambient declaration files | Exit `1`, one diagnostic: `writer.ts(355,17)` TS2339, `Property 'onmessage' does not exist on type 'MessagePort'.` No DOM declarations are in this program. |
| Same roots plus pinned `storage/indexeddb.ts` | Exit `0`, no diagnostics. Its original library reference brings in DOM without changing any compiler option or source. |
| Same green program with a scratch-only type assertion fault | Exit `1`, one TS2345 at the unchanged assertion site `spec-usage.test.ts:641`. The compiler rejects `notARealProperty` and lists the eight valid keys, including `sequences`. |

The bare import-closure error is real for its reduced program. It is not evidence that the workspace's own compiler command fails. The green result is a focused compiler check of the actual specification assertion, separate from the prior Vitest runtime result. Neither the full workspace type check nor native Go correspondence ran here.

## Reproduce offline

```sh
source scripts/project-env.sh
inventory=docs/upstream-v1.6.0/test-inventory
actual=$(mktemp "$TMPDIR/spec-usage-types.XXXXXX")
bash "$inventory/probe-spec-usage-types.sh" "$pinned_git_mirror" "$oracle" \
  "$HOME/.npm/_cacache" > "$actual"
cmp "$inventory/spec-usage-types.json" "$actual"
rm -- "$actual"
```

The probe reconstructs six packages from retained lock-verified archives: the Linux arm64 compiler and standard libs, `@types/node`, `undici-types`, and the three incomplete declaration packages (`@anthropic-ai/sdk`, `partial-json`, and `diff`). The compiler executable and Node worker-thread declaration also match the prepared Oracle byte for byte. Remaining imports resolve from the prepared Oracle read-only; the receipt does not verify each transitive package archive. The compiler runs under the shared lock in a network namespace with a scrubbed environment. The [receipt](spec-usage-types.json) reproduced byte for byte. No installation, tracked source change, credential, or cache write was needed.
