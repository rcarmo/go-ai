package goai

// SnapshotEvent detaches a progress event before a producer publishes it. Call
// on the producer's goroutine while it owns Partial; cloning after channel
// delivery cannot make a concurrently mutated partial safe. Terminal events
// already transfer ownership and are returned unchanged.
func SnapshotEvent(event Event) Event {
	snapshot := func(message *Message) *Message {
		if message == nil {
			return nil
		}
		copy := cloneMessage(*message)
		if message.Deferred != nil {
			handle := *message.Deferred
			handle.Data = deepCopyAny(handle.Data)
			copy.Deferred = &handle
		}
		return &copy
	}
	switch value := event.(type) {
	case *StartEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *TextStartEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *TextDeltaEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *TextEndEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ThinkingStartEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ThinkingDeltaEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ThinkingEndEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ToolCallStartEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ToolCallDeltaEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		return &copy
	case *ToolCallEndEvent:
		copy := *value
		copy.Partial = snapshot(value.Partial)
		copy.ToolCall.Arguments = deepCopyStringAnyMap(value.ToolCall.Arguments)
		return &copy
	default:
		return event
	}
}
