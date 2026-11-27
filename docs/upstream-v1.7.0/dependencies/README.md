# v1.7.0 dependency and release closure

[check-release.ts](check-release.ts) checks the official v1.6.1 and v1.7.0 pins, the four-package npm closure, the published coding-agent installer, both Linux arm64 release bundles, the source archives and the 10 published v1.7.0 workspace packages. [additional-npm-inputs.json](additional-npm-inputs.json) supplies the six publisher archives not listed in v1.7.0's four-package npm inputs.

The exact [22-row JSONL receipt](release-check.jsonl) is 15,608 bytes with SHA-256 `71df6d213370345724aa127f2d256dc12e9de866b8c5b68799a68b984addb6b4`. It records 305 npm placements, six linked workspaces and 547 resolved or absent dependency edges in the workspace closure. The single lock delta outside that closure, `@earendil-works/elanor-react` 0.1.1, matched its SRI and packed manifest. The installer has 122 placements: 88 same-slot closure entries, 24 same-slot entries outside the closure, two rebased entries and eight publisher registry replacements. Its 114 third-party records remain equal; all eight publisher artifacts and 10 publisher dependency requests advance to 1.7.0.

Each release independently matched five downloaded assets (source archive, installer manifest and lock, Linux arm64 bundle and checksum list), the pinned Git objects, the release build recipe and the Photon archive/WASM. Both actual Linux arm64 binaries then ran `--version`, and both smoke scripts returned `PI_CODEMODE_BINARY_SMOKE_OK`. The two runs used an empty user network namespace, an empty HOME/cache/config, `PI_OFFLINE=1`, the checked installer generator, and the published bundle. No network route was available. The scratch extractions explicitly use `--no-same-owner` so a user-namespace extraction cannot attempt to restore archive uid/gid ownership.

## Reproduce

Run from the repository root with the acquired, immutable inputs:

```sh
mirror=/tmp/go-ai/runs/durable-port/upstream-review-1.5.2
V=/tmp/go-ai/v161-inputs.RTYO2d
W=$(cat /tmp/go-ai/v170-round1.current)
T=$(cat /tmp/go-ai/v170-deps-closure.current)

bun run docs/upstream-v1.7.0/dependencies/check-release.ts \
  "$mirror" "$V/release.json" "$V" "$W/release.json" \
  "$W/assets-data" "$T" "$T/npm" "$HOME/.npm/_cacache" \
  > "$T/receipt.hardened"
cmp "$T/receipt.hardened" docs/upstream-v1.7.0/dependencies/release-check.jsonl
sha256sum "$T/receipt.hardened"
```

The checked base and target are `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe` and `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The second is the same exact pin used by the active v1.7.0 inventory.

Two small private fault controls failed as intended: changing one byte of the Elanor archive failed its SRI check, and changing the v1.7.0 source-archive digest in a copied release receipt failed the corresponding asset check. The controls did not alter the staged inputs or retained receipts.

This closes static dependency, publisher-archive, installer and release-bundle provenance. It does not run the upstream or Go acceptance suites, install dependencies, publish anything, or validate live-provider credentials.
