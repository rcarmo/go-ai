// Basic example: non-streaming completion with OpenAI Responses.
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
	"os"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/openairesponses" // register OpenAI Responses provider
)

func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("set OPENAI_API_KEY to run this example")
	}

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
