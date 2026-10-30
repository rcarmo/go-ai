package goai

import (
	"encoding/json"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// UTF16String is an immutable lossless string preview for isolated JS code units.
// Ordinary Unicode values remain Go strings. Use CodeUnits for exceptional values.
type UTF16String = jsonparse.UTF16String

// StreamingArgumentsJSON explicitly represents UTF-16 object keys. It does not
// relax the persistence validator's numeric, resource or authority checks.
type StreamingArgumentsJSON = jsonparse.ArgumentsObject

// SetStreamingArguments installs parser-produced JSON values and a diagnostic.
// Raw parser state is never attached to the tool call.
func (t *ToolCall) SetStreamingArguments(args map[string]any, diagnostic string) {
	if args == nil {
		args = map[string]any{}
	}
	t.Arguments = args
	t.ArgumentsError = diagnostic
	t.streamingArguments = true
}

// SetStreamingArguments opts a tool-call block into lossless argument encoding.
func (b *ContentBlock) SetStreamingArguments(args map[string]any, diagnostic string) {
	if args == nil {
		args = map[string]any{}
	}
	b.Arguments = args
	b.ArgumentsError = diagnostic
	b.streamingArguments = true
}
func (t ToolCall) HasStreamingArguments() bool     { return t.streamingArguments }
func (b ContentBlock) HasStreamingArguments() bool { return b.streamingArguments }

func streamingArgumentsFields(args map[string]any, diagnostic string, native bool) (any, any, error) {
	var arguments any = args
	var message any
	if diagnostic != "" {
		message = diagnostic
	}
	if native {
		arguments = StreamingArgumentsJSON(args)
		if diagnostic != "" {
			var err error
			message, err = jsonparse.UTF16Text(diagnostic)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	return arguments, message, nil
}
func optionalString(value string, present bool) *string {
	if value != "" || present {
		return &value
	}
	return nil
}
func optionalBool(value bool, present bool) *bool {
	if value || present {
		return &value
	}
	return nil
}

type toolCallJSONAlias ToolCall

func (t ToolCall) MarshalJSON() ([]byte, error) {
	args, message, err := streamingArgumentsFields(t.Arguments, t.ArgumentsError, t.streamingArguments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		toolCallJSONAlias
		Arguments        any     `json:"arguments"`
		ArgumentsError   any     `json:"argumentsError,omitempty"`
		ThoughtSignature *string `json:"thoughtSignature,omitempty"`
		Namespace        *string `json:"namespace,omitempty"`
	}{toolCallJSONAlias(t), args, message, optionalString(t.ThoughtSignature, t.ThoughtSignaturePresent), optionalString(t.Namespace, t.NamespacePresent)})
}

type contentBlockJSONAlias ContentBlock

func (b ContentBlock) MarshalJSON() ([]byte, error) {
	args, message, err := streamingArgumentsFields(b.Arguments, b.ArgumentsError, b.streamingArguments)
	if err != nil {
		return nil, err
	}
	if !b.streamingArguments && len(b.Arguments) == 0 {
		args = nil
	}
	return json.Marshal(struct {
		contentBlockJSONAlias
		Arguments         any     `json:"arguments,omitempty"`
		ArgumentsError    any     `json:"argumentsError,omitempty"`
		TextSignature     *string `json:"textSignature,omitempty"`
		ThinkingSignature *string `json:"thinkingSignature,omitempty"`
		Redacted          *bool   `json:"redacted,omitempty"`
		ThoughtSignature  *string `json:"thoughtSignature,omitempty"`
		Namespace         *string `json:"namespace,omitempty"`
	}{contentBlockJSONAlias(b), args, message, optionalString(b.TextSignature, b.TextSignaturePresent), optionalString(b.ThinkingSignature, b.ThinkingSignaturePresent), optionalBool(b.Redacted, b.RedactedPresent), optionalString(b.ThoughtSignature, b.ThoughtSignaturePresent), optionalString(b.Namespace, b.NamespacePresent)})
}
