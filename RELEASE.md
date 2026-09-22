# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.87.1`
- Upstream tag/SHA: `f07218c4d4bbc12bef056a7058c3dd49dfe41abe`
- Previous accepted upstream baseline: `v0.87.0` / `16787ad5b2dc748047f314ca1bfe7708f30f54f3`
- Previous accepted Go runtime baseline before this audit: `c51fb076ad9f0207ba128af750d94fc40de9a121`
- Current repository baseline before this audit: `bd630670abbbcbbf12c9389cd616e37f7ba1560e`
- Official npm artifact: `/workspace/tmp/pi-ai-audit-0871/earendil-works-pi-ai-0.87.1.tgz`
- Official npm artifact SHA-256: `35b4432f27cc2665f86beebb9af6a39b1251970883c3044bd8be4f4e8c731ca0`
- Detailed path matrix: `docs/v0871-release-ledger.md`
- Whole-corpus upstream test crosswalk: `docs/v0871-150-test-manifest.md`

## Scope evidence

- Changed paths: `16` canonical rows, committed at `docs/v0871/changed-paths.txt`, SHA-256 `2756fce613d0163b6eb5c47b599584589a229e5c7ed30a86b65380031e272eb6`.
- Changed tests: `9` rows, committed at `docs/v0871/changed-tests.txt`, SHA-256 `5b66a8cf9050b36a8dbae7a1b802c12037a2ec2332cf2e3f370953ea9ef9ac43`.
- Whole upstream test corpus: `150` basename-only rows, committed at `docs/v0871/test-corpus-150.txt`; SHA-256 `042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75`.
- Source/test delta spans focused `0.87.1` behavior: OpenAI-compatible image-only message handling, Claude Code OAuth user-agent version, and generated catalog updates.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.87.1:

- Exact generated text catalog refresh to `1495` models across `41` providers and `10` APIs.
- Exact generated image catalog refresh to `55` image models, adding InclusionAI Ming Image 0.1 Design.
- OpenAI-compatible image-only user messages now omit exactly empty text parts while preserving whitespace text.
- Anthropic OAuth latent branch exposed with upstream auth precedence and `claude-cli/2.1.280` user-agent/x-app behavior.
- Generated metadata covers Grok 4.7 Responses/xhigh/pricing, Claude Opus 5.5 effort/context/pricing, GPT-6 Sol/Luna, and Copilot aliases.
- v0.87.1 manifests, whole-corpus crosswalk, catalog delta validator, generation/fault evidence, focused runtime tests, full gates, and hosted CI evidence recorded.

Historical v0.85.1 and v0.87.0 runtime/SBOM/README release evidence remains in this file and git history.

## Validation evidence

Current v0.87.1 acceptance evidence:

- `docs/v0871/changed-paths.txt`, `changed-tests.txt`, and basename-only `test-corpus-150.txt` match pinned hashes.
- `scripts/validate-v0871-inventory.py` and `--self-test` — passed; corruption self-tests cover all three manifests.
- `scripts/validate-v0871-catalog-delta.py` and `--self-test` — passed (`text +62/-12/35`, `images +1/-0/0`, current counts `1495/41/10` and `55/1/1`).
- Generation twice comparison — passed; normalized generated text source SHA-256 `13badf32117c7faf05657c280300d1ad83df77c9b256b000da8f68f808033ce8`, generated image source SHA-256 `4b3c94c02dbc82a7a0fe343fed241b91dde151963a87ddb8a517c5d0c33a7c4c`.
- Focused OpenAI image-only and Anthropic OAuth user-agent/precedence tests — passed.
- Full local gates passed under `nice -n 10`: `go test ./...`, `make check`, race, fuzz, SBOM, vuln, and license.
- Accepted runtime `c2d0231d8bef63a920e1663143e6c1c39ef0679d` passed hosted CI `35794325936` with SHA-specific CycloneDX 1.6 SBOM artifact `10723572304`; SBOM SHA-256 `3a58a615ca42dd521688030407faae013ab23cb4984f290bf0925a2ddfbf2b5f`, root revision/version `c2d0231d8bef`.

## Durable SBOM release assets

Accepted runtime `c2d0231d8bef63a920e1663143e6c1c39ef0679d` is the current v0.87.1 release baseline. The guarded manual publisher workflow publishes `sbom.cdx.json` and `sbom.cdx.json.sha256` for tag `upstream-v0.87.1` against that exact runtime ref. Historical v0.87.0 durable SBOM links for `c51fb076ad9f0207ba128af750d94fc40de9a121` remain under `upstream-v0.87.0`; historical v0.85.1/v0.85.0 release assets remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation/fix/adaptation/N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
