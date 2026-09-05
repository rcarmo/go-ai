# go-ai

[![Go Reference](https://pkg.go.dev/badge/github.com/rcarmo/go-ai.svg)](https://pkg.go.dev/github.com/rcarmo/go-ai)
[![CI](https://github.com/rcarmo/go-ai/actions/workflows/ci.yml/badge.svg)](https://github.com/rcarmo/go-ai/actions/workflows/ci.yml)
[![SBOM: CycloneDX](https://img.shields.io/badge/SBOM-CycloneDX-6f42c1.svg)](https://github.com/rcarmo/go-ai/releases/download/upstream-v0.85.0/sbom.cdx.json)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

![go-ai](docs/icon-256.png)

A Go port of [`@earendil-works/pi-ai`](https://www.npmjs.com/package/@earendil-works/pi-ai) with the same broad shape: model discovery, streaming events, tool calls, OAuth helpers, and multi-provider request plumbing.

> **Experimental.** This module is still at `v0` and tracks upstream closely enough that release audits can move public details. The accepted v0.85.0 audit embeds 1336 text/chat models across 39 providers, 9 text/chat API protocols, and 50 image models.

## Documentation

* [Go Reference](https://pkg.go.dev/github.com/rcarmo/go-ai) has the published API surface.
* [Basic usage](docs/basic-usage.md), [model selection](docs/model-selection.md), [prompt/context handling](docs/prompts-and-context.md), [tool calling](docs/tool-calling.md), and [image handling](docs/image-handling.md) cover the common paths.
* [Harness helpers](docs/HARNESS.md) describe the higher-level agent/session utilities.
* [RELEASE.md](RELEASE.md) and [docs/v0850-release-ledger.md](docs/v0850-release-ledger.md) record the current upstream baseline, audit scope, and validation results.

## Features

* One `Stream`/`Complete` entry point over the registered provider implementation, with channel-based text, thinking, and tool-call events.
* A generated model registry for text/chat and image models, checked against the upstream v0.85.0 records rather than copied by hand.
* JSON-compatible message, context, tool, usage, diagnostic, and stream-option types for cross-language transcript hand-off.
* Tool calling with JSON Schema parameters, strict/constrained sampling helpers where providers expose them, and partial JSON parsing for streamed arguments.
* Reasoning/thinking support, including signed thinking replay, Anthropic managed effort markers, raw stop reasons, and provider-specific compatibility flags.
* OAuth helpers for GitHub Copilot, OpenAI Codex, Anthropic, Google Gemini CLI, Google Antigravity, Radius, Kimi Coding, and xAI.
* Image generation support through the `images` package and OpenRouter image provider registration.
* Local release gates for regenerated catalog drift, SBOM/security/license checks, logging quality, race tests, and reproducible test runs.

## Installation

```bash
go get github.com/rcarmo/go-ai
```

Import the provider package that matches the selected model's API. The provider packages register themselves through side effects, which keeps binaries explicit about which transports they include.

## Quick start

```go
package main

import (
	"context"
	"fmt"
	"log"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func main() {
	goai.RegisterBuiltinModels()

	model := goai.GetModel(goai.ProviderOpenAI, "gpt-4o-mini")
	if model == nil {
		log.Fatal("model not found")
	}

	ctx := &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []goai.Message{
			goai.UserMessage("What is 2+2?"),
		},
	}

	for event := range goai.Stream(context.Background(), model, ctx, nil) {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			fmt.Print(e.Delta)
		case *goai.DoneEvent:
			fmt.Printf("\n\nTokens: %d in, %d out ($%.6f)\n",
				e.Message.Usage.Input,
				e.Message.Usage.Output,
				e.Message.Usage.Cost.Total,
			)
		case *goai.ErrorEvent:
			log.Fatal(e.Err)
		}
	}
}
```

Set API keys in the environment, or pass them through `StreamOptions`/provider-specific OAuth runtime helpers. For OpenAI-compatible models, `OPENAI_API_KEY` is enough for the example above.

More complete examples live under `examples/basic`, `examples/streaming`, `examples/tools`, and `examples/copilot`.

## Package/source layout

```text
go-ai/
├── types.go                     # Message, Context, Tool, Model, Usage, StreamOptions
├── events.go                    # Stream event types
├── registry.go                  # Stream, Complete, provider and model registries
├── compat.go                    # Provider compatibility flags
├── retry.go                     # HTTP retry/proxy helper
├── models_generated.go          # generated text/chat model registry
├── inference/                   # text/chat facade plus provider implementations
│   └── provider/
│       ├── openai/
│       ├── openairesponses/
│       ├── openaicodex/
│       ├── anthropic/
│       ├── google/
│       ├── geminicli/
│       ├── mistral/
│       ├── bedrock/
│       ├── githubcopilot/
│       └── faux/
├── images/                      # image generation API and generated image model registry
│   └── openrouter/
├── oauth/                       # OAuth flows and runtime helpers
├── transports/                  # SSE and WebSocket primitives
├── internal/                    # private parsers/helpers
├── examples/                    # runnable examples
├── scripts/                     # generators and validation gates
└── docs/                        # usage docs and release ledgers
```

The root package remains the public `goai` package. `inference` and `images` provide narrower import trees for callers that want that split.

## Provider status

| Surface | Status |
|---|---|
| OpenAI Chat Completions and compatible APIs | Implemented |
| OpenAI Responses and Azure OpenAI Responses | Implemented |
| OpenAI Codex Responses, SSE and WebSocket paths | Implemented |
| Anthropic Messages, including managed effort/signed thinking replay | Implemented |
| Google Generative AI and Gemini CLI | Implemented |
| Mistral Conversations | Implemented |
| Amazon Bedrock ConverseStream | Implemented via AWS SDK |
| GitHub Copilot aggregate provider/OAuth runtime | Implemented |
| Cloudflare Workers AI / AI Gateway compatible routes | Implemented for HTTP dispatch; Workers `env.AI.fetch` is represented by a Go adapter surface |
| OpenRouter image generation | Implemented in `images/openrouter` |
| Faux provider | Implemented for deterministic tests |

The generated catalog also includes provider metadata for OpenRouter, xAI, Groq, Cerebras, Vercel AI Gateway, Fireworks, Together, Moonshot AI, Xiaomi/MiMo, Qwen Token Plan, ZAI, NVIDIA, Baseten, and related regional/provider-specific OpenAI- or Anthropic-compatible endpoints where upstream models define them.

## Known limitations/divergences

* This is a Go library, so JavaScript-only surfaces such as a Workers `env.AI.fetch` binding are adapted as Go interfaces and helpers rather than copied as runtime globals.
* Provider SDK behaviour is not always byte-for-byte identical. Where Go uses its own HTTP transport or an official Go SDK, the request/stream semantics are tested against deterministic fixtures and recorded in the release ledger.
* Live-provider smoke tests that require credentials stay out of the local gate. The repository favours deterministic wire, parser, replay, catalog, OAuth, and validation tests, with live-only gaps called out in `docs/v0850-142-test-manifest.md`.
* `CompactContext` is deliberately simple tail truncation. If you need semantic summaries or specialised transcript retention, add that in your agent layer.
* The module is still pre-`v1`; compatibility is best read against the release ledger for the upstream version being tracked.

## Compatibility/versioning

The current accepted runtime tracks upstream `@earendil-works/pi-ai` v0.85.0. Contexts, messages, events, tools, usage, and many provider compatibility fields are intended to serialize in the same shape as upstream so logs and agent state can move between Go and TypeScript when the supported surface overlaps.

Release audits update `RELEASE.md`, the generated catalogs, and the per-release manifests in `docs/`. Tags should be treated as upstream-aligned checkpoints rather than a promise that every upstream runtime surface exists unchanged in Go.

## Upstream and attribution

This project is a derivative port of [@earendil-works/pi-ai](https://www.npmjs.com/package/@earendil-works/pi-ai), part of the [earendil-works/pi](https://github.com/earendil-works/pi/tree/main/packages/ai) project, originally created by [Mario Zechner](https://mariozechner.at). The TypeScript API design, event protocol, provider implementations, model registry, and OAuth flows originate upstream. This port adapts them idiomatically for Go. All credit for the original design goes to Mario and the upstream contributors.

## Supply-chain metadata

The accepted v0.85.0 runtime (`90d17907b2ce26ffe5f46cd061edc8209e357bed`) has a validated CycloneDX SBOM published as durable, version-pinned release assets:

* [sbom.cdx.json](https://github.com/rcarmo/go-ai/releases/download/upstream-v0.85.0/sbom.cdx.json)
* [sbom.cdx.json.sha256](https://github.com/rcarmo/go-ai/releases/download/upstream-v0.85.0/sbom.cdx.json.sha256)

The SBOM is generated and checked by `make sbom-check`, then can be republished through the manual `publish-sbom-release.yml` workflow against that accepted runtime ref.

## License

MIT.
