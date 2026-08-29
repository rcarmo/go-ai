# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.84.4`
- Upstream tag/SHA: `b79e4cc834970cca69daebffab7df1da7d1e52c4`
- Published: 2026-08-28T22:05:13.974Z
- Previous accepted baseline: `v0.84.3` / `4e58f324fae8ebfa98a3d45181fb248072a2afac`
- Accepted local baseline before this audit: `e25cd2e0b454767c35ac53831d2c1a5bb4641299`
- Accepted local candidate SHA for this audit: `3e996243d34e5e4568a58cf4f85ca098c5d098f8`
- Exact upstream checkout used: `/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4`
- Official npm data artifact used for generated provider JSON shards: `/workspace/tmp/pi-ai-0844-npm/package`
- Official npm tarball SHA-256: `dfd3c929cee5a7387199a0a24dfc1be2096f1ea8f59ffb8285198a0ed01ebf93`
- Detailed path matrix: `docs/v0844-release-ledger.md`
- Whole-corpus test crosswalk: `docs/v0844-137-test-manifest.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from official `v0.84.3` to official `v0.84.4`, no unpublished `main` changes.

Audited scope:

- 15 changed `packages/ai` paths: 5 source files, 2 scripts (1 modified + 1 added), 6 tests (5 modified + 1 added), 2 package/docs.
- Added test: `openrouter-reasoning-options.test.ts`.
- Whole test corpus: 137 `packages/ai/test/*.test.ts` files, fully classified with 0 unclassified rows.
- Text model catalog: 1290 models across 39 providers and 9 APIs, exact provider/id pair parity with upstream.
- Full-record text catalog delta versus v0.84.3: `1312→1290`, with `+57` added records, `-79` removed records, and `227` changed records.
- Image model catalog: 50 OpenRouter image models, +5 vs v0.84.3 (`meta/muse-image` and Recraft v4 variants).

## v0.84.4 Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| Text catalog refresh | Implemented mechanically. `models_generated.go` regenerated from exact v0.84.4 source and official npm provider shards: 1290 models / 39 providers / 9 APIs. Full-record delta recorded as `1312→1290`, `+57/-79/227 changed`. |
| Image catalog refresh | Implemented mechanically. `images/models_generated.go` regenerated from exact v0.84.4 source: 50 OpenRouter image models, including `meta/muse-image` and Recraft v4 variants. |
| Generator OpenRouter reasoning metadata | Implemented/adapted into generated Go `ThinkingLevelMap` and `OpenAICompletionsCompat` fields. OpenRouter `supported_efforts` maps to mandatory/optional/off behavior, including `off:null` for mandatory reasoning and `off:"none"` for optional disable semantics where applicable. |
| Cloudflare AI Gateway Workers AI mirror | Implemented mechanically in generated catalog: tool-capable Workers AI models are mirrored under Cloudflare AI Gateway with `workers-ai/` prefix, `/compat` endpoint, session-affinity compat, and deduped IDs. |
| OpenAI-compatible explicit `toolChoice:"none"` | Implemented in Chat Completions payload generation. Explicit `ToolChoiceNone` serializes as `tool_choice:"none"` even when no tools are present, while omitting `tools`. |
| OpenAI-compatible streamed reasoning details | Implemented v0.84.4 semantics: streamed `reasoning.text`, `reasoning.summary`, and encrypted details are replay metadata, adjacent text/summary details merge while preserving common metadata/order, and the final array is serialized once on a thinking block `thinkingSignature` with no duplicate tool-call `thoughtSignature`. Legacy stored encrypted tool-call signatures still replay. |
| Mistral fragmented tool-call chunks | Implemented indexed tool-call accumulation so later chunks that omit ID and provide an empty function name merge into the original call. |
| ZAI GLM-5.3 metadata | Implemented mechanically via regenerated catalog and tested for v0.84.4 metadata/compat. |
| Fireworks catalog changes | Implemented mechanically via regenerated catalog and tested for Kimi K2.7/K3 additions plus retired K2.6 router removal while preserving Fireworks env-key compatibility. |
| DeepSeek V4 vision metadata | Implemented mechanically via regenerated catalog and tested for `deepseek-v4-flash-vision-exp` multimodal metadata. |
| Tests and docs | Added/updated deterministic tests, exact 15-path ledger, exact 137-file corpus crosswalk, and this release record. |
| JS-only/runtime-specific surfaces | Narrowly N/A/adapted where no Go equivalent exists (private TS generator helper structure, JS type-only surfaces); observable generated catalog and provider wire behavior are covered in Go. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data \
  python3 scripts/compare-upstream-models.py /workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/providers
upstream pairs: 1290
generated pairs: 1290
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/models.generated.ts \
PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/go-v0844-regen-cache/pi-b79e4cc834970cca69daebffab7df1da7d1e52c4/packages/ai/src/image-models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0844-npm/package/dist/providers/data \
TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
model regeneration metadata comparator passed
image model regeneration comparator passed
```

## Supply-chain and security evidence

Current workflow policy adds pinned Go supply-chain targets:

- SBOM generator: `github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.12.0`
- SBOM format/artifacts: CycloneDX JSON `artifacts/sbom.cdx.json` plus `artifacts/sbom.cdx.json.sha256` (gitignored, CI-uploaded artifact)
- SBOM digest: produced by `make sbom` in `artifacts/sbom.cdx.json.sha256` for the exact checked-out candidate SHA; do not copy stale pre-commit digests across commits.
- SBOM validation: `scripts/validate-sbom.py` checks JSON/schema identity, normalized checksum, root module, expected Git revision, root dependency ref, non-empty dependencies, required Go deps, and absence of local absolute paths.
- SBOM validator self-tests: `scripts/test-validate-sbom.py` proves the validator rejects revision tampering even with a recomputed checksum, checksum mismatch, malformed JSON, local absolute path leakage, empty dependency graphs, and missing root dependency refs.
- Vulnerability policy self-tests: `scripts/test-check-vuln-policy.py` proves the policy rejects different-Go-version findings, undocumented findings, and unused active exceptions.
- CI execution path: `make check` performs SBOM generation/validation, SBOM self-tests, vulnerability scan/policy self-tests, and license review exactly once; the workflow then uploads the already-generated SBOM artifacts without rerunning security work.
- Vulnerability scanner: `golang.org/x/vuln/cmd/govulncheck@v1.7.0`
- Vulnerability disposition: `make vuln-check` passes only with exact Go-version-scoped, expiring `security-vuln-policy.json` entries. Each exception must include owner, rationale, mitigation, expiry, Go version, and scope. Local Go `go1.26.3` reports documented standard-library toolchain advisories, owned by Rui Carmo, expiring `2026-09-30`, with mitigation to upgrade to Go `1.26.6` or later and remove the exceptions. Hosted CI uses Go `go1.24.13` and reports no reachable vulnerabilities, so no CI exceptions are active or consumed. Dependency findings remain blocking unless separately documented.
- License scanner: `github.com/google/go-licenses@v1.6.0`; `make license-check` passes with the pinned allowlist in `Makefile`. Future incompatible or unknown license exceptions require owner, rationale, mitigation, and expiry; the allowlist alone is not an exception record.

## Validation evidence

Candidate gate commands:

```text
python3 scripts/validate-test-manifest.py docs/v0844-137-test-manifest.md /workspace/tmp/v0844-test-files.txt
# manifest rows: 137; unique paths: 137; expected paths: 137; changed-row markers: 6; manifest validation passed

TMPDIR=/workspace/tmp go test ./...
make check GO_TMPDIR=/workspace/tmp
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go vet ./...
make staticcheck GO_TMPDIR=/workspace/tmp
make check-logging
make test-repro GO_TMPDIR=/workspace/tmp
make ci-artifacts GO_TMPDIR=/workspace/tmp
# all PASS locally with Go go1.26.3; documented toolchain-scoped stdlib findings only. Hosted CI uses Go go1.24.13 and reports `govulncheck: no reachable vulnerabilities`; CI uploads SBOM artifacts generated by `make check` without a duplicate security scan.

Hosted CI for candidate `3e996243d34e5e4568a58cf4f85ca098c5d098f8` passed:
- Run: https://github.com/rcarmo/go-ai/actions/runs/33251362203 (`CI`, conclusion `success`)
- Job `Vet, Staticcheck & Deterministic Tests (1.24.x)`: success; build, check (vet/staticcheck/deterministic tests), race tests, coverage, and logging quality gate all succeeded.
- Job `Fuzz Tests`: success; partial JSON parser, SSE parser, context round-trip, message transformation, and overflow detection fuzz jobs all succeeded.
```
