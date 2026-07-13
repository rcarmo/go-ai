// Package githubcopilot registers the end-to-end GitHub Copilot integration.
//
// Import this package for side effects when using GitHub Copilot models. It
// registers the OAuth provider plus the OpenAI-compatible, OpenAI Responses,
// and Anthropic transports used by Copilot's model catalog.
package githubcopilot

import (
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
	_ "github.com/rcarmo/go-ai/oauth"
)
