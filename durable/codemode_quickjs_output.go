package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// formatQuickJSText preserves upstream text/console grouping. Image file
// storage and full-output spill require a separate environment boundary.
func formatQuickJSText(result quickjs.Result) (string, error) {
	items := append([]quickjs.OutputItem(nil), result.Output...)
	if result.OK && result.Value != nil {
		if !json.Valid(result.Value) {
			return "", fmt.Errorf("invalid QuickJS return value")
		}
		if len(result.Value) > 0 && result.Value[0] == '"' {
			var text string
			if err := json.Unmarshal(result.Value, &text); err != nil {
				return "", err
			}
			items = append(items, quickjs.OutputItem{Type: "text", Text: text})
		} else {
			// Indent the guest's JSON directly: decoding into maps/floats would
			// change insertion order, numeric spelling and precision.
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, result.Value, "", "  "); err != nil {
				return "", err
			}
			items = append(items, quickjs.OutputItem{Type: "text", Text: pretty.String()})
		}
	}
	if !result.OK && result.Error != nil {
		items = append(items, quickjs.OutputItem{Type: "text", Text: "Script error:\n" + formatQuickJSFailure(result)})
	}
	texts := make([]string, 0, len(result.Output)+2)
	console := []string{}
	count := 0
	for _, item := range items {
		if item.Type == "text" {
			count++
		}
	}
	index := 0
	for _, item := range items {
		switch item.Type {
		case "text":
			index++
			text := item.Text
			if count > 1 {
				text = fmt.Sprintf("==> text %d/%d <==\n%s", index, count, text)
			}
			texts = append(texts, text)
		case "console":
			console = append(console, item.Text)
		case "image":
			// Image blocks are retained separately in the durable result.
			// Environment-backed file labels are not available at this layer.
		default:
			return "", fmt.Errorf("invalid QuickJS output kind")
		}
	}
	if len(console) > 0 {
		texts = append(texts, "<console_output>\n"+strings.Join(console, "\n")+"\n</console_output>")
	}
	return strings.Join(texts, "\n"), nil
}

func formatQuickJSFailure(result quickjs.Result) string {
	err := result.Error
	head := "Script sandbox failed: " + err.Message
	switch err.Kind {
	case quickjs.ErrorScript:
		head = err.Stack
		if head == "" {
			name := err.Name
			if name == "" {
				name = "Error"
			}
			head = name + ": " + err.Message
		}
	case quickjs.ErrorTimeout:
		head = "Script timed out: " + err.Message
	case quickjs.ErrorAborted:
		head = "Script aborted: " + err.Message
	}
	summary := "No tool calls were made."
	if len(result.Calls) > 0 {
		calls := make([]string, 0, len(result.Calls))
		for _, call := range result.Calls {
			calls = append(calls, call.Name+" ("+call.Status+")")
		}
		summary = "Tool calls made before the failure (they are not undone): " + strings.Join(calls, ", ")
	}
	return head + "\n\n" + summary
}
