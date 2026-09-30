# v0.99.2 changed-path disposition crosswalk (15 rows)

Official input: `@earendil-works/pi-ai@0.99.2`, npm tarball SHA-256 `0b3df8791b488216f309d908789294a744bb61bbaad123d94098e56df9538d25`, upstream commit `005af57d88ee23b33778f343a9595b32e67ff788`.

Manifest: `docs/v0992/changed-paths.txt`, SHA-256 `53b2c290d902bb8d79c87e035b87c52a13b97617849ea85b51c8e2b11133cc15`.

| # | upstream | disposition | Go evidence / rationale |
|---:|---|---|---|
| 1 | `packages/ai/CHANGELOG.md` | documented/n/a | Release metadata only; no Go runtime surface. |
| 2 | `packages/ai/README.md` | documented/n/a | Documents JS `@earendil-works/pi-ai/models` package-entry usage. Go has explicit package registration and generated catalogs; no Node export boundary. |
| 3 | `packages/ai/package.json` | documented/n/a | Version/dependency plus JS `./models` export. Go module layout is unchanged; `models-entry.test.ts` classified separately as JS package-entry mechanics. |
| 4 | `packages/ai/src/api/anthropic-messages.ts` | implemented/adapted | `inference/provider/anthropic/federation.go`; `inference/provider/anthropic/anthropic.go`; `anthropic_federation_v0992_test.go`; `anthropic_strict_tool_schema_v0992_test.go`. Go direct HTTP path implements Anthropic federation token exchange/cache/coalescing and strict schema fallback; JS SDK credential-chain internals are not used. |
| 5 | `packages/ai/src/api/constrained-sampling.ts` | implemented | `schema_strict.go` adds provider-specific unsupported-keyword hook; Anthropic provider passes the hook; strict/fallback/require cases covered by `anthropic_strict_tool_schema_v0992_test.go`. |
| 6 | `packages/ai/src/env-api-keys.ts` | implemented | Federation env constants are consumed in `inference/provider/anthropic/federation.go`; precedence and optional/required variables covered by federation tests. |
| 7 | `packages/ai/src/providers/anthropic.ts` | implemented/adapted | Direct Go provider path handles Anthropic federation env auth, API-key/auth-token/header precedence, anthropic-only gating, bearer request auth, and scoped env propagation in tests. |
| 8 | `packages/ai/src/utils/overflow.ts` | implemented | `context.go` adds z.ai CN `Prompt exceeds max length`; `tests/overflow_upstream_test.go` covers it. |
| 9 | `packages/ai/src/utils/provider-retry.ts` | implemented | `provider_retry.go` ignores non-finite `Retry-After` values and falls back to exponential retry; production HTTP path covered by `TestDoProviderRequestWithRetryFallsBackForNonFiniteRetryAfterHeader`. |
| 10 | `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | covered-existing/updated | Retained eager-tool cases remain in `anthropic_eager_tool_input_compat_test.go`; strict-schema assertions moved to `anthropic_strict_tool_schema_v0992_test.go`. |
| 11 | `packages/ai/test/anthropic-federation-sdk.test.ts` | adapted/n/a | Exact TS SDK hooks (`_shouldResolveDefaultCredentials`, SDK token cache) are JS SDK-only. Go equivalent direct HTTP behavior is covered by `anthropic_federation_v0992_test.go`: no ambient SDK credential chain, explicit auth prevents file read/exchange, exchange cache reuse/coalescing. |
| 12 | `packages/ai/test/anthropic-federation.test.ts` | implemented | `anthropic_federation_v0992_test.go` covers exact URL/body, whitespace-trimmed identity token, bearer header, cache reuse/expiry, concurrent coalescing, malformed/missing token response, token type rejection, transport error, cancellation, reset/isolation, and no token leak in endpoint-derived diagnostics. |
| 13 | `packages/ai/test/anthropic-strict-tool-schema.test.ts` | implemented | `anthropic_strict_tool_schema_v0992_test.go` covers supported normalized strict schema, prefer fallback for minimum/maximum, `minItems > 1`, unsupported format, require rejection, and eager-input independence. |
| 14 | `packages/ai/test/models-entry.test.ts` | n/a | JS package-entry/module-loader check for `@earendil-works/pi-ai/models`; Go has no Node barrel export. Go analogue remains explicit `RegisterBuiltinModels`, `RegisterBuiltinImageModels`, and `RegisterBuiltinClassifierModels`, now covered by v0.99.2 typed registry tests and full-record regeneration comparator. |
| 15 | `packages/ai/test/overflow.test.ts` | implemented | z.ai CN overflow regression added to `tests/overflow_upstream_test.go`. |

## Catalog oracle

Generated from exact published `dist/models.generated.js`.

- Chat: `1523` → `1529` (+6/-0): `amazon-bedrock/anthropic.claude-sonnet-5-5`, `amazon-bedrock/openai.gpt-6.1-sol`, `amazon-bedrock/us.openai.gpt-6.1-sol`, `baseten/deepseek-ai/DeepSeek-V4.1-Flash-Fast`, `github-copilot/gpt-6.1-sol`, `opencode/gpt-6.1-sol`. Existing `openai/gpt-6.1-sol` remains present; `tests/models_v0992_typed_registry_test.go` keeps durable OpenAI request/image-limit assertions separate from the new Copilot record's context/header/tier assertions.
- Image: `57` → `57` (+0/-0), proven unchanged by full-record regeneration comparator.
- Classifier: `12` → `15` (+3/-0): `openrouter/inception/mercury-decide:free`, `openrouter/togethercomputer/tev1-4b-experimental`, `vercel-ai-gateway/liquid/d1`.

Focused evidence before broad gates:

```text
go test ./inference/provider/anthropic -run 'TestAnthropic(Federation|StrictToolSchema)' -count=1
# ok

go test ./tests -run 'Test(DoProviderRequestWithRetryFallsBackForNonFiniteRetryAfterHeader|OverflowDetectsZAICNPromptExceedsMaxLengthErrors)' -count=1
# ok

PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-0992/tar/package/dist/models.generated.js ./scripts/check-model-regeneration.sh
# chat/image/classifier regeneration comparators passed

PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-0992/tar/package/dist/models.generated.js python3 scripts/test-check-model-regeneration.py
# deliberate text/image/classifier fault gates passed

go test ./tests -run 'Test(V0844CatalogCounts|V0850CatalogCounts|V0992UnifiedTypedLookup|RegisterBuiltinModels|V0992TypedRegistry)' -count=1
# ok
```
