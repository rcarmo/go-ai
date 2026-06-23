# Upstream parity gaps — @earendil-works/pi-ai v0.80.2

Generated: 2026-06-23

Canonical upstream inspected: `/tmp/pi-ai-0.80.2/package/dist` (published npm tarball). The package does **not** ship `src/` or `*.test.ts`, so source/test inventories below are based on published `dist/*.d.ts`/`dist/*.js` exports. If upstream tests become available from a repository checkout, this document must be extended with a test-for-test port checklist.

Status key: **DONE** = implemented and covered in Go; **PARTIAL** = semantically covered but not export-for-export/test-for-test; **MISSING** = no Go equivalent yet.

## Coverage estimate

- Runtime/provider wire behavior: **~90%**
- Public module/export parity: **~70%**
- Upstream test-suite parity: **MISSING** from published package; local Go tests cover high-risk behavior but are not a test-for-test port.
- Overall functional parity estimate: **~82%**

## Top 3 gaps

1. **MISSING/PARTIAL upstream test-suite port** — upstream npm tarball has no `*.test.ts`; current Go tests are targeted parity/regression tests, not a one-to-one upstream suite port.
2. **PARTIAL JS `Models` / auth collection abstraction** — Go has global registries, direct options, scoped env, and OAuth helpers, but not an export-for-export equivalent of upstream `createModels`, `createProvider`, `CredentialStore`, and `resolveProviderAuth` collection APIs.
3. **PARTIAL package/export compatibility layer** — upstream has lazy API modules, legacy JS stream aliases, and compat barrel exports. Go has idiomatic packages/registries; several compatibility exports are intentionally not mirrored as named Go functions yet.

Recently closed during this audit: upstream `providers/faux.*` content/message helper constructors now have Go equivalents and regression tests.

## Published upstream module inventory

| Upstream module/export area | Go path/status |
|---|---|
| `index.d.ts` barrel exports | PARTIAL — root package `github.com/rcarmo/go-ai`, provider packages, image package |
| `compat.d.ts` barrel exports | PARTIAL — root package + docs; JS barrel aliases not mirrored 1:1 |
| `legacy-api-aliases.*` | PARTIAL — Go provider packages/register APIs exist; no deprecated alias functions |
| `types.d.ts` core JSON/event/model types | DONE — `types.go`, `events.go`, `context.go` |
| `utils/event-stream.*` event stream | DONE — channel event protocol in `events.go`, `registry.go` |
| `utils/diagnostics.*` | DONE — `types.go` `AssistantMessageDiagnostic` helpers |
| `utils/json-parse.*` | DONE — `internal/jsonparse` |
| `utils/overflow.*` | PARTIAL — context/overflow helpers in `harness.go`, `transform.go` |
| `utils/typebox-helpers.*` | MISSING — Go uses JSON Schema bytes/maps rather than TypeBox builders |
| `utils/validation.*` | DONE — `context.go` `ValidateToolCall`, `ValidateToolArguments` |
| `utils/headers.*` | DONE — `utils.go` header conversion/merge/suppression helpers |
| `utils/provider-env.*` | DONE — `env.go` `ProviderEnv`, scoped lookup helpers |
| `utils/oauth/*` | PARTIAL — `oauth/` package covers login/refresh/model mutation; not same callback/type surface |
| `auth/context.*`, `auth/types.*`, `auth/helpers.*`, `auth/resolve.*`, `auth/credential-store.*` | PARTIAL — direct Go `StreamOptions`, scoped env, and `oauth` package; no collection `CredentialStore` API |
| `models.*` (`createModels`, `createProvider`, builtin model helpers, cost) | PARTIAL — `registry.go`, `models_generated.go`, `simple_options.go`; no `Models` collection object |
| `images-models.*` | PARTIAL — `images/api.go`, `images/models_generated.go`; no image `Models` collection object |
| `session-resources.*` | DONE — `RegisterSessionResourceCleanup` and Codex cleanup integration |
| `api/lazy.*` | PARTIAL — not needed in Go; providers register via `init()` |

## Provider/API modules

| Upstream API/provider module | Status | Go implementation |
|---|---:|---|
| `api/openai-completions.*` | DONE | `inference/provider/openai/` |
| `api/openai-responses.*` | DONE | `inference/provider/openairesponses/` |
| `api/azure-openai-responses.*` | DONE | `inference/provider/openairesponses/`, `azure.go` |
| `api/openai-codex-responses.*` | DONE | `inference/provider/openaicodex/` |
| `api/anthropic-messages.*` | DONE | `inference/provider/anthropic/` |
| `api/bedrock-converse-stream.*` | DONE | `inference/provider/bedrock/` |
| `api/google-generative-ai.*` | DONE | `inference/provider/google/` |
| `api/google-vertex.*` | DONE | `inference/provider/google/` |
| `api/google-shared.*` | DONE | `inference/provider/google/`, `transform.go` |
| `api/mistral-conversations.*` | DONE | `inference/provider/mistral/` |
| `api/openrouter-images.*` | DONE | `images/openrouter/` |
| `api/simple-options.*` | DONE | `simple_options.go` |
| `api/transform-messages.*` | DONE | `transform.go` + provider-specific conversions |
| `api/openai-responses-shared.*` | DONE | `inference/provider/openairesponses/` + Codex shared-like conversion |
| `api/cloudflare.*`, `providers/cloudflare-auth.*` | DONE | `env.go`, `utils.go`, provider base URL resolution |
| `api/github-copilot-headers.*` | DONE | `utils.go` Copilot header helpers |
| `api/openai-prompt-cache.*` | DONE | `utils.go` prompt-cache key clamp |
| `providers/faux.*` | DONE | `inference/provider/faux/`; includes faux content/message helper constructors and registration/queue helpers |
| `providers/*.models.*`, `models.generated.*` | DONE | `models_generated.go`, `scripts/generate-models.go` |
| `providers/images/*.models.*`, `image-models.generated.*` | DONE | `images/models_generated.go` |

## Core event and content types

| Upstream type/event | Status | Go path |
|---|---:|---|
| `TextContent`, `ImageContent`, `ThinkingContent`, `ToolCall` | DONE | `types.go` `ContentBlock`, `ToolCall` |
| `UserMessage`, `AssistantMessage`, `ToolResultMessage`, `Message` | DONE | `types.go` `Message` |
| `Usage`, `CostBreakdown` | DONE | `types.go`, `simple_options.go` |
| `StartEvent`, `TextStart/Delta/End`, `ThinkingStart/Delta/End` | DONE | `events.go` |
| `ToolCallStart/Delta/End` | DONE | `events.go` |
| `DoneEvent`, `ErrorEvent` | DONE | `events.go` |
| `ProviderResponse`, `onPayload`, `onResponse` | DONE | `types.go`, `harness.go`, providers |
| `ImagesOptions`, `AssistantImages`, image output/content types | DONE | `images/api.go` |

## Tools/validation inventory

| Upstream tool surface | Status | Go path |
|---|---:|---|
| `Tool` schema definition | DONE | `types.go` `Tool` |
| `validateToolCall` | DONE | `context.go` |
| `validateToolArguments` | DONE | `context.go` |
| Provider tool conversion: OpenAI/Responses/Anthropic/Bedrock/Google/Mistral | DONE | respective provider directories |
| TypeBox schema constructors/helpers | MISSING | no Go TypeBox equivalent; Go accepts JSON Schema bytes/maps |

## Upstream tests inventory

No `*.test.ts` files are present in `/tmp/pi-ai-0.80.2/package`. Current Go test coverage includes provider fake-server tests, parser tests, model metadata parity tests, OAuth tests, image tests, race/staticcheck/logging/repro gates, but it is **not** a test-for-test port of upstream.

## Current closure target

Closed: `providers/faux.*` helper export gap. Added Go equivalents for upstream faux content/message constructors and tests for metadata/content semantics.
