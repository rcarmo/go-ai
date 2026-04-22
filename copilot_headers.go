// GitHub Copilot header generation.
package goai

// CopilotHeaders returns the standard headers required for GitHub Copilot API calls.
func CopilotHeaders() map[string]string {
	return map[string]string{
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"Editor-Version":        "vscode/1.107.0",
		"Editor-Plugin-Version": "copilot-chat/0.35.0",
		"Copilot-Integration-Id": "vscode-chat",
	}
}

// CopilotHeadersWithIntent returns Copilot headers plus an intent header.
func CopilotHeadersWithIntent(intent string) map[string]string {
	h := CopilotHeaders()
	if intent != "" {
		h["openai-intent"] = intent
	}
	return h
}
