# go-ai upstream release parity

This file is the root release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai` / `github.com/earendil-works/pi`.

## Current audited upstream release

- Package: `@earendil-works/pi-ai`
- Release/tag: `v0.99.2`
- Upstream tag/gitHead: `005af57d88ee23b33778f343a9595b32e67ff788`
- Official npm artifact: `/workspace/tmp/pi-ai-0992.tgz`
- Official npm artifact SHA-256: `0b3df8791b488216f309d908789294a744bb61bbaad123d94098e56df9538d25`
- Previous accepted upstream baseline: `v0.99.1` / accepted Go runtime `d3ac443f6f553a9f079501c3954eb2c65d051c24`
- Rollback SHA before the v0.99.2 port cycle: `7a667f39f3642cbb54b4e648d91b7d3f8d697d9d`
- Rejected v0.99.2 candidate: `3edf860c4e8b28f16b2e0499bdc6b3e0d221a869` (hosted artifact omitted the full 40-character root `vcs.revision`; only the 12-character root version appeared in root version/purl).
- Replacement runtime status: this normal replacement commit supersedes `3edf860` and is locally pending hosted CI/artifact acceptance. Hosted run, job, artifact, and digest fields must be added later by an explicitly docs-only post-acceptance receipt if required.
- Publication state: blocked. No `v0.99.2` tags, releases, or aliases are authorized from `3edf860` or from this replacement until auditor acceptance.
- Native/upstream release tags, when later authorized, must target the accepted replacement runtime SHA, not a later docs-only receipt commit.

## Scope evidence

- Changed paths: `15` canonical rows, committed at `docs/v0992/changed-paths.txt`, SHA-256 `53b2c290d902bb8d79c87e035b87c52a13b97617849ea85b51c8e2b11133cc15`.
- Changed tests: `6` rows, committed at `docs/v0992/changed-tests.txt`, SHA-256 `1ad16f63dc47b019cdcf4fdf7029c86785f4cbac38158e7fb63db963ce9ce66d`.
- Whole upstream test corpus: `171` rows, committed at `docs/v0992/test-corpus-171.txt`, SHA-256 `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
- Detailed path matrix: `docs/v0992/changed-paths-crosswalk.md`.
- Changed-test crosswalk: `docs/v0992/changed-tests-crosswalk.md`.
- Source/test delta spans Anthropic workload identity federation, Anthropic strict tool schema keyword fallback, provider retry delay parsing, z.ai CN overflow wording, JS package-entry documentation, and generated chat/classifier catalog metadata.

## Current Go implementation/adaptation summary

Implemented or adapted for v0.99.2:

- Exact generated chat catalog refresh to `1529` models across `41` providers and `10` chat APIs. The existing `openai/gpt-6.1-sol` record remains present with `InputLimits.MaxRequestBytes == 512MiB` and request image-count limits; the new `github-copilot/gpt-6.1-sol` record is asserted separately with Copilot headers, `1050000` context window, and shared tier pricing.
- Exact generated classifier catalog refresh to `15` classifier models across `5` providers; image catalog remains unchanged at `57` image models across `1` provider.
- Anthropic workload identity federation in the Go direct HTTP path: exact `/v1/oauth/token` body, whitespace-trimmed identity token, bearer auth on message requests, API-key/auth-token/header precedence, cache reuse, expiry refresh, coalesced concurrent exchanges, cancellation, reset/isolation, malformed/token-type/transport errors, and endpoint-derived diagnostic redaction for echoed assertions.
- Provider-specific Anthropic strict JSON-schema unsupported-keyword hook: supported normalized strict schemas remain strict; `minimum`/`maximum`, `minItems > 1`, and unsupported `format` fall back for `prefer` and fail for `require`; eager tool-input streaming remains independent.
- z.ai CN context-overflow wording detection for `Prompt exceeds max length`.
- Provider retry delay parsing now ignores non-finite `Retry-After`/`Retry-After-Ms` values and falls back through the production HTTP retry path.
- JS-only `@earendil-works/pi-ai/models` package-entry/module-loader mechanics are classified N/A for Go; Go uses explicit registration APIs and side-effect provider packages.
- SBOM provenance hardening after rejecting `3edf860`: `make sbom` now embeds root component property `vcs.revision=<40-character HEAD>` while keeping the 12-character root version/purl; `make sbom-check` requires and validates the same exact full revision. Normalizer and validator fail closed on missing, truncated, malformed, duplicate, or mismatched exact root revisions.

## Validation evidence

Focused v0.99.2 evidence before the SBOM provenance replacement:

- `go test ./inference/provider/anthropic -run 'TestAnthropic(Federation|StrictToolSchema)' -count=1` — passed.
- `go test ./tests -run 'Test(DoProviderRequestWithRetryFallsBackForNonFiniteRetryAfterHeader|OverflowDetectsZAICNPromptExceedsMaxLengthErrors)' -count=1` — passed.
- `go test ./tests -run 'Test(V0992TypedRegistry|V0992UnifiedTypedLookup)' -count=1` — passed with separate OpenAI/Copilot `gpt-6.1-sol` assertions.
- `PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-0992/tar/package/dist/models.generated.js ./scripts/check-model-regeneration.sh` — passed for chat (`1529/41`), image (`57/1`), and classifier (`15/5`) full-record regeneration.
- `PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-0992/tar/package/dist/models.generated.js python3 scripts/test-check-model-regeneration.py` — passed; deliberate text/image/classifier corruption gates fail as expected.
- `make staticcheck` — passed.
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1` — passed.
- `make check` and `make test-repro` — passed on `3edf860` before hosted SBOM rejection.

SBOM replacement local evidence for this normal replacement commit must include, before push:

- `python3 scripts/test-normalize-sbom.py` — proves normalizer inserts and replaces a single root `vcs.revision` property and rejects truncated/malformed values.
- `python3 scripts/test-validate-sbom.py` — proves validator rejects missing root property, truncated 12-character property, malformed/non-hex property, duplicate property, mismatched full SHA, and malformed expected revision.
- `make sbom sbom-check sbom-self-test vuln-check license-check` — must pass from the replacement tree.
- After committing, `make sbom sbom-check` must be rerun so `artifacts/sbom.cdx.json` embeds this replacement commit's exact 40-character SHA as root `vcs.revision`; the SBOM root version remains the 12-character commit prefix.

Hosted CI/artifact evidence for this replacement runtime is pending auditor acceptance. Do not publish tags/releases/aliases until accepted.

## Durable SBOM release assets

No v0.99.2 durable SBOM release asset is accepted yet. The hosted SBOM artifact for `3edf860c4e8b28f16b2e0499bdc6b3e0d221a869` was rejected because the full candidate SHA occurred zero times and the root `vcs.revision` property was absent. Future durable SBOM assets must include the accepted replacement runtime as root `vcs.revision=<40-character SHA>` while retaining the expected 12-character root version/purl. Historical durable release assets for earlier accepted releases remain unchanged.

## Release documentation policy

For every future upstream `@earendil-works/pi-ai` release audit, update this `RELEASE.md` in the same release parity commit before declaring completion. The update must include the upstream tag/SHA, npm artifact/checksum, changed-path and test-corpus counts/hashes, every Go implementation, fix, adaptation, N/A decision, local validation/fault-gate evidence, SBOM/security/license evidence, hosted CI evidence, and final/rollback SHAs.
