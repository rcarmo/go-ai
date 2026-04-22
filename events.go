package goai

// --- Stream events ---
// These mirror the AssistantMessageEvent discriminated union from pi-ai.

// Event is the interface for all stream events.
type Event interface {
	eventType() string
}

// StartEvent is emitted at the beginning of a stream.
type StartEvent struct {
	Partial *Message
}

func (e *StartEvent) eventType() string { return "start" }

// TextStartEvent signals the beginning of a text content block.
type TextStartEvent struct {
	ContentIndex int
	Partial      *Message
}

func (e *TextStartEvent) eventType() string { return "text_start" }

// TextDeltaEvent carries an incremental text chunk.
type TextDeltaEvent struct {
	ContentIndex int
	Delta        string
	Partial      *Message
}

func (e *TextDeltaEvent) eventType() string { return "text_delta" }

// TextEndEvent signals the end of a text content block.
type TextEndEvent struct {
	ContentIndex int
	Content      string
	Partial      *Message
}

func (e *TextEndEvent) eventType() string { return "text_end" }

// ThinkingStartEvent signals the beginning of a thinking block.
type ThinkingStartEvent struct {
	ContentIndex int
	Partial      *Message
}

func (e *ThinkingStartEvent) eventType() string { return "thinking_start" }

// ThinkingDeltaEvent carries an incremental thinking chunk.
type ThinkingDeltaEvent struct {
	ContentIndex int
	Delta        string
	Partial      *Message
}

func (e *ThinkingDeltaEvent) eventType() string { return "thinking_delta" }

// ThinkingEndEvent signals the end of a thinking block.
type ThinkingEndEvent struct {
	ContentIndex int
	Content      string
	Partial      *Message
}

func (e *ThinkingEndEvent) eventType() string { return "thinking_end" }

// ToolCallStartEvent signals the beginning of a tool call.
type ToolCallStartEvent struct {
	ContentIndex int
	Partial      *Message
}

func (e *ToolCallStartEvent) eventType() string { return "toolcall_start" }

// ToolCallDeltaEvent carries an incremental JSON chunk for tool arguments.
type ToolCallDeltaEvent struct {
	ContentIndex int
	Delta        string
	Partial      *Message
}

func (e *ToolCallDeltaEvent) eventType() string { return "toolcall_delta" }

// ToolCallEndEvent signals the end of a tool call with the parsed call.
type ToolCallEndEvent struct {
	ContentIndex int
	ToolCall     ToolCall
	Partial      *Message
}

func (e *ToolCallEndEvent) eventType() string { return "toolcall_end" }

// DoneEvent signals successful completion.
type DoneEvent struct {
	Reason  StopReason
	Message *Message
}

func (e *DoneEvent) eventType() string { return "done" }

// ErrorEvent signals an error or abort.
type ErrorEvent struct {
	Reason StopReason
	Error  *Message
	Err    error // Go-native error for convenience
}

func (e *ErrorEvent) eventType() string { return "error" }
