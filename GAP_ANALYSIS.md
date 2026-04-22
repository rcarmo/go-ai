# go-ai Gap Analysis & Implementation Plan

## Source: `@mariozechner/pi-ai` v0.68.0

Full module inventory with porting status.

---

## 1. Core Framework

| pi-ai module | JS lines | go-ai equivalent | Status | Notes |
|---|---:|---|---|---|
| `types` | 1 (+ 366 d.ts) | `types.go` | ✅ Done | All types ported: Message, Context, Tool, Model, Usage, StreamOptions, ContentBlock, events |
| `stream` | 26 | `registry.go` | ✅ Done | `Stream()`, `Complete()`, `StreamSimple()`, `CompleteSimple()` |
| `api-registry` | 43 | `registry.go` | ✅ Done | `RegisterApi()`, `GetApiProvider()`, source-ID based unregister not yet implemented |
| `models` | 57 | `registry.go` | ✅ Partial | `GetModel()`, `ListModels()` done; missing `calculateCost()`, `supportsXhigh()`, `modelsAreEqual()` |
| `models.generated` | 14433 | — | ❌ Not started | Auto-generated model registry with all known models, costs, context windows |
| `env-api-keys` | 106 | `env.go` | ✅ Partial | Basic env key lookup; missing full provider-to-env-var mapping table |
| `index` | 13 | `doc.go` | ✅ Done | Package docs + re-exports |
| `cli` | 115 | — | ⏭️ Skip | CLI tool for testing; not part of the library API |

### Gaps in core:
- [ ] `calculateCost()` — compute cost breakdown from usage + model
- [ ] `supportsXhigh()` — check if model supports xhigh thinking
- [ ] `modelsAreEqual()` — compare models by id+provider
- [ ] `unregisterApiProviders(sourceId)` — remove providers by source
- [ ] `clearApiProviders()` — reset all
- [ ] Full env-var mapping table (per-provider key names)
- [ ] **Model registry code generator** — script to convert `models.generated.js` → Go

---

## 2. Providers

### 2.1 Implemented

| Provider | API | JS lines | go-ai lines | Status | Gaps |
|---|---|---:|---:|---|---|
| OpenAI Completions | `openai-completions` | 853 | 450 | ✅ Working | Missing: compat flags, cache control, throttle/retry, image content, `onPayload`/`onResponse` hooks |
| Anthropic Messages | `anthropic-messages` | 773 | 350 | ✅ Working | Missing: thinking budgets, cache control markers, image content, retry logic |

### 2.2 Not started

| Provider | API | JS lines | Complexity | Priority | Notes |
|---|---|---:|---|---|---|
| Google Generative AI | `google-generative-ai` | 398 + 337 shared | High | P1 | Uses `@google/genai` SDK; shared module with Vertex/GeminiCLI |
| Google Vertex AI | `google-vertex` | 419 + 337 shared | High | P2 | Same wire format as Google, different auth (service account) |
| Google Gemini CLI | `google-gemini-cli` | 778 + 337 shared | High | P2 | Cloud Code Assist API, OAuth required |
| Mistral | `mistral-conversations` | 533 | Medium | P1 | Own SSE format with `@mistralai/mistralai` SDK patterns |
| Amazon Bedrock | `bedrock-converse-stream` | 695 | High | P2 | Uses AWS SDK SigV4 signing; ConverseStream API |
| OpenAI Responses | `openai-responses` | 202 + 478 shared | Medium | P1 | New OpenAI API; shared with Azure/Codex |
| Azure OpenAI Responses | `azure-openai-responses` | 182 + 478 shared | Medium | P2 | Same as OpenAI Responses with Azure auth/endpoints |
| OpenAI Codex Responses | `openai-codex-responses` | 777 + 478 shared | High | P2 | WebSocket transport, OAuth, service tier routing |
| Faux (test) | custom | 367 | Low | P3 | Test double provider for unit testing |

### 2.3 Provider support modules

| Module | JS lines | Status | Notes |
|---|---|---|---|
| `register-builtins` | 260 | ✅ Partial | Go uses `init()` per provider; need lazy/deferred loading |
| `simple-options` | 35 | ❌ | Maps `ThinkingLevel` → provider-specific options |
| `transform-messages` | 181 | ❌ | Cross-provider tool call ID normalization |
| `github-copilot-headers` | 28 | ❌ | Copilot-specific header generation |
| `google-shared` | 337 | ❌ | Shared Gemini message/tool conversion |
| `openai-responses-shared` | 478 | ❌ | Shared Responses API stream processing |

---

## 3. OAuth

| Module | JS lines | Status | Priority | Notes |
|---|---|---:|---|---|
| `utils/oauth/index` | 130 | ❌ | P2 | OAuth provider registry + login orchestration |
| `utils/oauth/types` | 1 (+ d.ts) | ❌ | P2 | OAuthCredentials, OAuthProvider, OAuthLoginCallbacks types |
| `utils/oauth/pkce` | 30 | ❌ | P2 | PKCE code verifier/challenge generation |
| `utils/oauth/github-copilot` | 291 | ❌ | P2 | GitHub device flow OAuth for Copilot |
| `utils/oauth/google-gemini-cli` | 481 | ❌ | P2 | Google OAuth for Gemini CLI / Cloud Code Assist |
| `utils/oauth/google-antigravity` | 376 | ❌ | P3 | Google OAuth for Antigravity (Vertex consumer) |
| `utils/oauth/anthropic` | 334 | ❌ | P3 | Anthropic OAuth (Console tokens) |
| `utils/oauth/openai-codex` | 373 | ❌ | P3 | OpenAI OAuth for Codex/ChatGPT subscriptions |
| `utils/oauth/oauth-page` | 104 | ⏭️ Skip | P4 | HTML page for OAuth redirect; browser-only |

---

## 4. Utilities

| Module | JS lines | go-ai equivalent | Status | Notes |
|---|---|---:|---|---|
| `utils/event-stream` | 80 | `internal/eventstream/` | ✅ Partial | SSE parser done; missing `EventStream` class (async iterable with result promise) |
| `utils/json-parse` | 28 | `internal/jsonparse/` | ✅ Done | Partial JSON parser |
| `utils/overflow` | 131 | — | ❌ | Context overflow detection (per-provider error patterns) |
| `utils/validation` | 79 | — | ❌ | Tool call argument validation against JSON Schema |
| `utils/hash` | 13 | — | ❌ | Short deterministic hash |
| `utils/headers` | 7 | — | ❌ | Headers → Record conversion (trivial in Go) |
| `utils/sanitize-unicode` | 25 | — | ❌ | Remove unpaired Unicode surrogates |
| `utils/typebox-helpers` | 20 | — | ⏭️ Skip | TypeBox-specific; Go uses `json.RawMessage` for schemas |

---

## 5. OpenAI Completions Compat Flags

The OpenAI Completions provider has extensive compatibility flags for OpenAI-compatible APIs. These are critical for supporting Ollama, Groq, xAI, OpenRouter, vLLM, etc.

| Flag | Status | Notes |
|---|---|---|
| `supportsStore` | ❌ | Whether to send `store` field |
| `supportsDeveloperRole` | ❌ | `developer` vs `system` role |
| `supportsReasoningEffort` | ❌ | Whether `reasoning_effort` is accepted |
| `reasoningEffortMap` | ❌ | Map ThinkingLevel → provider values |
| `supportsUsageInStreaming` | ❌ | `stream_options.include_usage` |
| `maxTokensField` | ❌ | `max_completion_tokens` vs `max_tokens` |
| `requiresToolResultName` | ❌ | Whether tool results need `name` field |
| `requiresAssistantAfterToolResult` | ❌ | Insert empty assistant message |
| `requiresThinkingAsText` | ❌ | Convert thinking blocks to `<thinking>` delimiters |
| `thinkingFormat` | ❌ | openai/openrouter/zai/qwen variants |
| `openRouterRouting` | ❌ | Provider selection/pricing/latency prefs |
| `vercelGatewayRouting` | ❌ | Vercel AI Gateway provider routing |
| `zaiToolStream` | ❌ | z.ai streaming tool call support |
| `supportsStrictMode` | ❌ | Whether tools accept `strict: true` |
| `cacheControlFormat` | ❌ | Anthropic-style cache markers |
| `sendSessionAffinityHeaders` | ❌ | Session affinity for prompt caching |

---

## 6. Implementation Plan

### Phase 1 — Core completeness (P0)

**Goal**: Make OpenAI + Anthropic providers production-quality.

1. **`compat` flags for OpenAI Completions** — add `OpenAICompletionsCompat` struct and auto-detection from base URL
2. **Image content support** — handle `ImageContent` in message conversion for both providers
3. **Retry logic** — HTTP 429/5xx retry with backoff and `Retry-After` header parsing
4. **`simple-options`** — `ThinkingLevel` → provider-specific option mapping
5. **`transform-messages`** — tool call ID normalization for cross-provider hand-off
6. **`overflow`** — context overflow detection with provider-specific error patterns
7. **`validation`** — tool call argument validation against JSON Schema
8. **`calculateCost()`** / `supportsXhigh()` / `modelsAreEqual()`
9. **Cache control** — Anthropic `cache_control` markers, OpenAI session affinity
10. **`sanitize-unicode`** — unpaired surrogate removal
11. **Full env-var mapping table**

**Estimated effort**: M

### Phase 2 — OpenAI Responses API family (P1)

**Goal**: Support the newer OpenAI Responses protocol (used by GPT-5.x, Codex).

1. **`openai-responses-shared`** — shared message conversion and stream processing
2. **`openai-responses`** — native OpenAI Responses provider
3. **`azure-openai-responses`** — Azure variant with auth differences
4. **Reasoning summary** — `reasoningSummary` option support

**Estimated effort**: M

### Phase 3 — Google providers (P1)

**Goal**: Support Gemini models via all three Google APIs.

1. **`google-shared`** — Gemini message/tool/thinking conversion
2. **`google`** — Google Generative AI (API key auth)
3. **`google-vertex`** — Vertex AI (service account auth)
4. **`google-gemini-cli`** — Cloud Code Assist (OAuth)
5. **Thought signatures** — Gemini thought signature handling

**Estimated effort**: L

### Phase 4 — Mistral + remaining providers (P1-P2)

1. **`mistral`** — Mistral Conversations API
2. **`amazon-bedrock`** — AWS Bedrock ConverseStream (SigV4 signing)
3. **`openai-codex-responses`** — WebSocket transport, OAuth
4. **`github-copilot-headers`** — Copilot header generation

**Estimated effort**: L

### Phase 5 — OAuth (P2)

1. **OAuth types and registry**
2. **PKCE helpers**
3. **GitHub Copilot device flow**
4. **Google Gemini CLI OAuth**
5. **Anthropic OAuth**
6. **OpenAI Codex OAuth**

**Estimated effort**: L

### Phase 6 — Model registry + code generation (P1)

1. **Code generator** — parse `models.generated.js` and emit Go
2. **`models_generated.go`** — all known models with costs, context windows, capabilities
3. **Auto-update workflow** — CI job to regenerate when pi-ai releases a new version

**Estimated effort**: M

### Phase 7 — Test provider + integration tests (P2)

1. **`faux` provider** — test double for unit testing
2. **Integration tests** — against real APIs (OpenAI, Anthropic, Google)
3. **Cross-language round-trip test** — serialize Context in Go, deserialize in TS, verify

**Estimated effort**: S

### Phase 8 — CI + packaging (P2)

1. **GitHub Actions** — build, test, lint (`golangci-lint`)
2. **Go module versioning** — `v0.x.y` tags
3. **GoDoc** — published documentation
4. **Benchmarks** — streaming throughput, memory allocation

**Estimated effort**: S

---

## 7. Line count summary

| Category | pi-ai JS lines | go-ai Go lines | Coverage |
|---|---:|---:|---|
| Core framework | 246 | 480 | ~80% |
| Providers (implemented) | 1,626 | 800 | ~50% (missing compat, retry, images) |
| Providers (not started) | 5,261 | 0 | 0% |
| Provider support | 841 | 0 | 0% |
| OAuth | 2,120 | 0 | 0% |
| Utils | 363 | 150 | ~40% |
| Models generated | 14,433 | 0 | 0% (needs code gen) |
| CLI | 115 | — | Skip |
| **Total** | **24,890** | **1,430** | **~6%** |

### Excluding auto-generated code and CLI:

| | pi-ai | go-ai | Coverage |
|---|---:|---:|---|
| **Handwritten code** | 10,342 | 1,430 | **~14%** |

---

## 8. Priority matrix

```
            HIGH IMPACT
                │
   ┌────────────┼────────────┐
   │  P0: Core  │  P1: OpenAI│
   │  quality   │  Responses │
   │            │  + Google  │
   │            │  + Models  │
LOW ────────────┼────────────── HIGH EFFORT
   │  P3: Faux  │  P2: OAuth │
   │  + edge    │  + Bedrock │
   │  utils     │  + Codex   │
   │            │            │
   └────────────┼────────────┘
                │
            LOW IMPACT
```

---

## 9. Recommended execution order

1. **Phase 1** — Core completeness (compat flags, retry, images, overflow, validation)
2. **Phase 6** — Model registry code generation (unlocks all providers for testing)
3. **Phase 2** — OpenAI Responses family
4. **Phase 3** — Google providers
5. **Phase 4** — Mistral + Bedrock
6. **Phase 8** — CI + packaging
7. **Phase 5** — OAuth
8. **Phase 7** — Faux provider + integration tests
