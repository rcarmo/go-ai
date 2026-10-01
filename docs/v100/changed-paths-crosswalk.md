# v1.0.0 changed-path disposition crosswalk (8 rows)

Official input: `@earendil-works/pi-ai@1.0.0`, upstream tag/gitHead `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`, npm tarball SHA-256 `f39b99c29b8598f175b10840e5d2a81983e7c0ce5cae4d7df83a1007447d2c2b`.

Manifest: `docs/v100/changed-paths.txt`, SHA-256 `b8db49581470036b68078ac093dc6b41eaa92222647b14cf44a92b870d54eab4`.

| # | upstream | disposition | Go evidence / rationale |
|---:|---|---|---|
| 1 | `packages/ai/CHANGELOG.md` | documented/n/a | Release metadata only; no Go runtime surface. |
| 2 | `packages/ai/package.json` | documented/adapted | Version moved to `1.0.0`; Go release docs/scripts now track official `1.0.0` tarball and hash. |
| 3 | `packages/ai/src/api/openai-responses-shared.ts` | implemented | `inference/provider/openairesponses/responses.go` resolves one grammar map from transcript declarations and provider capability. This map drives tool declarations, calls and results: unsupported/default capability uses function calls/results; supported uses custom calls/results. Foreign provider/API history normalizes IDs to `fc_<hash>` before prefix gating, so custom replay omits them; same-source matching `ctc_` remains valid. Missing/null custom input is empty text. Production OnPayload controls in `responses_grammar_replay_v100_test.go` cover these cases, transcript additions and radius#115; existing foreign/namespace/request tests remain green. |
| 4 | `packages/ai/src/auth/oauth/anthropic.ts` | implemented/adapted | `oauth/anthropic.go` adds backward-compatible login method selection, default browser flow through shared callback server, copy-code flow using `https://platform.claude.com/oauth/code/callback`, verifier-as-state validation, JSON authorization/refresh exchange at the default platform endpoint, exact keys/headers and `code=true` auth parameter, official selection labels, refresh rotation/fallback, cancellation/selection errors and redacted diagnostics. Defaulted configuration copies preserve `&AnthropicProvider{}` Login/Refresh without receiver mutation. The shared completion hook exchanges tokens before 200 success, returns 502 on failure, and exchanges once for duplicate callbacks. Length-delimited responses flush before completion, with graceful server shutdown. Tests in `oauth/anthropic_test.go` and `oauth/anthropic_async_callback_v100_test.go` intercept transport/listener; no live credentials. |
| 5 | `packages/ai/src/utils/oauth-page.ts` | implemented | Shared production callback HTML in `oauth/callback_server.go` contains the official Pi SVG geometry/fills `#F09082`, `#4D9ABF`, `#F1BE58` and escaped `Signed in to <provider>.` text. `oauth/callback_page_v100_test.go` checks real HTTP output and malicious-provider escaping; Anthropic's browser test also checks the actual shared response. |
| 6 | `packages/ai/test/anthropic-oauth.test.ts` | implemented/adapted | Deterministic Go tests cover browser and copy-code Anthropic OAuth selection, redirect URIs, JSON authorization/refresh bodies/headers, refresh fallback, zero-value compatibility, state mismatch/cancellation, selection errors, callback completion ordering/duplicate exchange prevention, asynchronous 200/502 response delivery and no token/code/verifier leakage. SDK/browser UI mechanics without Go equivalents are adapted through callbacks. |
| 7 | `packages/ai/test/constrained-sampling.test.ts` | implemented | Responses grammar replay production-path regressions cover radius#115: `call_1|fc_...` grammar replay emits `custom_tool_call`, `call_id=call_1`, `input=abc`, and no `id`; matched-prefix controls cover `fc_` and `ctc_`. |
| 8 | `packages/ai/test/oauth-callback-server.test.ts` | implemented | `oauth/callback_page_v100_test.go` checks actual shared callback HTTP output: SVG geometry/fills, content type, signed-in text and HTML escaping. `oauth/anthropic_async_callback_v100_test.go` checks repeated asynchronous success/failure delivery under race; the external auditor overlay also passes. Nil completion hooks preserve existing ChatGPT behaviour. State/code validation is retained. |

## Catalog oracle

Generated only through `scripts/generate-models.go` from exact official `dist/models.generated.js`.

- Chat: `1529` → `1532` (+5/-2/19 metadata changes), `41` model-bearing providers. The official package imports `42` provider modules; the extra `typesafe` provider is classifier-only in `dist/providers/data/typesafe.json` and has no chat model rows.
- Image: `57` → `57` (+0/-0/0).
- Classifier: `15` → `15` (+0/-0/1 metadata change).

Focused evidence so far:

```text
go test ./inference/provider/openairesponses -run 'Test.*(Grammar|ForeignToolCall|Namespace|Request)' -count=1
# passed

go test ./oauth -count=1
# passed

PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js ./scripts/check-model-regeneration.sh
# chat/image/classifier regeneration comparators passed

PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js python3 scripts/test-check-model-regeneration.py
# deliberate text/image/classifier fault gates passed
```
