package goai

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// AssistantMessageFrame is a compact, immutable stream frame that can be used
// to reconstruct an assistant message without retaining provider scratch fields.
type AssistantMessageFrame struct {
	Type                     string                 `json:"type"`
	Partial                  *Message               `json:"partial,omitempty"`
	ContentIndex             int                    `json:"contentIndex,omitempty"`
	Content                  *ContentBlock          `json:"content,omitempty"`
	Delta                    string                 `json:"delta,omitempty"`
	ToolCall                 *ToolCall              `json:"toolCall,omitempty"`
	JSON                     string                 `json:"json,omitempty"`
	ID                       string                 `json:"id,omitempty"`
	Name                     string                 `json:"name,omitempty"`
	Arguments                map[string]interface{} `json:"arguments,omitempty"`
	Text                     string                 `json:"-"`
	TextSignature            string                 `json:"textSignature,omitempty"`
	TextSignaturePresent     bool                   `json:"-"`
	ThinkingSignature        string                 `json:"thinkingSignature,omitempty"`
	ThinkingSignaturePresent bool                   `json:"-"`
	Redacted                 *bool                  `json:"redacted,omitempty"`
	ThoughtSignature         string                 `json:"thoughtSignature,omitempty"`
	ThoughtSignaturePresent  bool                   `json:"-"`
	Namespace                string                 `json:"namespace,omitempty"`
	NamespacePresent         bool                   `json:"-"`
}

// MarshalJSON emits the upstream discriminated wire shape for each frame type.
// Required fields such as contentIndex are always present, including index 0;
// authoritative text/thinking end content is encoded as JSON field "content".
func (f AssistantMessageFrame) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{"type": f.Type}
	putIndex := func() { m["contentIndex"] = f.ContentIndex }
	switch f.Type {
	case "start":
		partial, err := assistantStartFrameWire(f.Partial)
		if err != nil {
			return nil, err
		}
		m["partial"] = partial
	case "text_start", "thinking_start":
		putIndex()
		if f.Content == nil {
			return nil, fmt.Errorf("%s frame missing content", f.Type)
		}
		content, err := frameContentWire(f.Content, f.Type)
		if err != nil {
			return nil, err
		}
		m["content"] = content
	case "text_delta", "thinking_delta":
		putIndex()
		m["delta"] = f.Delta
	case "text_end":
		putIndex()
		m["content"] = f.Text
		if f.TextSignaturePresent || f.TextSignature != "" {
			m["textSignature"] = f.TextSignature
		}
	case "thinking_end":
		putIndex()
		m["content"] = f.Text
		if f.ThinkingSignaturePresent || f.ThinkingSignature != "" {
			m["thinkingSignature"] = f.ThinkingSignature
		}
		if f.Redacted != nil {
			m["redacted"] = *f.Redacted
		}
	case "toolcall_start":
		putIndex()
		if f.ToolCall == nil {
			return nil, fmt.Errorf("toolcall_start frame missing toolCall")
		}
		m["toolCall"] = frameToolCallWire(f.ToolCall)
	case "toolcall_checkpoint":
		putIndex()
		m["json"] = f.JSON
	case "toolcall_delta":
		putIndex()
		m["delta"] = f.Delta
	case "toolcall_end":
		putIndex()
		m["id"] = f.ID
		m["name"] = f.Name
		args := cloneFrameMap(f.Arguments)
		if args == nil {
			args = map[string]interface{}{}
		}
		m["arguments"] = args
		if f.ThoughtSignaturePresent || f.ThoughtSignature != "" {
			m["thoughtSignature"] = f.ThoughtSignature
		}
		if f.NamespacePresent || f.Namespace != "" {
			m["namespace"] = f.Namespace
		}
	default:
		return nil, fmt.Errorf("unknown assistant message frame type %q", f.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON accepts the same upstream discriminated wire shape emitted by
// MarshalJSON and rejects unknown/malformed variants instead of silently
// accepting lossy persistence records.
func (f *AssistantMessageFrame) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var typ string
	if err := unmarshalRequiredFrameField(raw, "type", &typ); err != nil {
		return err
	}
	out := AssistantMessageFrame{Type: typ}
	readIndex := func() error { return unmarshalRequiredFrameField(raw, "contentIndex", &out.ContentIndex) }
	switch typ {
	case "start":
		partial, err := decodeAssistantStartFrameWire(raw)
		if err != nil {
			return err
		}
		out.Partial = partial
	case "text_start", "thinking_start":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "content", &out.Content); err != nil {
			return err
		}
		want := "text"
		required := "text"
		if typ == "thinking_start" {
			want = "thinking"
			required = "thinking"
		}
		if out.Content == nil || out.Content.Type != want {
			return fmt.Errorf("%s frame contains invalid content", typ)
		}
		var contentRaw map[string]json.RawMessage
		if err := json.Unmarshal(raw["content"], &contentRaw); err != nil {
			return fmt.Errorf("invalid content: %w", err)
		}
		var requiredText string
		if err := unmarshalRequiredFrameField(contentRaw, required, &requiredText); err != nil {
			return fmt.Errorf("%s frame content missing %s: %w", typ, required, err)
		}
		if value, present, err := unmarshalOptionalFrameString(contentRaw, "textSignature"); err != nil {
			return err
		} else if present {
			out.Content.TextSignature = value
			out.Content.TextSignaturePresent = true
		}
		if value, present, err := unmarshalOptionalFrameString(contentRaw, "thinkingSignature"); err != nil {
			return err
		} else if present {
			out.Content.ThinkingSignature = value
			out.Content.ThinkingSignaturePresent = true
		}
		if rawRedacted, ok := contentRaw["redacted"]; ok {
			var redacted bool
			if len(rawRedacted) == 0 || string(rawRedacted) == "null" {
				return fmt.Errorf("invalid redacted: null")
			}
			if err := json.Unmarshal(rawRedacted, &redacted); err != nil {
				return fmt.Errorf("invalid redacted: %w", err)
			}
			out.Content.Redacted = redacted
			out.Content.RedactedPresent = true
		}
	case "text_delta", "thinking_delta", "toolcall_delta":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "delta", &out.Delta); err != nil {
			return err
		}
	case "text_end":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "content", &out.Text); err != nil {
			return err
		}
		if value, present, err := unmarshalOptionalFrameString(raw, "textSignature"); err != nil {
			return err
		} else if present {
			out.TextSignature = value
			out.TextSignaturePresent = true
		}
	case "thinking_end":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "content", &out.Text); err != nil {
			return err
		}
		if value, present, err := unmarshalOptionalFrameString(raw, "thinkingSignature"); err != nil {
			return err
		} else if present {
			out.ThinkingSignature = value
			out.ThinkingSignaturePresent = true
		}
		if rawRedacted, ok := raw["redacted"]; ok {
			var redacted bool
			if len(rawRedacted) == 0 || string(rawRedacted) == "null" {
				return fmt.Errorf("invalid redacted: null")
			}
			if err := json.Unmarshal(rawRedacted, &redacted); err != nil {
				return fmt.Errorf("invalid redacted: %w", err)
			}
			out.Redacted = &redacted
		}
	case "toolcall_start":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "toolCall", &out.ToolCall); err != nil {
			return err
		}
		if out.ToolCall == nil || out.ToolCall.Type != "toolCall" {
			return fmt.Errorf("toolcall_start frame contains invalid toolCall")
		}
		var toolRaw map[string]json.RawMessage
		if err := json.Unmarshal(raw["toolCall"], &toolRaw); err != nil {
			return fmt.Errorf("invalid toolCall: %w", err)
		}
		var toolType, toolID, toolName string
		if err := unmarshalRequiredFrameField(toolRaw, "type", &toolType); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(toolRaw, "id", &toolID); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(toolRaw, "name", &toolName); err != nil {
			return err
		}
		var args map[string]interface{}
		if err := unmarshalRequiredFrameField(toolRaw, "arguments", &args); err != nil {
			return err
		}
		if value, present, err := unmarshalOptionalFrameString(toolRaw, "thoughtSignature"); err != nil {
			return err
		} else if present {
			out.ToolCall.ThoughtSignature = value
			out.ToolCall.ThoughtSignaturePresent = true
		}
		if value, present, err := unmarshalOptionalFrameString(toolRaw, "namespace"); err != nil {
			return err
		} else if present {
			out.ToolCall.Namespace = value
			out.ToolCall.NamespacePresent = true
		}
	case "toolcall_checkpoint":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "json", &out.JSON); err != nil {
			return err
		}
	case "toolcall_end":
		if err := readIndex(); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "id", &out.ID); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "name", &out.Name); err != nil {
			return err
		}
		if err := unmarshalRequiredFrameField(raw, "arguments", &out.Arguments); err != nil {
			return err
		}
		if value, present, err := unmarshalOptionalFrameString(raw, "thoughtSignature"); err != nil {
			return err
		} else if present {
			out.ThoughtSignature = value
			out.ThoughtSignaturePresent = true
		}
		if value, present, err := unmarshalOptionalFrameString(raw, "namespace"); err != nil {
			return err
		} else if present {
			out.Namespace = value
			out.NamespacePresent = true
		}
	default:
		return fmt.Errorf("unknown assistant message frame type %q", typ)
	}
	*f = out
	return nil
}

type assistantStartPartialWire struct {
	Role                  Role                         `json:"role"`
	Content               []ContentBlock               `json:"content"`
	Timestamp             int64                        `json:"timestamp"`
	Api                   Api                          `json:"api,omitempty"`
	Provider              Provider                     `json:"provider,omitempty"`
	Model                 string                       `json:"model,omitempty"`
	ResponseID            string                       `json:"responseId,omitempty"`
	ResponseModel         string                       `json:"responseModel,omitempty"`
	ProviderThinkingLevel string                       `json:"providerThinkingLevel,omitempty"`
	Diagnostics           []AssistantMessageDiagnostic `json:"diagnostics,omitempty"`
	Usage                 *Usage                       `json:"usage,omitempty"`
	StopReason            StopReason                   `json:"stopReason,omitempty"`
}

func assistantStartFrameWire(partial *Message) (*assistantStartPartialWire, error) {
	if partial == nil {
		return nil, fmt.Errorf("start frame missing partial")
	}
	if partial.Role != RoleAssistant {
		return nil, fmt.Errorf("start frame partial role must be assistant")
	}
	stopReason := StopReasonPending
	if partial.StopReason == StopReasonPending || partial.StopReason == "" {
		stopReason = StopReasonPending
	}
	var usage *Usage
	if partial.Usage != nil {
		u := *partial.Usage
		usage = &u
	}
	return &assistantStartPartialWire{
		Role:                  RoleAssistant,
		Content:               []ContentBlock{},
		Timestamp:             partial.Timestamp,
		Api:                   partial.Api,
		Provider:              partial.Provider,
		Model:                 partial.Model,
		ResponseID:            partial.ResponseID,
		ResponseModel:         partial.ResponseModel,
		ProviderThinkingLevel: partial.ProviderThinkingLevel,
		Diagnostics:           cloneFrameAny(partial.Diagnostics).([]AssistantMessageDiagnostic),
		Usage:                 usage,
		StopReason:            stopReason,
	}, nil
}

func decodeAssistantStartFrameWire(raw map[string]json.RawMessage) (*Message, error) {
	partialRaw, ok := raw["partial"]
	if !ok || len(partialRaw) == 0 || string(partialRaw) == "null" {
		return nil, fmt.Errorf("assistant message frame missing partial")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(partialRaw, &fields); err != nil {
		return nil, fmt.Errorf("invalid partial: %w", err)
	}
	forbidden := []string{"deferred", "rawStopReason", "errorMessage", "endTurn", "toolCallId", "toolName", "addedToolNames", "isError", "details"}
	for _, name := range forbidden {
		if _, ok := fields[name]; ok {
			return nil, fmt.Errorf("start frame partial contains forbidden field %s", name)
		}
	}
	var partial assistantStartPartialWire
	if err := json.Unmarshal(partialRaw, &partial); err != nil {
		return nil, fmt.Errorf("invalid partial: %w", err)
	}
	if partial.Role != RoleAssistant {
		return nil, fmt.Errorf("start frame partial role must be assistant")
	}
	if partial.Content == nil || len(partial.Content) != 0 {
		return nil, fmt.Errorf("start frame partial content must be []")
	}
	if partial.StopReason != "" && partial.StopReason != StopReasonPending {
		return nil, fmt.Errorf("start frame partial stopReason must be pending")
	}
	return &Message{
		Role:                  RoleAssistant,
		Content:               []ContentBlock{},
		Timestamp:             partial.Timestamp,
		Api:                   partial.Api,
		Provider:              partial.Provider,
		Model:                 partial.Model,
		ResponseID:            partial.ResponseID,
		ResponseModel:         partial.ResponseModel,
		ProviderThinkingLevel: partial.ProviderThinkingLevel,
		Diagnostics:           cloneFrameAny(partial.Diagnostics).([]AssistantMessageDiagnostic),
		Usage:                 partial.Usage,
		StopReason:            StopReasonPending,
	}, nil
}

func unmarshalRequiredFrameField(raw map[string]json.RawMessage, name string, target interface{}) error {
	value, ok := raw[name]
	if !ok || len(value) == 0 || string(value) == "null" {
		return fmt.Errorf("assistant message frame missing %s", name)
	}
	if err := json.Unmarshal(value, target); err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	return nil
}

func unmarshalOptionalFrameString(raw map[string]json.RawMessage, name string) (string, bool, error) {
	value, ok := raw[name]
	if !ok {
		return "", false, nil
	}
	if len(value) == 0 || string(value) == "null" {
		return "", false, fmt.Errorf("invalid %s: null", name)
	}
	var out string
	if err := json.Unmarshal(value, &out); err != nil {
		return "", false, fmt.Errorf("invalid %s: %w", name, err)
	}
	return out, true, nil
}

func frameContentWire(block *ContentBlock, frameType string) (map[string]interface{}, error) {
	switch frameType {
	case "text_start":
		if block.Type != "text" {
			return nil, fmt.Errorf("text_start frame contains invalid content")
		}
		m := map[string]interface{}{"type": "text", "text": block.Text}
		if block.TextSignaturePresent || block.TextSignature != "" {
			m["textSignature"] = block.TextSignature
		}
		return m, nil
	case "thinking_start":
		if block.Type != "thinking" {
			return nil, fmt.Errorf("thinking_start frame contains invalid content")
		}
		m := map[string]interface{}{"type": "thinking", "thinking": block.Thinking}
		if block.ThinkingSignaturePresent || block.ThinkingSignature != "" {
			m["thinkingSignature"] = block.ThinkingSignature
		}
		if block.RedactedPresent || block.Redacted {
			m["redacted"] = block.Redacted
		}
		return m, nil
	default:
		return nil, fmt.Errorf("unknown content frame type %q", frameType)
	}
}

func frameToolCallWire(call *ToolCall) map[string]interface{} {
	args := cloneFrameMap(call.Arguments)
	if args == nil {
		args = map[string]interface{}{}
	}
	m := map[string]interface{}{"type": "toolCall", "id": call.ID, "name": call.Name, "arguments": args}
	if call.ThoughtSignaturePresent || call.ThoughtSignature != "" {
		m["thoughtSignature"] = call.ThoughtSignature
	}
	if call.NamespacePresent || call.Namespace != "" {
		m["namespace"] = call.Namespace
	}
	return m
}

// AssistantMessageFrameEncoder converts mutable stream events into compact
// frames. Terminal done/error events are intentionally not framed; settlement is
// handled by the stream consumer.
type AssistantMessageFrameEncoder struct {
	started  bool
	terminal bool
	blocks   map[int]*assistantFrameEncoderBlock
}

type assistantFrameEncoderBlock struct {
	kind              string
	coveredChars      int
	deltaChars        int
	caughtUp          bool
	catchupJSON       string
	snapshotArguments string
}

func NewAssistantMessageFrameEncoder() *AssistantMessageFrameEncoder {
	return &AssistantMessageFrameEncoder{blocks: map[int]*assistantFrameEncoderBlock{}}
}

func (e *AssistantMessageFrameEncoder) Encode(event Event) (*AssistantMessageFrame, error) {
	if e.blocks == nil {
		e.blocks = map[int]*assistantFrameEncoderBlock{}
	}
	if e.terminal {
		return nil, fmt.Errorf("assistant message event %s follows a terminal event", event.eventType())
	}
	switch ev := event.(type) {
	case *StartEvent:
		if e.started {
			return nil, fmt.Errorf("assistant message stream contains more than one start event")
		}
		e.started = true
		return &AssistantMessageFrame{Type: "start", Partial: cloneFrameMessage(ev.Partial, false)}, nil
	case *DoneEvent:
		if !e.started {
			return nil, fmt.Errorf("done event appears before start")
		}
		e.terminal = true
		return nil, nil
	case *ErrorEvent:
		e.terminal = true
		return nil, nil
	case *TextStartEvent:
		if err := e.requireStarted("text_start"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "text")
		if err != nil {
			return nil, err
		}
		if err := e.startBlock(ev.ContentIndex, "text"); err != nil {
			return nil, err
		}
		e.blocks[ev.ContentIndex].coveredChars = len(block.Text)
		return &AssistantMessageFrame{Type: "text_start", ContentIndex: ev.ContentIndex, Content: block}, nil
	case *TextDeltaEvent:
		if err := e.requireStarted("text_delta"); err != nil {
			return nil, err
		}
		delta, err := e.encodeTextDelta(ev.ContentIndex, ev.Delta, "text")
		if err != nil || delta == "" {
			return nil, err
		}
		return &AssistantMessageFrame{Type: "text_delta", ContentIndex: ev.ContentIndex, Delta: delta}, nil
	case *TextEndEvent:
		if err := e.requireStarted("text_end"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "text")
		if err != nil {
			return nil, err
		}
		if err := e.endBlock(ev.ContentIndex, "text"); err != nil {
			return nil, err
		}
		return &AssistantMessageFrame{Type: "text_end", ContentIndex: ev.ContentIndex, Delta: "", Text: ev.Content, TextSignature: block.TextSignature, TextSignaturePresent: block.TextSignaturePresent}, nil
	case *ThinkingStartEvent:
		if err := e.requireStarted("thinking_start"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "thinking")
		if err != nil {
			return nil, err
		}
		if err := e.startBlock(ev.ContentIndex, "thinking"); err != nil {
			return nil, err
		}
		e.blocks[ev.ContentIndex].coveredChars = len(block.Thinking)
		return &AssistantMessageFrame{Type: "thinking_start", ContentIndex: ev.ContentIndex, Content: block}, nil
	case *ThinkingDeltaEvent:
		if err := e.requireStarted("thinking_delta"); err != nil {
			return nil, err
		}
		delta, err := e.encodeTextDelta(ev.ContentIndex, ev.Delta, "thinking")
		if err != nil || delta == "" {
			return nil, err
		}
		return &AssistantMessageFrame{Type: "thinking_delta", ContentIndex: ev.ContentIndex, Delta: delta}, nil
	case *ThinkingEndEvent:
		if err := e.requireStarted("thinking_end"); err != nil {
			return nil, err
		}
		block, err := frameBlock(ev.Partial, ev.ContentIndex, "thinking")
		if err != nil {
			return nil, err
		}
		if err := e.endBlock(ev.ContentIndex, "thinking"); err != nil {
			return nil, err
		}
		var redacted *bool
		if block.RedactedPresent || block.Redacted {
			value := block.Redacted
			redacted = &value
		}
		return &AssistantMessageFrame{Type: "thinking_end", ContentIndex: ev.ContentIndex, Text: ev.Content, ThinkingSignature: block.ThinkingSignature, ThinkingSignaturePresent: block.ThinkingSignaturePresent, Redacted: redacted}, nil
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
		if err := e.startBlock(ev.ContentIndex, "toolCall"); err != nil {
			return nil, err
		}
		e.blocks[ev.ContentIndex].snapshotArguments = jsonArgs
		e.blocks[ev.ContentIndex].caughtUp = jsonArgs == "{}"
		if e.blocks[ev.ContentIndex].caughtUp {
			e.blocks[ev.ContentIndex].snapshotArguments = ""
		}
		return &AssistantMessageFrame{Type: "toolcall_start", ContentIndex: ev.ContentIndex, ToolCall: &call}, nil
	case *ToolCallDeltaEvent:
		if err := e.requireStarted("toolcall_delta"); err != nil {
			return nil, err
		}
		state, err := e.requireBlock(ev.ContentIndex, "toolCall")
		if err != nil {
			return nil, err
		}
		if state.caughtUp {
			if ev.Delta == "" {
				return nil, nil
			}
			return &AssistantMessageFrame{Type: "toolcall_delta", ContentIndex: ev.ContentIndex, Delta: ev.Delta}, nil
		}
		state.catchupJSON += ev.Delta
		parsed := parseFrameToolJSON(state.catchupJSON)
		if compactJSON(parsed) != state.snapshotArguments {
			snapshot := parseFrameToolJSON(state.snapshotArguments)
			if !isFrameJSONPrefix(snapshot, parsed) {
				return nil, nil
			}
		}
		state.caughtUp = true
		state.snapshotArguments = ""
		json := state.catchupJSON
		state.catchupJSON = ""
		if json == "" {
			return nil, nil
		}
		return &AssistantMessageFrame{Type: "toolcall_checkpoint", ContentIndex: ev.ContentIndex, JSON: json}, nil
	case *ToolCallEndEvent:
		if err := e.requireStarted("toolcall_end"); err != nil {
			return nil, err
		}
		if _, err := frameBlock(ev.Partial, ev.ContentIndex, "toolCall"); err != nil {
			return nil, err
		}
		if err := e.endBlock(ev.ContentIndex, "toolCall"); err != nil {
			return nil, err
		}
		call := ev.ToolCall
		if call.Type != "toolCall" && call.Type != "" {
			return nil, fmt.Errorf("toolcall_end event has invalid tool call at index %d", ev.ContentIndex)
		}
		return &AssistantMessageFrame{Type: "toolcall_end", ContentIndex: ev.ContentIndex, ID: call.ID, Name: call.Name, Arguments: cloneFrameMap(call.Arguments), ThoughtSignature: call.ThoughtSignature, ThoughtSignaturePresent: call.ThoughtSignaturePresent, Namespace: call.Namespace, NamespacePresent: call.NamespacePresent}, nil
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

func (e *AssistantMessageFrameEncoder) startBlock(contentIndex int, kind string) error {
	if contentIndex < 0 {
		return fmt.Errorf("invalid assistant message frame contentIndex: %d", contentIndex)
	}
	if _, ok := e.blocks[contentIndex]; ok {
		return fmt.Errorf("assistant message block %d starts more than once", contentIndex)
	}
	e.blocks[contentIndex] = &assistantFrameEncoderBlock{kind: kind}
	return nil
}

func (e *AssistantMessageFrameEncoder) requireBlock(contentIndex int, kind string) (*assistantFrameEncoderBlock, error) {
	if contentIndex < 0 {
		return nil, fmt.Errorf("invalid assistant message frame contentIndex: %d", contentIndex)
	}
	state, ok := e.blocks[contentIndex]
	if !ok {
		return nil, fmt.Errorf("assistant message %s block %d has not started", kind, contentIndex)
	}
	if state.kind != kind {
		return nil, fmt.Errorf("assistant message block %d is %s, not %s", contentIndex, state.kind, kind)
	}
	return state, nil
}

func (e *AssistantMessageFrameEncoder) endBlock(contentIndex int, kind string) error {
	if _, err := e.requireBlock(contentIndex, kind); err != nil {
		return err
	}
	delete(e.blocks, contentIndex)
	return nil
}

func (e *AssistantMessageFrameEncoder) encodeTextDelta(contentIndex int, delta, kind string) (string, error) {
	state, err := e.requireBlock(contentIndex, kind)
	if err != nil {
		return "", err
	}
	deltaStart := state.deltaChars
	state.deltaChars += len(delta)
	covered := state.coveredChars - deltaStart
	if covered <= 0 {
		return delta, nil
	}
	if covered >= len(delta) {
		return "", nil
	}
	return delta[covered:], nil
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
			if out != nil {
				return nil, fmt.Errorf("assistant message frame sequence contains more than one start frame")
			}
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
			out.Content[frame.ContentIndex].TextSignaturePresent = frame.TextSignaturePresent
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
			out.Content[frame.ContentIndex].ThinkingSignaturePresent = frame.ThinkingSignaturePresent
			out.Content[frame.ContentIndex].Redacted = false
			out.Content[frame.ContentIndex].RedactedPresent = false
			if frame.Redacted != nil {
				out.Content[frame.ContentIndex].Redacted = *frame.Redacted
				out.Content[frame.ContentIndex].RedactedPresent = true
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
			out.Content[frame.ContentIndex].ThoughtSignaturePresent = frame.ThoughtSignaturePresent
			out.Content[frame.ContentIndex].Namespace = frame.Namespace
			out.Content[frame.ContentIndex].NamespacePresent = frame.NamespacePresent
		default:
			return nil, fmt.Errorf("unknown assistant message frame type %q", frame.Type)
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
	out.Diagnostics = cloneFrameAny(m.Diagnostics).([]AssistantMessageDiagnostic)
	if includeContent {
		out.Content = cloneBlocks(m.Content)
	} else {
		out.Content = []ContentBlock{}
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
	out, _ := cloneFrameAny(in).(map[string]interface{})
	return out
}

func cloneFrameAny[T any](in T) any {
	data, err := json.Marshal(in)
	if err != nil {
		return in
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return in
	}
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
	return ToolCall{Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: cloneFrameMap(block.Arguments), ThoughtSignature: block.ThoughtSignature, ThoughtSignaturePresent: block.ThoughtSignaturePresent, Namespace: block.Namespace, NamespacePresent: block.NamespacePresent}
}

func toolCallToBlock(call *ToolCall) ContentBlock {
	if call == nil {
		return ContentBlock{Type: "toolCall", Arguments: map[string]interface{}{}}
	}
	return ContentBlock{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: cloneFrameMap(call.Arguments), ThoughtSignature: call.ThoughtSignature, ThoughtSignaturePresent: call.ThoughtSignaturePresent, Namespace: call.Namespace, NamespacePresent: call.NamespacePresent}
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

func isFrameJSONPrefix(snapshot, current interface{}) bool {
	if reflect.DeepEqual(snapshot, current) {
		return true
	}
	if s, ok := snapshot.(string); ok {
		c, ok := current.(string)
		return ok && strings.HasPrefix(c, s)
	}
	if s, ok := snapshot.([]interface{}); ok {
		c, ok := current.([]interface{})
		if !ok || len(s) > len(c) {
			return false
		}
		for i := range s {
			if !isFrameJSONPrefix(s[i], c[i]) {
				return false
			}
		}
		return true
	}
	s, ok := snapshot.(map[string]interface{})
	if !ok {
		return false
	}
	c, ok := current.(map[string]interface{})
	if !ok {
		return false
	}
	for key, value := range s {
		currentValue, ok := c[key]
		if !ok || !isFrameJSONPrefix(value, currentValue) {
			return false
		}
	}
	return true
}
