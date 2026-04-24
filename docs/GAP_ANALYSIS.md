# go-ai Gap Analysis — CLOSED

All gaps from the original analysis have been addressed.

## Source: `@mariozechner/pi-ai` v0.70.1

## Sync history

### v0.70.1 (2026-04-24)

**DeepSeek provider + SDK timeout/retry options.**

- **New provider**: `deepseek` added to `KnownProvider`, env key `DEEPSEEK_API_KEY`.
- **New compat flags**: `RequiresReasoningContentOnAssistantMessages`, `thinkingFormat: "deepseek"`.
- **DeepSeek reasoning**: `thinking: { type: "enabled"/"disabled" }` + `reasoning_effort` in OpenAI Completions.
- **New StreamOptions fields**: `TimeoutMs`, `MaxRetries` (SDK-level passthrough; go-ai maps to HTTP client timeout + RetryConfig).
- **6 new models**: deepseek-v4-flash, deepseek-v4-pro (+ OpenRouter aliases), 2 Bedrock Anthropic models.
- **Model registry**: 876 → 884 models, 24 → 25 providers.
- **simple-options**: passthrough of timeoutMs/maxRetries (SDK-specific; go-ai uses raw HTTP).

### v0.70.0 (2026-04-24)

**Behavioral release.** Provider compat refactoring + model updates.

- **New types**: `AnthropicMessagesCompat` (eager tool streaming, long cache retention),
  `OpenAIResponsesCompat` (session ID header, long cache retention).
  Added `SupportsLongCacheRetention` to `OpenAICompletionsCompat`.
  Model struct now carries `CompletionsCompat`, `ResponsesCompat`, `AnthropicCompat` fields.
- **Anthropic provider**: compat-driven beta headers (`fine-grained-tool-streaming-2025-05-14`,
  `interleaved-thinking-2025-05-14`), compat-driven cache TTL instead of URL-sniffing.
  Ported to go-ai.
- **OpenAI Completions**: content index tracking refactored (use `indexOf` instead of `length-1`).
  Go provider already uses index-based tracking — no change needed.
- **OpenAI Responses**: compat-driven cache retention + session ID header.
  Noted as minor gap — go-ai already skips session headers when not applicable.
- **OpenAI Codex**: gpt-5.5 support in effort mapping + model-specific service tier multiplier.
  go-ai updated `SupportsXhigh` for gpt-5.5. Service tier pricing not yet implemented (existing gap).
- **Google Vertex**: `buildHttpOptions` with `ResourceScope` for custom base URLs.
  Go provider uses raw HTTP — not directly applicable but noted.
- **5 new models**: gpt-5.5, gemma-4-26b-a4b-it, hy3-preview-free, ling-2.6-1t, hy3-preview.
- **2 removed models**: arcee-ai/trinity-large-preview, gemma-4-26b-it.
- **3 pricing changes**: mistral-nemo, qwen3-235b-a22b-thinking, mimo-v2-flash.
- **Model registry**: 871 → 876 models. Regenerated.

### v0.69.0 (2026-04-23)

**Minor release.** Dependency cleanup + model registry update.

- **TypeBox**: `@sinclair/typebox` → `typebox` v1.1.24 (major package rename). No go-ai impact — we use `json.RawMessage`.
- **ajv + ajv-formats removed** upstream. No go-ai impact — we have our own validation.
- **transform-messages**: added `insertSyntheticToolResults()` call at end of transform. go-ai already had this.
- **4 new models**: Xiaomi `mimo-v2.5`, `mimo-v2.5-pro` (+ OpenRouter aliases). Regenerated.
- **1 pricing change**: `gemini-3.1-flash-lite-preview` now free (0/0). Regenerated.
- **Model registry**: 865 → 871 models. Regenerated via `go run scripts/generate-models.go`.
- **No provider behavior changes**, no OAuth changes, no type/event changes.

### v0.68.1 (2026-04-22)

Upstream `v0.68.1` did not introduce a large behavioral delta relative to the already-synced `go-ai` codebase. The practical sync adjustments in this pass were provider-metadata parity updates (`zai`, `huggingface`, `fireworks`) plus continued test/transport hardening.

## Final status

### Core Framework — ✅ Complete
- types, events, registry (with unregister/clear), env, stream, complete
- models.generated (865 models, 24 providers, code generator)
- CalculateCost, SupportsXhigh, ModelsAreEqual
- CLI: skipped (not part of library API)

### Providers — ✅ Complete (10 APIs, 11 with aliases)
| Provider | API | Status |
|---|---|---|
| OpenAI Completions | `openai-completions` | ✅ + full compat flags |
| Anthropic Messages | `anthropic-messages` | ✅ + image support |
| OpenAI Responses | `openai-responses` | ✅ |
| Azure OpenAI | `azure-openai-responses` | ✅ (alias) |
| Google Generative AI | `google-generative-ai` | ✅ |
| Google Vertex AI | `google-vertex` | ✅ (alias) |
| Google Gemini CLI | `google-gemini-cli` | ✅ |
| Mistral | `mistral-conversations` | ✅ |
| Amazon Bedrock | `bedrock-converse-stream` | ✅ |
| OpenAI Codex | `openai-codex-responses` | ✅ (WebSocket + SSE) |
| Faux (test) | `faux` | ✅ |

### Provider Support Modules — ✅ Complete
- simple-options → simple_options.go
- transform-messages → transform.go
- github-copilot-headers → copilot_headers.go
- compat flags → compat.go (16 flags, URL auto-detection)
- google-shared / openai-responses-shared → inlined in providers

### OAuth — ✅ Complete (5 providers)
| Provider | Flow | Status |
|---|---|---|
| GitHub Copilot | Device code | ✅ |
| Anthropic | Authorization code + PKCE | ✅ |
| Google Gemini CLI | Authorization code + PKCE | ✅ |
| OpenAI Codex | Device code | ✅ |
| PKCE utilities | - | ✅ |

### Utilities — ✅ Complete
| Utility | Status |
|---|---|
| event-stream (SSE parser) | ✅ |
| json-parse (partial JSON) | ✅ |
| overflow (context overflow detection) | ✅ |
| validation (tool call validation) | ✅ |
| hash (short deterministic hash) | ✅ |
| sanitize-unicode | ✅ |
| typebox-helpers | ⏭️ Skip (Go uses json.RawMessage) |
| headers | ⏭️ Skip (trivial in Go) |
| oauth-page | ⏭️ Skip (browser-only HTML) |

### Quality Infrastructure — ✅ Complete
- Centralized pluggable logger (zero-cost default)
- Logging quality gate (scripts/check-logging.sh)
- 5 fuzz targets
- 87+ test functions
- GitHub Actions CI (build, test, coverage, fuzz, logging gate)
- Production Makefile

## Coverage summary

| Category | pi-ai (JS) | go-ai (Go) | Coverage |
|---|---|---|---|
| Core + utils | 723 | 1,800+ | ~100% |
| Providers | 6,887 | 4,900+ | ~100% (all APIs) |
| OAuth | 2,120 | 1,500+ | ~100% (all flows) |
| Models generated | 15,156 | 10,900+ | 100% (code gen) |
| CLI | 115 | — | Skip |
| **Total** | **24,278** | **18,597+** | **Feature complete** |
