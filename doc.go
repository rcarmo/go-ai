// Package goai provides a unified LLM API with automatic model discovery,
// provider configuration, token/cost tracking, streaming, and tool calling.
//
// It is a feature-complete Go port of @mariozechner/pi-ai (TypeScript).
//
// # Quick Start
//
//	goai.RegisterBuiltinModels()
//	model := goai.GetModel(goai.ProviderOpenAI, "gpt-4o-mini")
//	ctx := &goai.Context{
//	    SystemPrompt: "You are a helpful assistant.",
//	    Messages: []goai.Message{goai.UserMessage("What time is it?")},
//	    Tools: []goai.Tool{timeTool},
//	}
//	result, err := goai.Complete(context.Background(), model, ctx, nil)
//
// # Streaming
//
//	events := goai.Stream(context.Background(), model, ctx, nil)
//	for event := range events {
//	    switch e := event.(type) {
//	    case *goai.TextDeltaEvent:
//	        fmt.Print(e.Delta)
//	    case *goai.DoneEvent:
//	        // final message in e.Message
//	    case *goai.ErrorEvent:
//	        log.Fatal(e.Err)
//	    }
//	}
//
// # Supported Providers
//
// OpenAI, Anthropic, Google, Google Vertex AI, Google Gemini CLI, Mistral,
// Amazon Bedrock, Azure OpenAI, OpenAI Codex, GitHub Copilot, xAI, Groq,
// Cerebras, OpenRouter, Vercel AI Gateway, MiniMax, and any OpenAI-compatible API.
//
// Retries are opt-in per request via StreamOptions.RetryConfig. By default,
// providers do not retry.
//
// # Documentation
//
// The rendered API reference is published by pkg.go.dev. For task-oriented
// guides and design notes, see the project documentation in the source
// repository:
//
//   - [Basic usage]
//   - [Model selection]
//   - [Prompt and context handling]
//   - [Tool calling]
//   - [Image handling]
//   - [Harness helpers]
//   - [Source repository]
//
// This module is currently v0 and should be treated as experimental until v1.
//
// [Basic usage]: https://github.com/rcarmo/go-ai/blob/main/docs/basic-usage.md
// [Model selection]: https://github.com/rcarmo/go-ai/blob/main/docs/model-selection.md
// [Prompt and context handling]: https://github.com/rcarmo/go-ai/blob/main/docs/prompts-and-context.md
// [Tool calling]: https://github.com/rcarmo/go-ai/blob/main/docs/tool-calling.md
// [Image handling]: https://github.com/rcarmo/go-ai/blob/main/docs/image-handling.md
// [Harness helpers]: https://github.com/rcarmo/go-ai/blob/main/docs/HARNESS.md
// [Source repository]: https://github.com/rcarmo/go-ai
package goai
