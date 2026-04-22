# go-ai

A Go port of [@mariozechner/pi-ai](https://www.npmjs.com/package/@mariozechner/pi-ai) — unified LLM API with automatic model discovery, streaming, tool calling, and multi-provider support.

> **⚠️ Experimental.** This library tracks the TypeScript original and is under
> active development. The API surface may change. Use in production at your own risk.

## Why

I needed a Go library for talking to LLMs that was at least as good as
[pi-ai](https://www.npmjs.com/package/@mariozechner/pi-ai) — unified
streaming, tool calling, multi-provider, proper cost tracking — and couldn't
find anything I liked. Everything was either OpenAI-only, didn't stream
properly, or required pulling in half the internet as dependencies.

So I ported pi-ai to Go. Same types (JSON-serialization-compatible), same
event protocol, same provider coverage. If you know pi-ai, you know this.

## Credits

This project is a derivative work of
[**@mariozechner/pi-ai**](https://github.com/nicobailon/pi-mcp-adapter) by
[Mario Zechner](https://github.com/nicobailon). The type system, event
protocol, provider implementations, model registry, and OAuth flows are all
ported from his TypeScript library. All credit for the design goes to him.

## Features

- **Unified API** — same `Stream()`/`Complete()` interface across all providers
- **Streaming** — channel-based event stream with text, thinking, and tool call deltas
- **Tool calling** — typed tool definitions with JSON Schema parameters
- **Multi-provider** — OpenAI, Anthropic, Google, Mistral, Bedrock, and OpenAI-compatible APIs
- **Context serialization** — JSON-compatible with pi-ai for cross-language hand-off
- **Cost tracking** — per-request token counts and USD cost breakdown
- **Thinking/reasoning** — unified thinking level across providers

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    goai "github.com/rcarmo/go-ai"
    _ "github.com/rcarmo/go-ai/provider/openai"     // register OpenAI
    _ "github.com/rcarmo/go-ai/provider/anthropic"   // register Anthropic
)

func main() {
    // Register a model (or use auto-discovery)
    goai.RegisterModel(&goai.Model{
        ID:            "gpt-4o-mini",
        Name:          "GPT-4o Mini",
        Api:           goai.ApiOpenAICompletions,
        Provider:      goai.ProviderOpenAI,
        BaseURL:       "https://api.openai.com/v1",
        Input:         []string{"text", "image"},
        ContextWindow: 128000,
        MaxTokens:     16384,
        Cost:          goai.ModelCost{Input: 0.15, Output: 0.60},
    })

    model := goai.GetModel(goai.ProviderOpenAI, "gpt-4o-mini")

    ctx := &goai.Context{
        SystemPrompt: "You are a helpful assistant.",
        Messages: []goai.Message{
            goai.UserMessage("What is 2+2?"),
        },
    }

    // Streaming
    events := goai.Stream(context.Background(), model, ctx, nil)
    for event := range events {
        switch e := event.(type) {
        case *goai.TextDeltaEvent:
            fmt.Print(e.Delta)
        case *goai.DoneEvent:
            fmt.Printf("\n\nTokens: %d in, %d out ($%.6f)\n",
                e.Message.Usage.Input, e.Message.Usage.Output, e.Message.Usage.Cost.Total)
        case *goai.ErrorEvent:
            log.Fatal(e.Err)
        }
    }
}
```

## Architecture

```
go-ai/
├── doc.go              # Package documentation
├── types.go            # Core types (Message, Context, Tool, Model, Usage, etc.)
├── events.go           # Stream event types (TextDelta, ToolCallStart, Done, etc.)
├── registry.go         # Provider + model registry, Stream(), Complete()
├── env.go              # API key resolution from environment
├── compat.go           # OpenAI Completions compat flags
├── overflow.go         # Context overflow detection
├── validation.go       # Tool call argument validation
├── transform.go        # Cross-provider message normalization
├── retry.go            # HTTP retry with backoff
├── simple_options.go   # ThinkingLevel mapping, cost calculation
├── sanitize.go         # Unicode surrogate removal
├── logger.go           # Centralized pluggable logging
├── harness.go          # Agent harness helpers (context, compaction, hooks)
├── hooks.go            # Provider hook invocation helpers
├── hash.go             # Short deterministic hash
├── copilot_headers.go  # GitHub Copilot header generation
├── models_generated.go # Auto-generated model registry (865 models)
├── provider/
│   ├── openai/         # OpenAI Chat Completions (+ compatible APIs)
│   ├── anthropic/      # Anthropic Messages API
│   ├── openairesponses/ # OpenAI Responses API (+ Azure)
│   ├── openaicodex/    # OpenAI Codex Responses (WebSocket + SSE)
│   ├── google/         # Google Generative AI + Vertex AI
│   ├── geminicli/      # Google Gemini CLI (Cloud Code Assist)
│   ├── mistral/        # Mistral Conversations API
│   ├── bedrock/        # Amazon Bedrock ConverseStream
│   └── faux/           # Test double provider
├── oauth/
│   ├── oauth.go            # OAuth framework + PKCE
│   ├── github_copilot.go   # GitHub Copilot device flow
│   ├── anthropic.go        # Anthropic auth code + PKCE
│   ├── google_gemini_cli.go # Google Gemini CLI auth code + PKCE
│   ├── google_antigravity.go # Antigravity (same flow as Gemini CLI)
│   └── openai_codex.go     # OpenAI Codex device flow
├── internal/
│   ├── eventstream/    # SSE parser
│   └── jsonparse/      # Partial JSON parser for streaming tool args
└── scripts/
    ├── generate-models.ts  # Model registry code generator
    └── check-logging.sh    # Logging quality gate
```

## Provider status

| Provider | API | Status |
|---|---|---|
| OpenAI | `openai-completions` | ✅ Implemented |
| Anthropic | `anthropic-messages` | ✅ Implemented |
| OpenAI Responses | `openai-responses` | ✅ Implemented |
| Azure OpenAI | `azure-openai-responses` | ✅ Implemented |
| Google Generative AI | `google-generative-ai` | ✅ Implemented |
| Google Vertex AI | `google-vertex` | ✅ Implemented |
| Mistral | `mistral-conversations` | ✅ Implemented |
| Amazon Bedrock | `bedrock-converse-stream` | ✅ Implemented |
| Google Gemini CLI | `google-gemini-cli` | ✅ Implemented |
| OpenAI Codex | `openai-codex-responses` | ✅ Implemented |
| Any OpenAI-compatible | `openai-completions` | ✅ Via OpenAI provider |

## OAuth

| Provider | Status |
|---|---|
| GitHub Copilot (device flow) | ✅ Implemented |
| Google Gemini CLI (auth code + PKCE) | ✅ Implemented |
| Anthropic (auth code + PKCE) | ✅ Implemented |
| OpenAI Codex (device flow) | ✅ Implemented |
| Antigravity | 🔲 Planned |

## Compatibility with pi-ai

Types are designed to be JSON-serialization-compatible with pi-ai's TypeScript types. A `Context` serialized in Go can be deserialized in TypeScript and vice versa, enabling:

- Cross-language agent hand-off
- Shared conversation logs
- Mixed Go/TypeScript tool pipelines

## Environment variables

API keys are resolved in order: explicit option → model config → environment variable.

| Provider | Environment Variable |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| Google | `GOOGLE_API_KEY` |
| Mistral | `MISTRAL_API_KEY` |
| xAI | `XAI_API_KEY` |
| Groq | `GROQ_API_KEY` |

## License

MIT
