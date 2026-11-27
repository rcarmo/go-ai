# pi v1.7.0 native port

**Active target:** [official pi v1.7.0](https://github.com/earendil-works/pi/releases/tag/v1.7.0), `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The official Git tag resolves to this commit. The AI, Codemode, Durable and Schema source manifests all declare `1.7.0` and Node `>=22.19.0`.

**Comparison base:** official v1.6.1, `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`, is an ancestor of v1.7.0. The verified native v1.6.1 checkpoint is `3c59c42f10dbc0ecfb159c8d3f5d2a5302c5100d`. Its [verification log](../upstream-v1.6.1/README.md), assertion ledger and crosswalk remain historical evidence.

## Verified inputs

A clean detached source checkout was staged at the exact pin. Its four package manifests and `package-lock.json` match the Git objects byte for byte. [Input fingerprints](inputs.json) record the manifest and lock SHA-256 values and the four published npm tarballs. Each tarball matched the registry's SHA-512 integrity value; its embedded package name and version matched the source manifest. No packages were installed or tested during the retarget. The results below record the later release, dependency and runtime checks separately.

## Change inventory

- [Changed paths](upstream-changes.tsv): 1,400 entries across the release (473 additions, 80 deletions, 845 modifications and two renames).
- [Changed-path dispositions](upstream-disposition.tsv): structural roles for those entries. There are 19 runtime-source paths and 12 catalog inputs in the four target packages. The role `outside-four-package-comparison` does not exclude a path from later dependency or release review.
- [Test inventory](test-inventory.tsv): 324 pinned test files and 23 changed test-file paths under `test/` and `test-workers/`. The checked-out bytes of all 23 changed tests match their pinned Git objects. This seeds the v1.7.0 crosswalk without carrying forward v1.6.1 assertion coverage.

```sh
source scripts/project-env.sh
bun run docs/upstream-v1.7.0/inventory.ts "$PI_MIRROR" --check
```

The mirror must contain both official tags at the recorded commits. Use `--write` to reproduce the inventory. The checker verifies the Git inputs and tracked inventory bytes. It does not download npm archives or run tests.

## Native baseline and next comparisons

Before changing the target, four focused native tests passed on `3c59c42f` with Go 1.27.3 and `CGO_ENABLED=0`: prepared temporal boundaries, request-size overflow boundaries, Codemode store checkpoints and independent Durable turn limits. No Go production code, dependency pins, catalog output or format generator changed in this retarget. The old generation gates still verify their recorded inputs.

Stage the locked dependencies and verify release assets before executing the first v1.7.0 originals. Start with Pi Image and Radius routing, OpenAI Completions reasoning and empty tools, Bedrock messages and Durable usage and storage-commit cases. Compare the published model records before changing the Go generator pin. Verify Codemode and Schema distribution contents separately from source-file changes. This retarget establishes the new comparison inputs; runtime parity and release acceptance remain open.

## First unchanged AI originals

The [locked AI test stage](ai-originals.md) ran 59 unchanged originals from five files. The selected release assets, installed lock placements and published catalog payloads have separate checks. Durable, Codemode and Schema test execution and the full dependency closure remain open.

## Pi Image

The [Pi Image comparison](pi-image.md) maps the eight original cases to the public native registry path. The adapter preserves the canonical options and output, provider cost, and response observation before body reading. The [assertion crosswalk](test-crosswalk.tsv) counts verified sites without claiming whole-file or release coverage.

```sh
bun run docs/upstream-v1.7.0/assertions/validate.ts "$PI_ORACLE" "$PI_MIRROR"
bun run docs/upstream-v1.7.0/test-crosswalk.ts --check
```
