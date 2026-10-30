package openai

import goai "github.com/rcarmo/go-ai"

// Native grammar input is still a string at the provider boundary. Accept the
// isolated-unit representation only when the tool block owns native arguments.
// Replay retains the provider's existing surrogate-sanitization policy.
func customGrammarInput(block goai.ContentBlock, property string) (string, bool) {
	switch value := block.Arguments[property].(type) {
	case string:
		return value, true
	case goai.UTF16String:
		if block.HasStreamingArguments() {
			return value.WTF8(), true
		}
	}
	return "", false
}
