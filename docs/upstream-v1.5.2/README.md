# v1.5.2 source delta

The working source target is [pi v1.5.2](https://github.com/earendil-works/pi/releases/tag/v1.5.2) at `b4afdddf44e059c67ccf5097e5317d913521b470`. The prior release is `f9a26365568127461c47a07a37dcb42c140a3ac5`. The [restart notes](../../notes/upstream-v1.5.2-restart.md) record the assertion mappings and focused results so far.

[`source-delta.tsv`](source-delta.tsv) inventories 242 changed paths in AI, Durable, Codemode, Schema, Telemetry, the workspace lockfile and the directly imported Chord subtrees. Rename detection is off, so renames appear as a removal and an addition. This is a review list, not an assertion ledger or a list of approved exclusions. Transitive Chord paths and unchanged tests still need review.

Verify the inventory and the corresponding 1,087 canonical file blobs against an extracted checkout:

```sh
bun run scripts/inventory-upstream-v152.ts /path/to/git-repository-with-release-tags /path/to/v1.5.2-source
```

The tool checks both tag commits and the contents of every tracked file in this scope, including unchanged tests. Generated files added to the extracted tree for builds are not part of the source tree inventory. `--write` regenerates the checked-in delta. The existing v1.5.0 acceptance register remains historical; this verification does not switch it or establish parity.
