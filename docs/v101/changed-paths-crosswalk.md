# v1.0.1 changed-path disposition

Official `@earendil-works/pi-ai@1.0.1`, gitHead `a7229ddc21810d6245105978033b7df645ecc2f7`, prior official v1.0.0 `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`. Artifact SHA-256 `8a9e69b1309cf93405d87729fa123c8b11c6be7c646b16f34f8bef7b792f9138`; SHA-512 integrity independently checked. npm advertises SLSA provenance; no attestation signature verification is claimed.

Exact source diff: 19 paths (one added / 18 modified), +546/-153. Six changed test files; 171-file whole corpus (164 executable tests plus seven support/fixtures), all accounted for in [whole-corpus crosswalk](test-corpus-crosswalk.md). `changed-paths.txt` matches fixed Git name-status, including docs and six tests. Packaged provider JSON changes are catalog evidence, not substitutions for source diff rows.

| # | Exact upstream path | Disposition | Native evidence / rationale |
| ---: | --- | --- | --- |
| 1 | `packages/ai/CHANGELOG.md` | adapted | RELEASE.md v1.0.1 local receipt and versioned ledgers; no rewrite of accepted v1.0.0 history |
| 2 | `packages/ai/README.md` | adapted | Inline tool schema/redefinition/removal and initial prefix behaviour tested; ChatGPT conflict/manual-context cancellation adaptation documented |
| 3 | `packages/ai/package.json` | version/provenance adapted; JS SDK N/A | Official Anthropic SDK0.124.0→0.129.0; native Go direct HTTP needs no SDK/dependency upgrade |
| 4 | `packages/ai/scripts/generate-models.ts` | adapted/generated | scripts/generate-models.go emits all native model types from offline schema6 JSON, incl pricing tiers, Cloudflare IDs and Together rename |
| 5 | `packages/ai/scripts/hydrate-model-catalog.ts` (added) | adapted | Native -data-dir validates official manifest/files/structure/identities/modalities/costs, formats before atomic rename; twelve atomic fault fixtures |
| 6 | `packages/ai/scripts/model-data.ts` | adapted | Strict offline manifest timestamp/schema6/42files/hashes/structure and API/type/ID/provider checks; no live catalog fetch |
| 7 | `packages/ai/src/api/anthropic-messages.ts` | implemented | Fixed initial tools+placeholder and inline beta from first request; full inline schema/strict/eager, cache on content block, remove/redefine handling; consistent OAuth initial/inline/replay/incoming name tests |
| 8 | `packages/ai/src/api/bedrock-converse-stream.ts` | implemented | Eligible adaptive binding+beta, IDs/name/GovCloud exclusions, budget/display/effort retention, signed production Converse request capture |
| 9 | `packages/ai/src/api/cloudflare-workers-ai-system-one.ts` | implemented | Direct answers-key presence even null, nested Completed fallback, strict answers/billed usage; real Classify HTTP fixtures |
| 10 | `packages/ai/src/auth/oauth/openai-chatgpt.ts` | implemented/adapted | Occupied1455 fails before all host callbacks/exchanges; other bind errors propagate; tracked listener/idle connection cleanup; context prompt race cancels+joins, legacy prompt remains synchronous |
| 11 | `packages/ai/src/types.ts` | docs adaptation | Compatibility field comment reflects inline definitions and late same-name replacement; SupportsToolReferences retained as deprecated native compatibility alias |
| 12 | `packages/ai/src/utils/retry.ts` | implemented | Capacity pattern in production RetryAssistantCall, positive/negative/retry-limit/delay/cancel tests |
| 13 | `packages/ai/src/utils/transcript.ts` | JS deprecation N/A; native integration fixed | No Go HasToolRedefinitions export existed, no API invented. TransformMessages/insertSyntheticToolResults preserve RoleSystem without premature orphan flush |
| 14 | `packages/ai/test/bedrock-thinking-payload.test.ts` | implemented/adapted | bedrock_thinking_v101_test.go + corrected pre-v101 adaptive beta assertion; ID/name/path/region mocks |
| 15 | `packages/ai/test/cloudflare-workers-ai-system-one.test.ts` | implemented | classifier_cloudflare_v101_test.go + issue1 JSON/hook/usage regression tests |
| 16 | `packages/ai/test/model-data-validation.test.ts` | adapted | test-generate-models-v101.py twelve valid/repro/invalid/sentinel faults + three typed-regeneration drift gates |
| 17 | `packages/ai/test/stream.test.ts` | portable deterministic + live remainder | New NVIDIA catalog/typed ID metadata from official records; native HTTP/SSE provider suites run; live added NVIDIA requests require unavailable credential, not a hidden Go skip |
| 18 | `packages/ai/test/together-models.test.ts` | implemented | models_catalog_upstream_test.go uses DeepSeek-V4-Pro-0813; v101 typed old-ID absence and thinking metadata assertions |
| 19 | `packages/ai/test/transcript-tool-changes.test.ts` | implemented/adapted | anthropic_inline_tools_v101_test.go + anthropic_oauth_names_v101_test.go + transform_system_v101_test.go production wire/order/cache/strict/SSE proof |

Catalog evidence remains separate: packaged schema6 manifest stamp `03d2e1aeeee6eb16959d4f727b47b9b187efaf863c688a47889fb90d200e6812` and42 modules. Chat1532→1536 (+17/-13/54changed),41providers/10APIs; image57→59 (+2/0/0),1provider/1API; classifier15→20 (+5/0/0),5providers/2APIs; total1615. `catalog-delta.json` contains full-record identities/counts; all generated outputs come from checked-in native tooling. Changed packaged JSON provider metadata does not appear in this exact source-path table.

Bounded native integration additions: transform.go RoleSystem preservation, callback-server connection lifecycle, optional context-aware LoginCallbacks hook and current-registry test isolation. No workflows, dependencies, durable implementation or accepted release-history mutations. Native durable implementation is required in a separate staged feature cycle after this release boundary; M1a alone is a foundation, not full completion.
