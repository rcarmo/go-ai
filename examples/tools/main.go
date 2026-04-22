// Tool calling example: agent loop with function execution.
//
// Usage:
//
//	export OPENAI_API_KEY=sk-...
//	go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/openairesponses"
)

var tools = []goai.Tool{
	{
		Name:        "get_time",
		Description: "Get the current time in a timezone",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"timezone": {
					"type": "string",
					"description": "IANA timezone (e.g. America/New_York)"
				}
			},
			"required": ["timezone"]
		}`),
	},
	{
		Name:        "calculate",
		Description: "Evaluate a math expression",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "Math expression to evaluate"
				}
			},
			"required": ["expression"]
		}`),
	},
}

func executeTool(tc goai.ToolCall) (string, bool) {
	switch tc.Name {
	case "get_time":
		tz, _ := tc.Arguments["timezone"].(string)
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return fmt.Sprintf("Error: unknown timezone %q", tz), true
		}
		return time.Now().In(loc).Format(time.RFC3339), false

	case "calculate":
		expr, _ := tc.Arguments["expression"].(string)
		return fmt.Sprintf("Result of %s = (not implemented)", expr), false

	default:
		return fmt.Sprintf("Unknown tool: %s", tc.Name), true
	}
}

func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatal("set OPENAI_API_KEY to run this example")
	}

	goai.SetLogger(goai.NewStderrLogger(goai.LogLevelDebug))
	goai.RegisterBuiltinModels()

	model := goai.GetModel(goai.ProviderOpenAI, "gpt-4o-mini")
	if model == nil {
		log.Fatal("model not found")
	}

	convCtx := &goai.Context{
		SystemPrompt: "You are a helpful assistant with access to tools.",
		Messages:     []goai.Message{goai.UserMessage("What time is it in Tokyo and London?")},
		Tools:        tools,
	}

	// Agent loop
	for turn := 0; turn < 10; turn++ {
		fmt.Printf("--- Turn %d ---\n", turn+1)

		msg, err := goai.Complete(context.Background(), model, convCtx, nil)
		if err != nil {
			log.Fatal(err)
		}

		goai.AppendAssistantMessage(convCtx, msg)

		if !goai.NeedsToolExecution(msg) {
			fmt.Printf("Assistant: %s\n", goai.GetTextContent(msg))
			break
		}

		// Execute tool calls
		for _, tc := range goai.GetToolCalls(msg) {
			fmt.Printf("Tool: %s(%v)\n", tc.Name, tc.Arguments)
			result, isErr := executeTool(tc)
			fmt.Printf("Result: %s\n", result)
			goai.AppendToolResult(convCtx, tc.ID, tc.Name, result, isErr)
		}
	}
}
