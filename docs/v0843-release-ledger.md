# v0.84.3 release ledger

Audit target: official `@earendil-works/pi-ai` / `github.com/earendil-works/pi` tag `v0.84.3`, SHA `4e58f324fae8ebfa98a3d45181fb248072a2afac`, published 2026-08-24T11:09:57Z.

Previous accepted baseline: `v0.84.2`, SHA `914cf1472e715297caa30db4b9535d534a9eb718`.

Exact artifacts: source checkout `/workspace/tmp/pi-v0843`; npm package `/workspace/tmp/v0843/npm0843/package`; npm SHA-256 `9c40af2f43950f8e94e7bbcd0c1b3548f000972da00c4fb9c0d0529d4d7d5431`.

## Changed-path matrix (48 `packages/ai` paths)

Exact command: `git diff --name-status v0.84.2..v0.84.3 -- packages/ai`.

| Upstream path | Disposition | Go evidence / rationale |
| --- | --- | --- |
| `packages/ai/CHANGELOG.md` | N/A/docs/package metadata | Recorded in RELEASE; no Go runtime surface beyond audited deltas. |
| `packages/ai/README.md` | N/A/docs/package metadata | Recorded in RELEASE; no Go runtime surface beyond audited deltas. |
| `packages/ai/package.json` | N/A/docs/package metadata | Recorded in RELEASE; no Go runtime surface beyond audited deltas. |
| `packages/ai/scripts/generate-models.ts` | Adopted mechanically | Go generator/check script updated; exact v0.84.3 regeneration proves final artifacts. |
| `packages/ai/src/api/anthropic-messages.ts` | Implemented/adapted | Fallback request field/beta and fallback pricing added; raw `streamAnthropic` HTTP/SSE test covers `fallbacks`, beta, UA precedence, returned fallback model, and pricing. |
| `packages/ai/src/api/azure-openai-responses.ts` | Implemented | Azure Responses toolChoice and default User-Agent behavior covered. |
| `packages/ai/src/api/bedrock-converse-stream.ts` | Implemented/adapted | Redacted reasoning preservation/replay/finalization implemented; response hook metadata adapted to Go SDK modeled request-id and covered for present/absent request-id. |
| `packages/ai/src/api/google-generative-ai.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/src/api/google-shared.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/src/api/google-vertex.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/src/api/mistral-conversations.ts` | Implemented | Existing exact v0.84.2 wire contract retained; default User-Agent added. |
| `packages/ai/src/api/openai-codex-responses.ts` | Implemented | Simple/provider-neutral toolChoice propagation added; existing Codex Responses tests retained. |
| `packages/ai/src/api/openai-completions.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/src/api/openai-responses.ts` | Implemented | Responses/Azure toolChoice and xAI Responses metadata/replay retained. |
| `packages/ai/src/api/pi-messages.ts` | Reviewed/adapted | No additional Go runtime surface beyond generated catalog/tests. |
| `packages/ai/src/api/simple-options.ts` | Reviewed/adapted | No additional Go runtime surface beyond generated catalog/tests. |
| `packages/ai/src/auth/oauth/device-code.ts` | Reviewed/adapted | No additional Go runtime surface beyond generated catalog/tests. |
| `packages/ai/src/auth/oauth/github-copilot.ts` | Implemented/adapted | Real Go Copilot login/policy workflow now covers account catalog filtering, Individual fallback, known/tool-capable/unconfigured policy selection, 429 `Retry-After` retry, continuation after transport failure, bounded 5s policy retry budget, and returned credentials after policy stop for caller persistence. |
| `packages/ai/src/auth/oauth/kimi-coding.ts` | Reviewed/adapted | No additional Go runtime surface beyond generated catalog/tests. |
| `packages/ai/src/index.ts` | Reviewed/adapted | No additional Go runtime surface beyond generated catalog/tests. |
| `packages/ai/src/providers/xai.ts` | Implemented mechanically | xAI generated models now use Responses only; Grok 4.6 raw `/responses` test covers low/medium/high/xhigh reasoning mapping, encrypted reasoning include, endpoint/auth, and explicit User-Agent override. |
| `packages/ai/src/types.ts` | Implemented | ToolChoice type, thinking budget field, allowed fallback model fields mirrored in Go types. |
| `packages/ai/src/utils/sleep.ts` | N/A/adapted | Go retry sleeps already accept context cancellation; existing retry tests remain passing. |
| `packages/ai/test/anthropic-auth-token.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/azure-openai-base-url.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/azure-openai-tool-choice.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/baseten-models.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/bedrock-redacted-reasoning.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/bedrock-response-headers.test.ts` | Implemented/adapted | `v0843_redacted_reasoning_test.go` covers modeled request-id/status 200 hook invocation and absent-request-id non-invocation; raw Smithy response headers remain Go SDK-adapted. |
| `packages/ai/test/generate-models-strict.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/github-copilot-oauth.test.ts` | Implemented/adapted | `oauth/github_copilot_v0843_test.go` covers policy filtering, retry-after, transport failure continuation, bounded policy retry budget, and login returning credentials for caller persistence. |
| `packages/ai/test/google-raw-stop-reason.test.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/test/google-thinking-level-map.test.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/test/google-vertex-api-key-resolution.test.ts` | Implemented | Mapped Google thinking levels/budgets and default User-Agent covered. |
| `packages/ai/test/mistral-http-transport.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/model-catalog-types.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/openai-completions-reasoning-details.test.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/test/openai-completions-thinking-as-text.test.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/test/openai-completions-thinking-token-budget.test.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/test/openai-completions-tool-choice.test.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/test/openai-completions-tool-result-images.test.ts` | Implemented | Configurable thinking budget field variants and DeepSeek/max_tokens behavior covered. |
| `packages/ai/test/pi-messages.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/qwen-token-plan-models.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/stream.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/supports-xhigh.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/xai-responses.test.ts` | Implemented | `xai_responses_upstream_test.go` retains Grok 4.5 regression and adds Grok 4.6 raw Responses request-shape coverage. |
| `packages/ai/test/xiaomi-models.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |
| `packages/ai/test/zai-coding-plan-models.test.ts` | Classified in corpus manifest | See docs/v0843-136-test-manifest.md; exact path row retained. |

## Corpus and catalog evidence

- Whole corpus manifest: `docs/v0843-136-test-manifest.md` (136 rows, exact unique filename set, 25 changed-row markers).
- Text catalog: 1312 models across 39 providers; `compare-upstream-models.py` verifies 1312/1312 exact provider/id pairs.
- Image catalog: unchanged 45 image models; `scripts/check-model-regeneration.sh` verifies text and image catalogs.

## Validation evidence

Final gate passed before commit/push:

```text
scripts/validate-test-manifest.py docs/v0843-136-test-manifest.md /workspace/tmp/v0843/test_files.txt
# manifest rows: 136; unique paths: 136; expected paths: 136; changed-row markers: 25; manifest validation passed

PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0843/packages/ai/src/providers
# upstream pairs: 1312; generated pairs: 1312; exact match

PI_AI_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/models.generated.ts PI_AI_IMAGE_MODELS_GENERATED_TS=/workspace/tmp/pi-v0843/packages/ai/src/image-models.generated.ts PI_AI_MODEL_DATA_DIR=/workspace/tmp/v0843/npm0843/package/dist/providers/data TMPDIR=/workspace/tmp bash scripts/check-model-regeneration.sh
# model regeneration metadata comparator passed; image model regeneration comparator passed

# deliberate text/image faults failed as expected; logs under /workspace/tmp/go-ai-v0843-fault-logs/

TMPDIR=/workspace/tmp go test ./oauth ./inference/provider/anthropic ./inference/provider/openairesponses ./inference/provider/bedrock
# focused v0.84.3 correction packages PASS

TMPDIR=/workspace/tmp go test ./...
make check GO_TMPDIR=/workspace/tmp
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go vet ./...
make staticcheck GO_TMPDIR=/workspace/tmp
make check-logging
make test-repro GO_TMPDIR=/workspace/tmp
# all PASS

```
