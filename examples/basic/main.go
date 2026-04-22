// Basic example: non-streaming completion with OpenAI.
//
// Usage:
//
//	export OPENAI_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/openai" // register OpenAI provider
)

func main() {
	goai.SetLogger(goai.NewStderrLogger(goai.LogLevelInfo))
	goai.RegisterBuiltinModels()

	model := goai.GetModel(goai.ProviderOpenAI, "gpt-4o-mini")
	if model == nil {
		log.Fatal("model not found")
	}

	ctx := &goai.Context{
		SystemPrompt: "You are a helpful assistant. Be concise.",
		Messages:     []goai.Message{goai.UserMessage("What is the capital of France?")},
	}

	msg, err := goai.Complete(context.Background(), model, ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(goai.GetTextContent(msg))
	fmt.Printf("\nTokens: %d in, %d out — $%.6f\n",
		msg.Usage.Input, msg.Usage.Output, msg.Usage.Cost.Total)
}
