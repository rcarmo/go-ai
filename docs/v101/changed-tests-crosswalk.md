# v1.0.1 changed-test crosswalk

Six changed upstream tests. Whole corpus171 rows is separate;164 executable files plus7 support/fixture files. No pending classifications; live credential remainders are labelled independently of deterministic coverage.

| Upstream test | Native disposition and production proof |
| --- | --- |
| `bedrock-thinking-payload.test.ts` | Implemented/adapted: `inference/provider/bedrock/bedrock_thinking_v101_test.go`, updated `bedrock_thinking_payload_upstream_test.go`; ID/name/GovCloud/adaptive/budget/effort and actual AWS client signed HTTP capture |
| `cloudflare-workers-ai-system-one.test.ts` | Implemented: `tests/classifier_cloudflare_v101_test.go`, existing issue1 hook/strict union/error/usage tests; direct Clef/flash, answers-null branch, nested envelopes, costs |
| `model-data-validation.test.ts` | Adapted offline: `scripts/test-generate-models-v101.py` valid full catalog/repro + twelve malformed/missing/hash/identity/duplicate/atomic failure cases; three regeneration drift fault gates |
| `stream.test.ts` | Portable IDs/catalog metadata implemented through native generated data and `models_v101_catalog_test.go`; native local HTTP/SSE providers already exercised. Added live NVIDIA suite needs unavailable NVIDIA_API_KEY; not represented as credential-skipped Go proof |
| `together-models.test.ts` | Implemented: updated `tests/models_catalog_upstream_test.go` removedV4Pro→0813 reasoning compat/map; `models_v101_catalog_test.go` removal/presence typed assertions |
| `transcript-tool-changes.test.ts` | Implemented/adapted: `anthropic_inline_tools_v101_test.go` initial prefix+placeholder/header/fulldefinitions/redefinition/removal/OAuth/cache/fallback; `tests/transform_system_v101_test.go` system metadata/order without tool orphan flush |

Additional production controls not in changed-test inventory: occupied1455 bind/no callbacks/no exchanges, non-address bind failure, callback-win/manual and cancellation idle/listener cleanup in `oauth/openai_chatgpt_callback_v101_test.go`; actual capacity retry/max attempts/delay/cancel in `tests/retry_capacity_v101_test.go`.
