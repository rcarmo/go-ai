package goai

import (
	"encoding/json"
	"fmt"
)

// AssistantMessageFrame is a compact, immutable stream frame that can be used
// to reconstruct an assistant message without retaining provider scratch fields.
type AssistantMessageFrame struct {
	Type              string                 `json:"type"`
	Partial           *Message               `json:"partial,omitempty"`
	ContentIndex      int                    `json:"contentIndex,omitempty"`
	Content           *ContentBlock          `json:"content,omitempty"`
	Delta             string                 `json:"delta,omitempty"`
	ToolCall          *ToolCall              `json:"toolCall,omitempty"`
	JSON              string                 `json:"json,omitempty"`
	ID                string                 `json:"id,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Arguments         map[string]interface{} `json:"arguments,omitempty"`
	Text              string                 `json:"-"`
	TextSignature     string                 `json:"textSignature,omitempty"`
	ThinkingSignature string                 `json:"thinkingSignature,omitempty"`
	Redacted          *bool                  `json:"redacted,omitempty"`
	ThoughtSignature  string                 `json:"thoughtSignature,omitempty"`
	Namespace         string                 `json:"namespace,omitempty"`
}

// AssistantMessageFrameEncoder converts mutable stream events into compact
// frames. Terminal done/error events are intentionally not framed; settlement is
// handled by the stream consumer.
type AssistantMessageFrameEncoder struct {
	started bool
	text    map[int]string
	think   map[int]string
	tool    map[int]string
}

func NewAssistantMessageFrameEncoder() *AssistantMessageFrameEncoder {
	return &AssistantMessageFrameEncoder{text: map[int]string{}, think: map[int]string{}, tool: map[int]string{}}
}

func (e *AssistantMessageFrameEncoder) Encode(event Event) (*AssistantMessageFrame, error) {
	if e.text == nil {
		e.text = map[int]string{}
		e.think = map[int]string{}
		e.tool = map[int]string{}
	}
	switch ev := event.(type) {
	case *StartEvent:
		e.started = true
		return &AssistantMessageFrame{Type: "start", Partial: cloneFrameMessage(ev.Partial, false)}, nil
	case *DoneEvent:
		if !e.started {
			return nil, fmt.Errorf("done event appears before start")
		}
		return nil, nil
	case *ErrorEvent:
		return nil, nil
	case *TextStartEvent:
		if err := e.requireStarted("text_start"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "text")
		if err != nil {
			return nil, err
		}
		e.text[ev.ContentIndex] = block.Text
		return &AssistantMessageFrame{Type: "text_start", ContentIndex: ev.ContentIndex, Content: block}, nil
	case *TextDeltaEvent:
		if err := e.requireStarted("text_delta"); err != nil {
			return nil, err
		}
		delta := trimCoveredDelta(e.text[ev.ContentIndex], ev.Delta, frameText(ev.Partial, ev.ContentIndex))
		if delta == "" {
			return nil, nil
		}
		e.text[ev.ContentIndex] += delta
		return &AssistantMessageFrame{Type: "text_delta", ContentIndex: ev.ContentIndex, Delta: delta}, nil
	case *TextEndEvent:
		if err := e.requireStarted("text_end"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "text")
		if err == nil {
			e.text[ev.ContentIndex] = ev.Content
			return &AssistantMessageFrame{Type: "text_end", ContentIndex: ev.ContentIndex, Delta: "", Text: ev.Content, TextSignature: block.TextSignature}, nil
		}
		return &AssistantMessageFrame{Type: "text_end", ContentIndex: ev.ContentIndex, Text: ev.Content}, nil
	case *ThinkingStartEvent:
		if err := e.requireStarted("thinking_start"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "thinking")
		if err != nil {
			return nil, err
		}
		e.think[ev.ContentIndex] = block.Thinking
		return &AssistantMessageFrame{Type: "thinking_start", ContentIndex: ev.ContentIndex, Content: block}, nil
	case *ThinkingDeltaEvent:
		if err := e.requireStarted("thinking_delta"); err != nil {
			return nil, err
		}
		delta := trimCoveredDelta(e.think[ev.ContentIndex], ev.Delta, frameThinking(ev.Partial, ev.ContentIndex))
		if delta == "" {
			return nil, nil
		}
		e.think[ev.ContentIndex] += delta
		return &AssistantMessageFrame{Type: "thinking_delta", ContentIndex: ev.ContentIndex, Delta: delta}, nil
	case *ThinkingEndEvent:
		if err := e.requireStarted("thinking_end"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "thinking")
		redacted := false
		sig := ""
		if err == nil {
			redacted = block.Redacted
			sig = block.ThinkingSignature
		}
		e.think[ev.ContentIndex] = ev.Content
		return &AssistantMessageFrame{Type: "thinking_end", ContentIndex: ev.ContentIndex, Text: ev.Content, ThinkingSignature: sig, Redacted: &redacted}, nil
	case *ToolCallStartEvent:
		if err := e.requireStarted("toolcall_start"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "toolCall")
		if err != nil {
			return nil, err
		}
		call := blockToToolCall(block)
		jsonArgs := compactJSON(call.Arguments)
		e.tool[ev.ContentIndex] = jsonArgs
		return &AssistantMessageFrame{Type: "toolcall_start", ContentIndex: ev.ContentIndex, ToolCall: &call}, nil
	case *ToolCallDeltaEvent:
		if err := e.requireStarted("toolcall_delta"); err != nil {
			return nil, err
		}
		live := frameToolJSON(ev.Partial, ev.ContentIndex)
		prev := e.tool[ev.ContentIndex]
		if live != "" && hasPrefixJSON(live, prev) && len(live) > len(prev)+len(ev.Delta) {
			e.tool[ev.ContentIndex] = live
			return &AssistantMessageFrame{Type: "toolcall_checkpoint", ContentIndex: ev.ContentIndex, JSON: live}, nil
		}
		delta := ev.Delta
		if len(prev) > 0 && len(delta) > 0 && len(prev) >= len(delta) && prev[len(prev)-len(delta):] == delta {
			return nil, nil
		}
		e.tool[ev.ContentIndex] += delta
		return &AssistantMessageFrame{Type: "toolcall_delta", ContentIndex: ev.ContentIndex, Delta: delta}, nil
	case *ToolCallEndEvent:
		if err := e.requireStarted("toolcall_end"); err != nil {
			return nil, err
		}
		call := ev.ToolCall
		return &AssistantMessageFrame{Type: "toolcall_end", ContentIndex: ev.ContentIndex, ID: call.ID, Name: call.Name, Arguments: cloneFrameMap(call.Arguments), ThoughtSignature: call.ThoughtSignature, Namespace: call.Namespace}, nil
	default:
		return nil, nil
	}
}

func (e *AssistantMessageFrameEncoder) requireStarted(kind string) error {
	if !e.started {
		return fmt.Errorf("%s event appears before start", kind)
	}
	return nil
}

// ReduceAssistantMessageFrames reconstructs the current assistant message from frames.
func ReduceAssistantMessageFrames(frames []AssistantMessageFrame) (*Message, error) {
	var out *Message
	ended := map[int]bool{}
	toolJSON := map[int]string{}
	for _, frame := range frames {
		if frame.Type != "start" && out == nil {
			return nil, fmt.Errorf("%s frame appears before the start frame", frame.Type)
		}
		switch frame.Type {
		case "start":
			out = cloneFrameMessage(frame.Partial, true)
		case "text_start":
			if err := startFrameBlock(out, frame.ContentIndex, frame.Content, "text"); err != nil {
				return nil, err
			}
		case "text_delta":
			if err := appendFrameText(out, frame.ContentIndex, frame.Delta, ended); err != nil {
				return nil, err
			}
		case "text_end":
			if err := endFrame(out, frame.ContentIndex, "text", ended); err != nil {
				return nil, err
			}
			out.Content[frame.ContentIndex].Text = frame.Text
			out.Content[frame.ContentIndex].TextSignature = frame.TextSignature
		case "thinking_start":
			if err := startFrameBlock(out, frame.ContentIndex, frame.Content, "thinking"); err != nil {
				return nil, err
			}
		case "thinking_delta":
			if err := appendFrameThinking(out, frame.ContentIndex, frame.Delta, ended); err != nil {
				return nil, err
			}
		case "thinking_end":
			if err := endFrame(out, frame.ContentIndex, "thinking", ended); err != nil {
				return nil, err
			}
			out.Content[frame.ContentIndex].Thinking = frame.Text
			out.Content[frame.ContentIndex].ThinkingSignature = frame.ThinkingSignature
			if frame.Redacted != nil {
				out.Content[frame.ContentIndex].Redacted = *frame.Redacted
			}
		case "toolcall_start":
			block := toolCallToBlock(frame.ToolCall)
			if err := startFrameBlock(out, frame.ContentIndex, &block, "toolCall"); err != nil {
				return nil, err
			}
		case "toolcall_delta":
			if err := appendFrameTool(out, frame.ContentIndex, frame.Delta, toolJSON, ended); err != nil {
				return nil, err
			}
		case "toolcall_checkpoint":
			if err := setFrameToolJSON(out, frame.ContentIndex, frame.JSON, toolJSON, ended); err != nil {
				return nil, err
			}
		case "toolcall_end":
			if err := endFrame(out, frame.ContentIndex, "toolCall", ended); err != nil {
				return nil, err
			}
			out.Content[frame.ContentIndex].ID = frame.ID
			out.Content[frame.ContentIndex].Name = frame.Name
			out.Content[frame.ContentIndex].Arguments = cloneFrameMap(frame.Arguments)
			out.Content[frame.ContentIndex].ThoughtSignature = frame.ThoughtSignature
			out.Content[frame.ContentIndex].Namespace = frame.Namespace
		}
	}
	return out, nil
}

func cloneFrameMessage(m *Message, includeContent bool) *Message {
	if m == nil {
		return nil
	}
	out := *m
	if m.Usage != nil {
		u := *m.Usage
		out.Usage = &u
	}
	out.Diagnostics = append([]AssistantMessageDiagnostic(nil), m.Diagnostics...)
	if includeContent {
		out.Content = cloneBlocks(m.Content)
	} else {
		out.Content = nil
	}
	return &out
}

func cloneBlocks(in []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, len(in))
	for i := range in {
		out[i] = cloneBlock(in[i])
	}
	return out
}

func cloneBlock(in ContentBlock) ContentBlock {
	out := in
	out.Arguments = cloneFrameMap(in.Arguments)
	return out
}

func cloneFrameMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	data, _ := json.Marshal(in)
	var out map[string]interface{}
	_ = json.Unmarshal(data, &out)
	return out
}

func frameBlock(m *Message, idx int, kind string) (*ContentBlock, error) {
	if m == nil || idx < 0 || idx >= len(m.Content) {
		return nil, fmt.Errorf("%s event points outside content", kind)
	}
	block := cloneBlock(m.Content[idx])
	if block.Type != kind {
		return nil, fmt.Errorf("%s event points to %s block", kind, block.Type)
	}
	return &block, nil
}

func blockToToolCall(block *ContentBlock) ToolCall {
	if block == nil {
		return ToolCall{Type: "toolCall", Arguments: map[string]interface{}{}}
	}
	return ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: cloneFrameMap(block.Arguments), ThoughtSignature: block.ThoughtSignature, Namespace: block.Namespace}
}

func toolCallToBlock(call *ToolCall) ContentBlock {
	if call == nil {
		return ContentBlock{Type: "toolCall", Arguments: map[string]interface{}{}}
	}
	return ContentBlock{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: cloneFrameMap(call.Arguments), ThoughtSignature: call.ThoughtSignature, Namespace: call.Namespace}
}

func startFrameBlock(out *Message, idx int, block *ContentBlock, kind string) error {
	if idx != len(out.Content) {
		return fmt.Errorf("%s start would leave a gap", kind)
	}
	if block == nil {
		block = &ContentBlock{Type: kind}
	}
	if block.Type != kind {
		return fmt.Errorf("expected %s block, got %s", kind, block.Type)
	}
	out.Content = append(out.Content, cloneBlock(*block))
	return nil
}

func endFrame(out *Message, idx int, kind string, ended map[int]bool) error {
	if idx < 0 || idx >= len(out.Content) {
		return fmt.Errorf("%s end points outside content", kind)
	}
	if ended[idx] {
		return fmt.Errorf("%s end follows the end", kind)
	}
	if out.Content[idx].Type != kind {
		return fmt.Errorf("expected %s block, got %s", kind, out.Content[idx].Type)
	}
	ended[idx] = true
	return nil
}

func appendFrameText(out *Message, idx int, delta string, ended map[int]bool) error {
	if idx < 0 || idx >= len(out.Content) || out.Content[idx].Type != "text" {
		return fmt.Errorf("expected text block")
	}
	if ended[idx] {
		return fmt.Errorf("text delta follows the end")
	}
	out.Content[idx].Text += delta
	return nil
}

func appendFrameThinking(out *Message, idx int, delta string, ended map[int]bool) error {
	if idx < 0 || idx >= len(out.Content) || out.Content[idx].Type != "thinking" {
		return fmt.Errorf("expected thinking block")
	}
	if ended[idx] {
		return fmt.Errorf("thinking delta follows the end")
	}
	out.Content[idx].Thinking += delta
	return nil
}

func appendFrameTool(out *Message, idx int, delta string, toolJSON map[int]string, ended map[int]bool) error {
	if idx < 0 || idx >= len(out.Content) || out.Content[idx].Type != "toolCall" {
		return fmt.Errorf("expected toolCall block")
	}
	if ended[idx] {
		return fmt.Errorf("toolcall delta follows the end")
	}
	toolJSON[idx] += delta
	args := parseFrameToolJSON(toolJSON[idx])
	out.Content[idx].Arguments = args
	return nil
}

func setFrameToolJSON(out *Message, idx int, text string, toolJSON map[int]string, ended map[int]bool) error {
	if idx < 0 || idx >= len(out.Content) || out.Content[idx].Type != "toolCall" {
		return fmt.Errorf("expected toolCall block")
	}
	if ended[idx] {
		return fmt.Errorf("toolcall checkpoint follows the end")
	}
	toolJSON[idx] = text
	out.Content[idx].Arguments = parseFrameToolJSON(text)
	return nil
}

func parseFrameToolJSON(text string) map[string]interface{} {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(text), &args); err == nil && args != nil {
		return args
	}
	for end := len(text); end >= 0; end-- {
		candidate := text[:end] + "\"}"
		if err := json.Unmarshal([]byte(candidate), &args); err == nil && args != nil {
			return args
		}
	}
	return map[string]interface{}{}
}

func frameText(m *Message, idx int) string {
	if m != nil && idx >= 0 && idx < len(m.Content) && m.Content[idx].Type == "text" {
		return m.Content[idx].Text
	}
	return ""
}

func frameThinking(m *Message, idx int) string {
	if m != nil && idx >= 0 && idx < len(m.Content) && m.Content[idx].Type == "thinking" {
		return m.Content[idx].Thinking
	}
	return ""
}

func frameToolJSON(m *Message, idx int) string {
	if m != nil && idx >= 0 && idx < len(m.Content) && m.Content[idx].Type == "toolCall" {
		return compactJSON(m.Content[idx].Arguments)
	}
	return ""
}

func compactJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func hasPrefixJSON(live, previous string) bool {
	return previous == "" || len(live) >= len(previous) && live[:len(previous)] == previous
}

func trimCoveredDelta(previous, delta, live string) string {
	if live == "" || previous == "" {
		return delta
	}
	covered := len(live) - len(previous)
	if covered <= 0 {
		return delta
	}
	if covered >= len(delta) {
		return ""
	}
	return delta[covered:]
}
