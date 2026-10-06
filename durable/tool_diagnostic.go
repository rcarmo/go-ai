package durable

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ToolDiagnostic is the pinned model-visible remark shape. Application details
// remain separate, and error severity alone does not set ToolResult.IsError.
type ToolDiagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
}

func validateToolDiagnostics(diagnostics []ToolDiagnostic, l Limits) error {
	if len(diagnostics) > l.MaxMembers {
		return reject("tool diagnostic limit")
	}
	for _, d := range diagnostics {
		if d.Severity != "info" && d.Severity != "warn" && d.Severity != "error" || !utf8.ValidString(d.Message) || !utf8.ValidString(d.Code) || len(d.Message) > l.MaxStringBytes || len(d.Code) > l.MaxStringBytes {
			return reject("invalid tool diagnostic")
		}
	}
	_, err := encodeBounded(diagnostics, l, l.MaxDocumentBytes)
	return err
}
func toolErrorMessage(name, code string) string {
	switch code {
	case "aborted":
		return "Tool " + name + " was aborted"
	case "interrupted":
		return "Tool " + name + " was interrupted and may have partially run"
	case "tool_unavailable":
		return "Tool " + name + " is not available"
	case "blocked":
		return "Tool call blocked"
	case "invalid_arguments":
		return "Invalid arguments for tool " + name
	default:
		return code
	}
}

func truncatedToolOutput(bytes, lines uint64, retain string) ToolDiagnostic {
	kept := ""
	if retain != "" {
		position := "beginning"
		if retain == "tail" {
			position = "end"
		}
		kept = " to its " + position
	}
	return ToolDiagnostic{Severity: "warn", Code: "truncated", Message: fmt.Sprintf("Output truncated%s: %d lines, %d bytes dropped", kept, lines, bytes)}
}
func renderToolDiagnostics(diagnostics []ToolDiagnostic) string {
	var text strings.Builder
	text.WriteString("<harness>\n")
	for i, d := range diagnostics {
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteByte('[')
		text.WriteString(d.Severity)
		text.WriteString("] ")
		text.WriteString(d.Message)
	}
	text.WriteString("\n</harness>")
	return text.String()
}
