# v0.87.0 upstream release ledger

## Release bounds

- Upstream package: `@earendil-works/pi-ai` `v0.87.0`
- Upstream tag/gitHead: `16787ad5b2dc748047f314ca1bfe7708f30f54f3`
- Previous accepted runtime baseline: `9c32e1d77bb01bac4574c6ecf260ce07bac9a351` (`v0.85.1`)
- Official npm tarball: `/workspace/tmp/pi-ai-audit-087/earendil-works-pi-ai-0.87.0.tgz`
- npm tarball SHA-256: `f2adf9de809d035f76f8dadf3d148720ebeef4606a848ab36ee834d895ae812f`

## Oracle manifests

- Changed package paths: `docs/v0870/changed-paths.txt`, 127 rows, SHA-256 `e6bd9733d8fff626838d386df8e6bb543d40d77af74340ee4f2f271b411b9828`.
- Changed tests: `docs/v0870/changed-tests.txt`, 82 rows, SHA-256 `a12a1453c8fbabfd6902ced06304cbd2fa9f7d82ce89b91a1403366bc11fe5b2`.
- Whole upstream test corpus: `docs/v0870/test-corpus-150.txt`, 150 rows, basename hash `042cdfbc8cc089da71409e615fb54cfe7273a960f8e0d10e63e07584ae9f2e75`.

## Generated catalog evidence

- Text old/new JSONL:
  - `docs/v0870/text-v0851.jsonl` — 1354 rows, SHA-256 `60dc44084fc111724264e85e4f55adb9b30d4a073061e7c50816abf0b7351eeb`.
  - `docs/v0870/text-v0870.jsonl` — 1445 rows, SHA-256 `baf3a836d7e8bc386dd65ec2a9f61e458e5009d19cd884a12daa4f637c3546ef`.
  - Full-record delta, excluding the deliberately new `inputLimits` metadata dimension from the changed-record count: `+149/-58/986`; current count/providers/APIs: `1445/41/10`.
- Image old/new JSONL:
  - `docs/v0870/images-v0851.jsonl` — 52 rows, SHA-256 `ee84a20ae8d536f90a223fd99fba0111a2b8bcc0a742682ff8363d987ef71011`.
  - `docs/v0870/images-v0870.jsonl` — 54 rows, SHA-256 `1a203c24d75d77c016a7497109dd6f2c8d390b29a7a3a08de60589b0c3c42f03`.
  - Full-record delta: `+2/-0/4`; current count/providers/APIs: `54/1/1`.

## Go implementation/adaptation matrix

| Upstream surface | Go disposition |
| --- | --- |
| Generated text/image catalogs | Regenerated from exact v0.87.0 tag plus published provider data. Added `meta` provider, Radius baseline models, v0.87 text/image additions/removals, image input limit metadata, prompt-cache metadata, provider routing metadata, Bedrock compat, and mid-conversation compat flags. |
| Catalog comparator | `scripts/compare-upstream-models.py` now anchors top-level generated `Model` literal fields so nested Radius `providers` routing entries cannot be misread as generated model IDs. |
| Meta provider/OAuth | Added `ProviderMeta`, `META_API_KEY` lookup, and `oauth.MetaProvider` device authorization + identity-token-to-Muse-key minting with deterministic tests for login, refresh/remint, setup URL, and API-key derivation. |
| Radius dynamic/static model behavior | Generated Radius baseline models are now present in the built-in catalog with lab/provider routing/enabled metadata. Existing dynamic Radius refresh remains in place for gateway updates. |
| OpenCode session headers | Added `WithOpenCodeSessionHeader`/`SessionIDFromOptions`; OpenAI completions, Responses, Anthropic, and Google transports add `x-opencode-session` for `opencode`/`opencode-go` unless caller/model headers already set it. |
| Transcript reconstruction | Added transcript types/helpers for system messages, tool additions/removals, section patching, and legacy-provider folding; current OpenAI completions, Responses, and Anthropic request builders resolve transcript contexts before payload conversion, including native system-message/tool-addition payload items where supported. |
| Mid-conversation compat metadata | Generated `supportsMidConvoSystemMessages`, `supportsMidConvoToolAdditions`, and `supportsMidConvoToolChanges` through Go compat structs and tests. |
| Bedrock strict-mode metadata | Added `BedrockCompat` and moved Bedrock strict schema support to Bedrock-specific generated metadata. |
| Image resize/request limits | Added `ModelInputLimits`/image resize metadata with tests for OpenAI, Anthropic, Bedrock, Meta, Radius, and OpenRouter-style vision models. Actual image byte resizing remains a caller/preprocessor concern in this Go runtime. |
| README/SBOM release links | Intentionally unchanged until final v0.87 runtime acceptance and release publication. |

## Validation evidence so far

- Oracle hashes verified from `/workspace/tmp/pi-ai-audit-087`.
- `scripts/validate-v0870-catalog-delta.py` — passed.
- `scripts/validate-v0870-catalog-delta.py --self-test` — passed.
- `TMPDIR=/workspace/tmp ./scripts/check-model-regeneration.sh` — passed for text and image generated sources.
- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-audit-087/package-0.87.0/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-ai-source-087/pi/packages/ai/src/providers` — `1445/1445`, exact match.
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

Hosted CI, final SHA, and SHA-specific hosted SBOM evidence are still pending.
