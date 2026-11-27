# Verified v1.6.1 inputs

The oracle is the Git tree at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. Before installing dependencies, the exported oracle matched all 4,851 Git blobs, executable modes and the exact non-directory path set. Its root npm lockfile SHA-256 is `f05391f49b31146692ff2ac76239d78a52fe72d0d694a1624e8580a6ea8fec41`.

## Release assets and npm closure

- The source tarball and two coding-agent install assets match GitHub's published [digests and sizes](release-assets.jsonl). Both install files also match the pinned Git blobs. The source tarball is not the Git oracle: it includes 43 extra provider JSON payloads and their manifest, and two Windows scripts use CRLF instead of the Git blobs' LF. The released archive was preserved separately from the Git export.
- Six [npm tarballs](package-archives.jsonl) match the npm registry SHA-512 digests. The packed names, versions and dependency fields match their source manifests. The registry metadata does not supply a Git commit attestation.
- The [lockfile ledger](npm-closure.jsonl) resolves the AI, Codemode, Durable and Schema roots and their linked dependencies: six workspaces, 305 npm placements and 547 edges. Of those edges, 522 resolve and 25 are absent optional peers. The ledger includes development and platform-constrained placements; it is not an installation or native correspondence claim.
- A workspace-only `npm ci` with lifecycle scripts disabled installed the exact Codemode test scope on Linux arm64 with Node 26.13.0 and Vitest 4.1.12. The locked `quickjs-wasi` 3.6.2 tarball matches its SHA-512 integrity; its WASM matches both the installed module and native embedded bytes (`d4c9375f2b1ca4dc95f72c8aa2982a7a9951ac8011490d79c6582df732b4bbd9`). Installation and byte identity do not establish host parity. The [Durable Node scope](durable-node-tests.md) adds only its pinned runtime packages to that runner.
- The 43 published provider payloads match the source tarball payloads byte for byte and the schema-v6 catalog's file hashes. The two manifests differ only in `generatedAt`. The published generated model module has SHA-256 `523505cb45698abc4f8073d6e64feefbde471fa7d7586962f95e2af7262d1777`. This input check does not regenerate the Go catalog.

## Reproduce the static checks

Use a new empty directory for the Git export, preserving the release tarball separately. Git archives perform checkout line-ending conversion. Restore the two Windows scripts from Git objects before verifying the raw blob bytes:

```sh
pin=ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe
git -C "$PI_MIRROR" archive "$pin" | tar -xf - -C "$PI_ORACLE"
for path in pi-test.bat pi-test.ps1; do
  git -C "$PI_MIRROR" show "$pin:$path" > "$PI_ORACLE/$path"
done
bun run docs/upstream-v1.6.1/dependencies/verify-source.ts "$PI_MIRROR" "$PI_ORACLE"
bun run docs/upstream-v1.6.1/dependencies/generate.ts "$PI_MIRROR" "$PI_ORACLE" "$PI_NPM_ARCHIVES" \
  | cmp - docs/upstream-v1.6.1/dependencies/npm-closure.jsonl
```

## Published catalog changes

The [catalog delta](catalog-module-delta.jsonl) compares complete exported records from the verified v1.6.0 and v1.6.1 npm modules by provider and ID, separately for each model type. The comparison ignores object key order but preserves array order and values. Repeating it on two copies of the v1.6.1 module finds no changes.

| Model type | v1.6.0 | v1.6.1 | Added | Removed | Changed |
| --- | ---: | ---: | ---: | ---: | ---: |
| Chat | 1,698 | 1,696 | 63 | 65 | 138 |
| Image | 74 | 74 | 0 | 0 | 0 |
| Classifier | 81 | 81 | 5 | 5 | 4 |

Use `bun run docs/upstream-v1.6.1/dependencies/catalog-module-delta.ts "$V160_MODELS_JS" "$V161_MODELS_JS"` with the two verified archives' intact `dist` trees. This compares upstream packages only. The [Round 2 comparison](../README.md#the-go-catalog-regenerates-from-the-verified-v161-archive) separately verifies the native generation, registry and local gate.

## Google SDK input used by the stream tests

The [`@google/genai` 3.6.1 receipt](google-sdk-input.json) records the exact npm lock placement and verified archive. The packed package manifest and Node entry point match the staged oracle bytes. Node loaded the `GoogleGenAI` export in a network-isolated process. This verifies the SDK input used by the scoped upstream comparisons, not a live SDK connection or constructor equivalence. The full static workspace closure also reproduced byte for byte.

## Bedrock SDK input

The [Bedrock SDK receipt](bedrock-sdk-input.json) compares the pinned `@aws-sdk/client-bedrock-runtime` 3.1127.0 npm archive with the staged manifest and CommonJS entry. An isolated Node import of the unchanged upstream provider loaded the same entry. The probe did not construct a client, send a request or run the upstream Bedrock tests. Run [the probe](probe-bedrock-sdk-input.sh) with the pinned Git mirror, staged oracle and npm `_cacache` directory to reproduce the receipt offline.

## Photon image worker

The [Photon receipt](photon-codec-input.json) records archive, JavaScript glue and WebAssembly byte checks for Photon 0.3.3. The unchanged Node BMP-to-PNG test passed offline, and a trace confirmed that the worker loaded the verified glue and instantiated the matching WASM. A comment-only change to the private glue copy kept the test green but failed the archive identity check. This did not compare the Go encoder or run a workerd worker.

## Shipped coding-agent installer

The [installer receipt](coding-agent-install-input.json) compares the shipped manifest and lock with the release source, pinned Git and workspace lock. Of 122 install placements, 88 match the workspace closure at the same slot and artifact; 24 match workspace lock entries outside that closure; two move under published `pi-ai`; and eight workspace links become registry entries. Seven optional peers are absent. The read-only upstream lock check passed. A separate offline probe loaded the pinned `photon-node` wrapper and round-tripped exact pixels through PNG. Neither probe ran an npm install, lifecycle scripts or the CLI.

## Cloudflare Codemode example on local workerd

The [workerd receipt](cloudflare-codemode-example.json) distinguishes the lock-verified Linux arm64 engine from the example run. With the exact v1.6.1 example and published Codemode archive, the local two-Worker fixture returned `200`, `2`, `n=2`, and one successful `count` call. A scratch WASM mutation appeared in the live module witness without changing that result. This did not install or run the Durable Cloudflare test suite, Wrangler, or a hosted Worker.
