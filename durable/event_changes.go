package durable

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
)

// MessageChange describes an adopted partial revision. Block/message replacements
// are the fallback when an incremental append cannot represent the revision.
type MessageChange struct {
	Type         string
	ContentIndex int
	Block        *goai.ContentBlock
	Delta        string
	Path         []any
	Message      *MessageReceipt
}
type ToolOutputChange struct {
	TrimStart int
	Append    string
	Set       *string
}

func partialMessageChanges(before, after MessageReceipt, limits Limits) ([]MessageChange, error) {
	owned, err := detachReceipts([]MessageReceipt{before, after}, limits)
	if err != nil {
		return nil, err
	}
	before, after = owned[0], owned[1]
	if len(after.Content) < len(before.Content) {
		return []MessageChange{{Type: "message", Message: &after}}, nil
	}
	changes := []MessageChange{}
	for index, block := range after.Content {
		if index >= len(before.Content) {
			kind := "toolcall_start"
			if block.Type == "text" {
				kind = "text_start"
			}
			if block.Type == "thinking" {
				kind = "thinking_start"
			}
			copy := block
			changes = append(changes, MessageChange{Type: kind, ContentIndex: index, Block: &copy})
			continue
		}
		prior := before.Content[index]
		if equalJSONValue(prior, block) {
			continue
		}
		if prior.Type == block.Type && (block.Type == "text" || block.Type == "thinking") {
			left, right := prior.Text, block.Text
			if block.Type == "thinking" {
				left, right = prior.Thinking, block.Thinking
			}
			if strings.HasPrefix(right, left) {
				comparison := prior
				if block.Type == "text" {
					comparison.Text = right
				} else {
					comparison.Thinking = right
				}
				if equalJSONValue(comparison, block) {
					kind := "text_delta"
					if block.Type == "thinking" {
						kind = "thinking_delta"
					}
					changes = append(changes, MessageChange{Type: kind, ContentIndex: index, Delta: right[len(left):]})
					continue
				}
			}
		}
		if prior.Type == "toolCall" && block.Type == "toolCall" && prior.ID == block.ID && prior.Name == block.Name {
			ops, err := diffOperations(prior.Arguments, block.Arguments, limits)
			if err != nil {
				return nil, err
			}
			appendOnly := len(ops) > 0
			for _, op := range ops {
				if op[0] != "a" {
					appendOnly = false
				}
			}
			if appendOnly {
				for _, op := range ops {
					changes = append(changes, MessageChange{Type: "toolcall_delta", ContentIndex: index, Path: op[1].([]any), Delta: op[2].(string)})
				}
				continue
			}
		}
		copy := block
		changes = append(changes, MessageChange{Type: "block", ContentIndex: index, Block: &copy})
	}
	// Any non-content metadata edit (except usage, which is sent separately)
	// requires the complete message to avoid losing provider attribution fields.
	before.Content, after.Content = nil, nil
	before.Usage, after.Usage = nil, nil
	if !equalJSONValue(before, after) {
		message := owned[1]
		return []MessageChange{{Type: "message", Message: &message}}, nil
	}
	return changes, nil
}
func outputChange(before, after string) *ToolOutputChange {
	if strings.HasPrefix(after, before) {
		return &ToolOutputChange{Append: after[len(before):]}
	}
	// A retained tail is represented by a scalar-safe UTF-16 front trim and
	// append. Bound the suffix probe; complete replacement is always valid.
	probed := 0
	for offset := range before {
		if offset == 0 || before[offset]&0xc0 == 0x80 {
			continue
		}
		probed++
		if probed > 4096 {
			break
		}
		if strings.HasPrefix(after, before[offset:]) {
			units := 0
			for _, r := range before[:offset] {
				units++
				if r > 0xffff {
					units++
				}
			}
			return &ToolOutputChange{TrimStart: units, Append: after[len(before)-offset:]}
		}
	}
	value := after
	return &ToolOutputChange{Set: &value}
}
func eventEntryMessage(entry Entry, limits Limits) (*MessageReceipt, error) {
	if len(entry.Model) > 0 {
		messages, err := detachReceipts(entry.Model[:1], limits)
		if err != nil {
			return nil, err
		}
		return &messages[0], nil
	}
	if entry.Kind != "message" {
		return nil, nil
	}
	var message MessageReceipt
	if err := fromObject(entry.Value, &message, limits); err != nil {
		return nil, err
	}
	return &message, nil
}

// Tool completion is adjacent to its result entry, not grouped ahead of every
// other message in a mixed commit. Starts are synthesised for unstreamed entries.
func arrangeEntryEvents(events []AgentEvent, before Snapshot, conversation ID, limits Limits) ([]AgentEvent, error) {
	ends := map[string]AgentEvent{}
	for _, event := range events {
		if event.Type == "tool_execution_end" && event.Message != nil {
			ends[event.ToolCallID] = event
		}
	}
	streamed := false
	for _, task := range before.Tasks {
		if task.Kind == "pi.generation" && task.Conversation == conversation && !terminalStatus(task.Status) {
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, limits); err != nil {
				return nil, err
			}
			if cp.Partial != nil {
				streamed = true
			}
		}
	}
	result := make([]AgentEvent, 0, len(events))
	matched := map[string]bool{}
	for _, event := range events {
		if event.Type == "tool_execution_end" && event.Message != nil {
			continue
		}
		if event.Type == "entry_appended" {
			message, err := eventEntryMessage(*event.Entry, limits)
			if err != nil {
				return nil, err
			}
			if message != nil {
				if message.Role == goai.RoleToolResult {
					if end, ok := ends[message.ToolCallID]; ok {
						entry, err := copyEntry(*event.Entry, limits)
						if err != nil {
							return nil, err
						}
						end.Entry = &entry
						result = append(result, end)
						matched[message.ToolCallID] = true
					}
				}
				if message.Role != goai.RoleAssistant || !streamed {
					result = append(result, AgentEvent{Type: "message_start", Seq: event.Seq, Message: message})
				}
				if message.Role == goai.RoleAssistant {
					streamed = false
				}
			}
		}
		result = append(result, event)
	}
	// A fault/orphan can end without an entry. Retain original call ordering.
	for _, event := range events {
		if event.Type == "tool_execution_end" && event.Message != nil && !matched[event.ToolCallID] {
			result = append(result, event)
		}
	}
	return result, nil
}
