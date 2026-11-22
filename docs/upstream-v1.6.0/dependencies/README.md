# v1.6.0 npm dependency ledger

[`npm-closure.jsonl`](npm-closure.jsonl) follows declarations from the four pinned workspaces: AI, Durable, Codemode and Schema. It walks the exact npm package placements in `package-lock.json` at `faac585884ae2c013a6b1400f3e9e77f10b34bac`, including linked Chord and Telemetry workspaces and development declarations of all reached workspaces. This is distinct from the [source blob inventory](../source-delta.tsv). It does not inventory unrelated monorepo roots or infer dependencies from source imports.

## Read the graph

| Record | Evidence |
| --- | --- |
| `manifest` | Lockfile, root manifest and six workspace manifest hashes. The generator compares every input file byte for byte against the pinned Git object. |
| `workspace` | Source manifest identity and declarations matched to the npm lock entry and link. The local published tarball also matches its independently recorded SHA-256 in the [release archive receipt](../README.md#verified-inputs). This does not assert equality of unpacked npm and Git trees. |
| `npm` | A particular installation slot from the pinned lock, with version, registry URL, SHA-512 integrity, engines, install-script flag and platform metadata when present. Its `lock-record-only` provenance is not proof that the archive bytes or built package were verified. |
| `edge` | Declared name, request and class, with the nearest recorded `node_modules` slot from the consumer's actual package directory and a SemVer check against the effective request. The original range and root override remain separate. |
| `summary` | Scoped graph totals and per-root reachability over resolved edges. The root counts as a workspace node. Only `runtime` edges are used for the strict traversal; the next traversal adds optional and peer edges; the final traversal adds development edges. None is an installed-package or acceptance count. |

The union has **6 workspace entries, 305 npm placements and 546 declaration edges**. Of those edges, 521 resolve and 25 are optional peers with no recorded slot. The latter are absent lock entries, not test skips. The ledger retains 90 platform-constrained npm placements across hosts. `runtime`, `optional`, `peer`, `optional-peer`, and `development` describe the declaration on that edge, not the path that reached it. `lockFlags` preserve npm's separate placement annotations. No package is installed and no platform is selected by this inventory.

For example, AI's `https-proxy-agent` resolves inside `packages/ai/node_modules`, not to the root package. Its `agent-base` dependency also resolves in that workspace. Google GenAI's `protobufjs` edge records both the source range and the pinned root override. Missing required slots and version mismatches have separate statuses; neither is silently accepted as an optional absence.

## Reproduce without network access

From the native repository, with Bun 1.4.4 and locally retained upstream inputs:

```sh
ledger=docs/upstream-v1.6.0/dependencies
bun run "$ledger/generate.ts" "$pinned_git_mirror" "$oracle" "$release_archive_dir" > /tmp/pi-v160-npm-closure.jsonl
cmp "$ledger/npm-closure.jsonl" /tmp/pi-v160-npm-closure.jsonl
```

Use the tracked npm lockfile. A locally generated Bun lockfile is not an Oracle input. The generator checks pinned inputs before writing the ledger and does not download archives, install packages or run install scripts.

## Open D26 correspondence

1. Verify actual third-party archive bytes against lock integrity where the archive was used, starting with `quickjs-wasi`, `@silvia-odwyer/photon`, the provider SDKs and the focused test runner. A workspace archive receipt does not extend to its dependencies.
2. Map runtime dependencies and workspace interfaces to native modules, asset digests and focused behavior receipts. Start with QuickJS/WASI and its replay boundary, Photon/image handling, Chord/Telemetry interfaces, Bedrock/AWS/Smithy and the other provider adapters. Classify native implementations and local replacements explicitly; matching library versions are not required when the behavior matches. Record Go graph and asset provenance separately.
3. Walk non-package dependencies (vendored fixtures and registries, generated provider/model resources, platform engines) and identify which optional and development edges affect the pinned tests. The lock does not establish whether a package was installed, loaded or needed for those tests.
4. Resolve platform and credential requirements through actual evidence. Do not treat absent optional peers, unsupported hosts, or development dependencies as approved test exclusions. Only D25 alternate-engine availability has prior approval.
