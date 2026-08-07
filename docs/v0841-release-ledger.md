# v0.84.1 release ledger

Audit target: official `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.84.1`, SHA `53fa77ccd8a279eb87e92294ef3687b03ff80112`, published 2026-08-07T06:07:00Z.

Previous accepted baseline: `v0.84.0`, SHA `a5f43bf8aff3c55752432655f7334e3dafd1e256`.

Exact checkouts/artifacts:

- Source tag checkout: `/workspace/tmp/pi-v0841`
- Previous tag checkout: `/workspace/tmp/pi-v0840`
- Official npm package artifact: `/workspace/tmp/pi-ai-0.84.1-package/package`
- Previous official npm package artifact: `/workspace/tmp/pi-ai-0.84.0-package/package`

## Changed-path matrix (25 `packages/ai` paths)

Exact command: `git diff --name-status a5f43bf8aff3c55752432655f7334e3dafd1e256..53fa77ccd8a279eb87e92294ef3687b03ff80112 -- packages/ai`.

| Upstream path | Disposition | Go evidence / rationale |
| --- | --- | --- |
| `packages/ai/CHANGELOG.md`, `packages/ai/README.md`, `packages/ai/package.json` | N/A/docs/package metadata | Recorded in `RELEASE.md`; no Go runtime behavior beyond provider/catalog changes. |
| `packages/ai/scripts/generate-models.ts` | Adopted via exact artifact consumption; strict rollback policy N/A/adapted | Go generator consumes exact source/package model artifacts. `scripts/compare-upstream-models.py` proves 1220/1220 provider/id parity; `scripts/check-model-regeneration.sh` proves normalized full `models_generated.go` regeneration equality for all Go-representable metadata. Upstream `--strict` rollback helper is private TS generator policy and is documented N/A/adapted. |
| `packages/ai/scripts/model-data.ts` | Adopted via exact artifact validation/comparison | Final generated Go catalog is compared against exact v0.84.1 provider data by both provider/id pair comparator and normalized regeneration diff; private TS shard validation helper failure modes remain N/A/adapted-generator-policy. |
| `packages/ai/src/env-api-keys.ts` | Adopted | Added `ProviderQwenTokenPlanIndividual` and `QWEN_TOKEN_PLAN_API_KEY` reuse in `types.go`/`env.go`; tested in `qwen_token_plan_upstream_test.go`. |
| `packages/ai/src/models.generated.ts` | Adopted mechanically | Regenerated `models_generated.go`: 1220 models across 39 providers; pair comparator exact 1220/1220 and normalized regeneration diff proves full Go-representable metadata equality. |
| `packages/ai/src/providers/all.ts` | Adopted idiomatically | Go uses package-level model/provider registry; new provider ID is available through generated catalog and `ListModels`. |
| `packages/ai/src/providers/qwen-token-plan-individual.models.ts` | Adopted mechanically | New provider appears in generated Go catalog with exact seven-model allowlist. |
| `packages/ai/src/providers/qwen-token-plan-individual.ts` | Adopted | New `qwen-token-plan-individual` provider constant/env/base URL/API behavior added through catalog/env/type registration. Endpoint: `https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1`; API: OpenAI Completions; env: `QWEN_TOKEN_PLAN_API_KEY`. |
| `packages/ai/src/types.ts` | Adopted | `ProviderQwenTokenPlanIndividual` added. |
| `packages/ai/test/qwen-token-plan-models.test.ts` | Deterministic port | `qwen_token_plan_upstream_test.go` verifies exact Individual allowlist, env-key reuse, endpoint/API metadata, image omission, retired preview omission; `qwen_token_plan_individual_upstream_test.go` verifies Qwen `enable_thinking` and `reasoning_effort`. |
| `packages/ai/test/generate-models-strict.test.ts` | N/A/adapted-generator-policy | Private TS generator rollback/failure policy; Go retains exact release artifact comparison and documents N/A/adapted rather than claiming helper parity. |
| `packages/ai/test/model-data-validation.test.ts` | N/A/adapted-generator-policy | Upstream helper assertions for exact allowlist/shard validation are private TS generator policy; Go verifies final artifacts by exact 1220/1220 comparator plus tests. |
| Modified live matrix tests: `abort.test.ts`, `context-overflow.test.ts`, `cross-provider-handoff.test.ts`, `empty.test.ts`, `image-tool-result.test.ts`, `stream.test.ts`, `tokens.test.ts`, `tool-call-without-result.test.ts`, `total-tokens.test.ts`, `unicode-surrogate.test.ts` | N/A/live-provider additions plus existing deterministic coverage | v0.84.1 adds Qwen Token Plan Individual live/credential cases. Deterministic portable behavior remains covered by existing simulated/provider tests and new Qwen request-shape tests; credential-gated cases are labeled N/A/live in `docs/v0841-128-test-manifest.md`. |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | Deterministic covered | Existing OpenAI-compatible tool/replay tests plus Qwen generated metadata/request tests cover Go-facing behavior. |

## Material delta disposition

| Upstream area | Disposition | Evidence |
| --- | --- | --- |
| `qwen-token-plan-individual` production provider | Implemented | `types.go`, `env.go`, `models_generated.go`, `qwen_token_plan_upstream_test.go`, `qwen_token_plan_individual_upstream_test.go` |
| OpenAI-compatible request behavior | Implemented/adapted | Existing OpenAI Completions provider path; patched Qwen `thinkingFormat:"qwen"` to emit `reasoning_effort` when generated compat supports it. |
| Exact seven-model allowlist | Implemented | `deepseek-v4-flash-0731`, `deepseek-v4-pro`, `glm-5.2`, `qwen3.6-flash`, `qwen3.7-max`, `qwen3.7-plus`, `qwen3.8-max`. |
| Environment key reuse | Implemented | `ProviderQwenTokenPlanIndividual` resolves `QWEN_TOKEN_PLAN_API_KEY`. |
| Images | Unchanged | `scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841.go` writes 42 image models and diffs clean against `images/models_generated.go`. |
| Text catalog | Adopted mechanically | 0.84.0 → 0.84.1 artifact comparison: 1153/38 → 1220/39, 70 added, 3 removed, 9 metadata changed. Generated Go pair comparator exact 1220/1220; normalized regeneration diff proves full Go-representable metadata equality across all 1220 entries. |
| Whole-corpus test crosswalk | Updated | `docs/v0841-128-test-manifest.md`: 128 exact rows, 101 deterministic/covered, 27 N/A/adapted, 0 TODO. |

## Comparator evidence

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0841/packages/ai/src/providers
upstream pairs: 1220
generated pairs: 1220
model provider/id pairs match exactly

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0841/packages/ai/src/models.generated.ts \
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data \
./scripts/check-model-regeneration.sh
model regeneration metadata comparator passed

python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841.go
wrote /workspace/tmp/images_v0841.go with 42 image models
# diff against images/models_generated.go: exact
```

Artifact diff summary:

```text
0.84.0 text models/providers: 1153 / 38
0.84.1 text models/providers: 1220 / 39
added pairs: 70
removed pairs: 3
metadata-changed pairs: 9
image models: 42 / 42 unchanged
```

## Validation gate

Passed before commit/push:

```text
docs/v0841-128-test-manifest.md row check: 128 rows, 101 deterministic/covered, 27 N/A/adapted, 0 TODO
TMPDIR=/workspace/tmp go test ./... -run 'QwenTokenPlan|QwenTokenPlanIndividual|RegisterBuiltinModels|OpenAICompletionsEmptyTools'
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0841/packages/ai/src/providers  # 1220/1220 provider/id exact
PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0841/packages/ai/src/models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-ai-0.84.1-package/package/dist/providers/data ./scripts/check-model-regeneration.sh  # normalized full metadata regeneration exact
python3 scripts/generate-image-models.py /workspace/tmp/pi-v0841/packages/ai/src/image-models.generated.ts /workspace/tmp/images_v0841_gate.go  # 42 image models, exact diff
make check  # includes check-model-regeneration
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

Additional final evidence correction:

```text
./scripts/check-model-regeneration.sh
# model regeneration metadata comparator passed
make check
# includes check-model-regeneration
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
make test-repro
# includes check-model-regeneration through test-repro-fast

# Clean worktree copy with no PI_AI_* overrides and no pre-existing fixtures:
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-clean-cache-wt make check GO_TMPDIR=/tmp
# check-model-regeneration fetched exact tag/package into the cache and passed

# Fault injection in clean worktree copy:
# openrouter/google/gemini-3-flash-preview MaxTokens 65536 -> 65537
GO_AI_MODEL_REGEN_CACHE=/workspace/tmp/go-ai-clean-cache-fault ./scripts/check-model-regeneration.sh
# failed with normalized regeneration diff showing 65537 vs 65536
```

Retained logs: `/workspace/tmp/go-ai-v0841-portable-gates/` and `/workspace/tmp/go-ai-v0841-metadata-gates/`.

Prior v0.84.1 logs retained: `/workspace/tmp/go-ai-v0841-gates/`.
