package openaicodex

import (
	"encoding/json"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Only function-call argument fields enter this domain. SSE and WebSocket
// processors share reconciliation without changing ordinary text decoding.
type codexArgumentStream struct {
	stream jsonparse.ToolCallArgumentsStream
}

func (s *codexArgumentStream) append(raw []byte, block *goai.ContentBlock) (string, error) {
	var wire struct {
		Delta jsonparse.ArgumentText `json:"delta"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", err
	}
	text := string(wire.Delta)
	block.SetStreamingArguments(s.stream.Append(text), "")
	return text, nil
}
func (s *codexArgumentStream) replace(raw []byte, block *goai.ContentBlock) (string, error) {
	var wire struct {
		Arguments jsonparse.ArgumentText `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", err
	}
	result := s.stream.Replace(string(wire.Arguments))
	block.SetStreamingArguments(result.Arguments, "")
	switch text := result.Delta.(type) {
	case string:
		return text, nil
	case goai.UTF16String:
		return text.WTF8(), nil
	default:
		return "", nil
	}
}
func (s *codexArgumentStream) finish(raw []byte, block *goai.ContentBlock) (goai.ToolCall, error) {
	var wire struct {
		Arguments jsonparse.ArgumentText `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return goai.ToolCall{}, err
	}
	if wire.Arguments != "" {
		s.stream.Replace(string(wire.Arguments))
	}
	args, diagnostic := s.stream.Finish()
	block.SetStreamingArguments(args, diagnostic)
	call := goai.ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Namespace: block.Namespace, NamespacePresent: block.NamespacePresent}
	call.SetStreamingArguments(args, diagnostic)
	return call, nil
}
