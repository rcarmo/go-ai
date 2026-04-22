// Streaming example: real-time text output with Anthropic.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/anthropic" // register Anthropic provider
)

func main() {
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("ANTHROPIC_OAUTH_TOKEN") == "" {
		log.Fatal("set ANTHROPIC_API_KEY or ANTHROPIC_OAUTH_TOKEN to run this example")
	}

	goai.RegisterBuiltinModels()

	model := goai.GetModel(goai.ProviderAnthropic, "claude-sonnet-4-20250514")
	if model == nil {
		log.Fatal("model not found")
	}

	ctx := &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []goai.Message{goai.UserMessage("Explain Go channels in 3 sentences.")},
	}

	events := goai.Stream(context.Background(), model, ctx, nil)
	for event := range events {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			fmt.Print(e.Delta)
		case *goai.ThinkingDeltaEvent:
			// Thinking content (if reasoning model)
			fmt.Fprint(os.Stderr, e.Delta)
		case *goai.DoneEvent:
			fmt.Printf("\n\nTokens: %d in, %d out — $%.6f\n",
				e.Message.Usage.Input, e.Message.Usage.Output, e.Message.Usage.Cost.Total)
		case *goai.ErrorEvent:
			log.Fatal(e.Err)
		}
	}
}
