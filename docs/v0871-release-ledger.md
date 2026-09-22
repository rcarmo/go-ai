# v0.87.1 upstream release ledger

## Release bounds

- Upstream package: `@earendil-works/pi-ai` `v0.87.1`
- Upstream/npm gitHead: `f07218c4d4bbc12bef056a7058c3dd49dfe41abe`
- Previous accepted runtime baseline: `c51fb076ad9f0207ba128af750d94fc40de9a121` (`v0.87.0`)
- Official npm tarball: `/workspace/tmp/pi-ai-audit-0871/earendil-works-pi-ai-0.87.1.tgz`
- npm tarball SHA-256: `35b4432f27cc2665f86beebb9af6a39b1251970883c3044bd8be4f4e8c731ca0`

## Oracle manifests

- Changed package paths: `docs/v0871/changed-paths.txt`, 16 rows, SHA-256 `2756fce613d0163b6eb5c47b599584589a229e5c7ed30a86b65380031e272eb6`.
- Changed tests: `docs/v0871/changed-tests.txt`, 9 rows, SHA-256 `5b66a8cf9050b36a8dbae7a1b802c12037a2ec2332cf2e3f370953ea9ef9ac43`.
- Whole upstream test corpus: `docs/v0871/test-corpus-150.txt`, 150 rows, basename SHA-256 `042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75`.
- Source delta: 16 files, `+386/-78`.

## Generated catalog evidence

- Text old/new JSONL:
  - `docs/v0871/text-v0870.jsonl` — 1445 rows, SHA-256 `baf3a836d7e8bc386dd65ec2a9f61e458e5009d19cd884a12daa4f637c3546ef`.
  - `docs/v0871/text-v0871.jsonl` — 1495 rows, SHA-256 `13843511b69557a8d92ec276f8bc19f89579f79577ad9d26e69d165ef2270777`.
  - Full-record delta: `+62/-12/35`; current count/providers/APIs: `1495/41/10`.
- Image old/new JSONL:
  - `docs/v0871/images-v0870.jsonl` — 54 rows, SHA-256 `1a203c24d75d77c016a7497109dd6f2c8d390b29a7a3a08de60589b0c3c42f03`.
  - `docs/v0871/images-v0871.jsonl` — 55 rows, SHA-256 `17ed6676553d78d561f9e67848e77da79e8ecd62aacbd89e0d0b66abb64b2bf1`.
  - Full-record delta: `+1/-0/0`; current count/providers/APIs: `55/1/1`.

## Go implementation/adaptation matrix

| Upstream surface | Go disposition |
| --- | --- |
| Generated text/image catalogs | Regenerated from exact v0.87.1 tag plus published provider data. Catalog target is 1495 text models across 41 providers/10 APIs and 55 image models. |
| OpenAI-compatible image-only messages | Production converter now omits exactly empty string text parts in multimodal user messages, preserving whitespace-only text; focused provider tests cover both paths. |
| Claude OAuth user-agent bump | Existing upstream OAuth branch was latent baseline debt in Go. Go now exposes that branch with upstream `strings.Contains(apiKey, "sk-ant-oat")` detection and exact auth precedence; v0.87.1 delta is only the Claude Code version bump to `claude-cli/2.1.280`. Regression tests cover ordinary API key, explicit auth override, and OAuth headers. |
| Grok 4.7 | Generated catalog routes `xai/grok-4.7` through OpenAI Responses with xhigh thinking, encrypted reasoning include behavior inherited from xAI Responses provider, and long-context pricing tiers. |
| Claude Opus 5.5 | Generated catalog carries direct/Copilot/Bedrock/OpenRouter/other provider records, effort/context/pricing metadata, and thinking levels. |
| GPT-6 Sol/Luna and Copilot aliases | Generated catalog adds direct OpenAI/Azure/Codex/Copilot and wrapper records with pricing, xhigh/max thinking support, and existing Responses compat behavior. |

## Validation evidence

All CPU-heavy validation commands were run under `nice -n 10`.

- Official tarball SHA-256 verified: `35b4432f27cc2665f86beebb9af6a39b1251970883c3044bd8be4f4e8c731ca0`.
- Exact manifest hashes verified:
  - `docs/v0871/changed-paths.txt` — 16 rows, SHA-256 `2756fce613d0163b6eb5c47b599584589a229e5c7ed30a86b65380031e272eb6`.
  - `docs/v0871/changed-tests.txt` — 9 rows, SHA-256 `5b66a8cf9050b36a8dbae7a1b802c12037a2ec2332cf2e3f370953ea9ef9ac43`.
  - `docs/v0871/test-corpus-150.txt` — 150 basename-only rows, SHA-256 `042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75`.
- `scripts/validate-v0871-inventory.py` and `--self-test` — passed; corruption self-tests cover all three manifest files.
- `scripts/validate-test-manifest.py docs/v0871-150-test-manifest.md docs/v0871/test-corpus-150.txt` — passed; 150 rows, 9 changed-row markers.
- `scripts/validate-v0871-catalog-delta.py` and `--self-test` — passed; text `+62/-12/35`, images `+1/-0/0`, current counts `1495/41/10` and `55/1/1`.
- Focused OpenAI image-only regression tests — passed.
- Focused Anthropic OAuth user-agent/precedence tests — passed.
- Upstream/generated model pair comparator — `1495/1495`, exact match.
- Generation twice comparison — passed. Text generated source matched byte-for-byte after normalizing the timestamp line; image generated source matched byte-for-byte. Hashes:
  - normalized generated text source: `13badf32117c7faf05657c280300d1ad83df77c9b256b000da8f68f808033ce8`.
  - generated image source: `4b3c94c02dbc82a7a0fe343fed241b91dde151963a87ddb8a517c5d0c33a7c4c`.
  - committed `models_generated.go` full file: `51a4e08389353321abca4f8ed2ab8e4784a8109703b6dec658b63b5b4d224260`.
  - committed `images/models_generated.go`: `4b3c94c02dbc82a7a0fe343fed241b91dde151963a87ddb8a517c5d0c33a7c4c`.
- Exact metadata/provider focused test selection — passed across `./tests`, `./inference/provider/openai`, `./inference/provider/anthropic`, and `./inference/provider/openairesponses`.
- `go test ./... -count=1` — passed.
- `make check` — passed, including deterministic tests, vet, staticcheck, logging, v0.85/v0.87/v0.87.1 inventories and deltas, generation/fault gates, SBOM, vuln, and license gates.
- `CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- `make fuzz` — passed.

Hosted CI, final SHA, and SHA-specific SBOM evidence are pending.
