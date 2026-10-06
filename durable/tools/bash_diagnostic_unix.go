//go:build unix

package tools

import "github.com/rcarmo/go-ai/durable"

func reportBashSpill(api *durable.ToolAPI, result *durable.ToolResult, path string) error {
	diagnostic := durable.ToolDiagnostic{Severity: "info", Code: "full_output", Message: "Full output: " + path}
	if api != nil {
		return api.Diagnostic(diagnostic)
	}
	result.Diagnostics = append(result.Diagnostics, diagnostic)
	return nil
}
