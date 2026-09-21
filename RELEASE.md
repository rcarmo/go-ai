# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.87.0`
- Upstream tag/SHA: `16787ad5b2dc748047f314ca1bfe7708f30f54f3`
- Previous accepted upstream baseline: `v0.85.1` / `d981de1229ef899957bbe968bc8dcda02a21f477`
- Previous accepted Go runtime baseline before this audit: `9c32e1d77bb01bac4574c6ecf260ce07bac9a351`
- Current repository baseline before this audit: `5baec87b66c7ceb667e1cdbc2b1edbdc09e98c37`
- Official npm artifact: `/workspace/tmp/pi-ai-audit-087/earendil-works-pi-ai-0.87.0.tgz`
- Official npm artifact SHA-256: `f2adf9de809d035f76f8dadf3d148720ebeef4606a848ab36ee834d895ae812f`
- Detailed path matrix: `docs/v0870-release-ledger.md`
- Whole-corpus upstream test crosswalk: `docs/v0870-150-test-manifest.md`

## Scope evidence

- Changed paths: `127` canonical rows, committed at `docs/v0870/changed-paths.txt`, SHA-256 `e6bd9733d8fff626838d386df8e6bb543d40d77af74340ee4f2f271b411b9828`.
- Changed tests: `82` rows, committed at `docs/v0870/changed-tests.txt`, SHA-256 `a12a1453c8fbabfd6902ced06304cbd2fa9f7d82ce89b91a1403366bc11fe5b2`.
- Whole upstream test corpus: `150` rows, committed at `docs/v0870/test-corpus-150.txt`; basename SHA-256 `042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75`.
- Source/test delta spans cumulative `0.86.0`, `0.86.1`, and `0.87.0` behavior.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.87.0 so far:

- Exact generated text catalog refresh to `1445` models across `41` providers and `10` APIs.
- Exact generated image catalog refresh to `54` image models, adding OpenAI GPT Image 2.5 Flare/Sunburst.
- Generator/model type support for `inputLimits`, `promptCache`, `enabled`, `lab`, Radius provider-routing metadata, `BedrockCompat`, and mid-conversation compat flags.
- Meta provider/API-key environment support and deterministic Meta OAuth device/mint/remint tests.
- Radius baseline catalog models with dynamic refresh preservation.
- OpenCode `x-opencode-session` header helper and transport wiring for relevant providers.
- Transcript normalization helpers for system messages, sections, and tool additions/removals.
- Catalog comparator fixed for v0.87 nested Radius provider-routing entries.

Historical v0.85.1 runtime/SBOM/README release evidence remains in this file and git history; v0.87.0 README/SBOM release publication remains blocked until final runtime acceptance.

## Validation evidence

Current local evidence captured so far during the v0.87.0 audit:

- Oracle hash verification for tarball, changed paths, changed tests, and test corpus — passed.
- `scripts/validate-v0870-catalog-delta.py` and `--self-test` — passed (`text +149/-58/986`, `images +2/-0/4`).
- Model/image regeneration comparator — passed.
- Upstream/generated model pair comparator — `1445/1445`, exact match.
- Focused Meta OAuth tests — passed.
- `TMPDIR=/workspace/tmp go test ./...` — passed.
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...` — passed.
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- `TMPDIR=/workspace/tmp go vet ./...` — passed.
- `make staticcheck` — passed.
- `make check-logging` — passed.
- `make check` — passed, including deterministic tests, v0.85/v0.87 inventories, catalog deltas, regeneration, SBOM, vuln, and license gates.
- `make test-repro` — passed.
- `make fuzz` — passed.
- Hidden-skip scan over Go tests found no `t.Skip`/`SkipNow`/`.skip` matches; v0.87 docs/scripts/tests contain no TODO/classification placeholders.
- Clean-checkout overlay validation in `/workspace/tmp/go-ai-v0870-clean-validate` — passed `git diff --check`, `TMPDIR=/workspace/tmp go test ./...`, `make check-v0870-inventory check-v0870-catalog-delta check-model-regeneration`, and `scripts/compare-upstream-models.py` at `1445/1445`.

Accepted runtime `c51fb076ad9f0207ba128af750d94fc40de9a121` passed hosted CI `35655297308` (main job `106517221587`, fuzz job `106517221934`). CI artifact `10665105040` produced SHA-specific SBOM `b2ad97fc11b6fa843da191f611359e989c526a8110e51e3f0f1f5d3d0d5bee83` for root revision `c51fb076ad9f`; artifact ZIP SHA-256 `1c60ad7b0a882d9a3c1f1df868ba9ba5c3d9a3e9c3cef0f5cd4b5aad0f625de6` and checksum-file SHA-256 `dcb602f454d182ff48ba43964c4a6786b892443d43a7b659c94882d5fafdb9c8`. Public release publication for `upstream-v0.87.0` is authorized and will be verified separately.

## Durable SBOM release assets

Accepted runtime `c51fb076ad9f0207ba128af750d94fc40de9a121` is the current v0.87.0 release baseline. The guarded manual publisher workflow publishes `sbom.cdx.json` and `sbom.cdx.json.sha256` for tag `upstream-v0.87.0` against that exact runtime ref. Historical v0.85.1 durable SBOM links for `9c32e1d77bb01bac4574c6ecf260ce07bac9a351` remain under `upstream-v0.85.1`; v0.85.0 release assets remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation/fix/adaptation/N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
