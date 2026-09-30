# v0.99.2 changed-test crosswalk (6 rows)

Manifest: `docs/v0992/changed-tests.txt`, SHA-256 `1ad16f63dc47b019cdcf4fdf7029c86785f4cbac38158e7fb63db963ce9ce66d`.

| upstream test | disposition | Go evidence / rationale |
|---|---|---|
| `packages/ai/test/anthropic-eager-tool-input-compat.test.ts` | covered-existing/updated | Existing `inference/provider/anthropic/anthropic_eager_tool_input_compat_test.go` covers retained eager behavior. The strict-schema block removed upstream is now covered by `anthropic_strict_tool_schema_v0992_test.go`. |
| `packages/ai/test/anthropic-federation-sdk.test.ts` | adapted/n/a | Exact Anthropic TS SDK credential-chain hooks are JS SDK-only. Go direct HTTP implementation is covered by `anthropic_federation_v0992_test.go`, including explicit-auth no file read/exchange and in-process token cache reuse/coalescing. |
| `packages/ai/test/anthropic-federation.test.ts` | implemented | `inference/provider/anthropic/anthropic_federation_v0992_test.go` covers env resolution, precedence, exact token URL/body, trim, bearer header, cache reuse/expiry, malformed/missing token response, unsupported token type, transport error, cancellation, concurrency coalescing, reset/isolation, and no token leak in endpoint-derived diagnostics. |
| `packages/ai/test/anthropic-strict-tool-schema.test.ts` | implemented | `inference/provider/anthropic/anthropic_strict_tool_schema_v0992_test.go` covers normalized strict full schema, prefer fallback for Anthropic-rejected keyword classes, require rejection, and eager behavior independence. |
| `packages/ai/test/models-entry.test.ts` | n/a | JS package-entry/module-loader mechanics. Go has explicit registry APIs and side-effect provider packages, not a Node export/barrel. Catalog behavior is covered by `tests/models_v0992_typed_registry_test.go` and the full-record regeneration comparator. |
| `packages/ai/test/overflow.test.ts` | implemented | `tests/overflow_upstream_test.go` adds the z.ai CN `Prompt exceeds max length` detection. |

Whole upstream test corpus for this release: `docs/v0992/test-corpus-171.txt`, SHA-256 `9d24da3ede393a95a7131b1c9ac494f57d8165161d6eb581109c86809131abfc`.
