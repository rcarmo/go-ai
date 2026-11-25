# Verified v1.6.1 inputs

The oracle is the Git tree at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. Before installing dependencies, the exported oracle matched all 4,851 Git blobs, executable modes and the exact non-directory path set. Its root npm lockfile SHA-256 is `f05391f49b31146692ff2ac76239d78a52fe72d0d694a1624e8580a6ea8fec41`.

## Release assets and npm closure

- The source tarball and two coding-agent install assets match GitHub's published [digests and sizes](release-assets.jsonl). Both install files also match the pinned Git blobs. The source tarball is not the Git oracle: it includes 43 extra provider JSON payloads and their manifest, and two Windows scripts use CRLF instead of the Git blobs' LF. The released archive was preserved separately from the Git export.
- Six [npm tarballs](package-archives.jsonl) match the npm registry SHA-512 digests. The packed names, versions and dependency fields match their source manifests. The registry metadata does not supply a Git commit attestation.
- The [lockfile ledger](npm-closure.jsonl) resolves the AI, Codemode, Durable and Schema roots and their linked dependencies: six workspaces, 305 npm placements and 547 edges. Of those edges, 522 resolve and 25 are absent optional peers. The ledger includes development and platform-constrained placements; it is not an installation or native correspondence claim.
- A workspace-only `npm ci` with lifecycle scripts disabled installed the exact Codemode test scope on Linux arm64 with Node 26.13.0 and Vitest 4.1.12. The locked `quickjs-wasi` 3.6.2 tarball matches its SHA-512 integrity; its WASM matches both the installed module and native embedded bytes (`d4c9375f2b1ca4dc95f72c8aa2982a7a9951ac8011490d79c6582df732b4bbd9`). Installation and byte identity do not establish host parity. The Durable original-test workspace is not yet installed at this pin.
- The 43 published provider payloads match the source tarball payloads byte for byte and the schema-v6 catalog's file hashes. The two manifests differ only in `generatedAt`. The published generated model module has SHA-256 `523505cb45698abc4f8073d6e64feefbde471fa7d7586962f95e2af7262d1777`. No Go catalog was regenerated and no generation pin was changed.

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
