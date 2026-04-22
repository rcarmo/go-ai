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
package goai
