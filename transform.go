// Message transformation for cross-provider compatibility.
package goai

// TransformMessages normalizes messages for the target model:
//   - Strips thinking blocks when switching providers (keeps for same model)
//   - Replaces images with placeholders for non-vision models
//   - Skips errored/aborted assistant messages
//   - Inserts synthetic tool results for orphaned tool calls
func TransformMessages(messages []Message, model *Model) []Message {
	messages = downgradeUnsupportedImages(messages, model)

	// First pass: transform content blocks
	var transformed []Message
	toolCallIDMap := map[string]string{} // original → normalized
	_ = toolCallIDMap                    // used when normalizeToolCallId is provided

	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			transformed = append(transformed, msg)

		case RoleToolResult:
			transformed = append(transformed, msg)

		case RoleAssistant:
			// Skip errored/aborted assistant messages entirely
			if msg.StopReason == StopReasonError || msg.StopReason == StopReasonAborted {
				continue
			}

			isSameModel := msg.Provider == model.Provider &&
				msg.Api == model.Api &&
				msg.Model == model.ID

			var newContent []ContentBlock
			for _, block := range msg.Content {
				switch block.Type {
				case "thinking":
					if block.Redacted {
						if isSameModel {
							newContent = append(newContent, block)
						}
						continue
					}
					if isSameModel && block.ThinkingSignature != "" {
						newContent = append(newContent, block)
						continue
					}
					if block.Thinking == "" {
						continue
					}
					if isSameModel {
						newContent = append(newContent, block)
					} else {
						// Convert thinking to text for cross-provider
						newContent = append(newContent, ContentBlock{
							Type: "text",
							Text: block.Thinking,
						})
					}

				case "text":
					if isSameModel {
						newContent = append(newContent, block)
					} else {
						newContent = append(newContent, ContentBlock{
							Type: "text",
							Text: block.Text,
						})
					}

				case "toolCall":
					tc := block
					if !isSameModel && tc.ThoughtSignature != "" {
						tc.ThoughtSignature = ""
					}
					newContent = append(newContent, tc)

				default:
					newContent = append(newContent, block)
				}
			}

			newMsg := msg
			newMsg.Content = newContent
			transformed = append(transformed, newMsg)
		}
	}

	// Second pass: insert synthetic tool results for orphaned tool calls
	return insertSyntheticToolResults(transformed)
}

func insertSyntheticToolResults(messages []Message) []Message {
	var result []Message
	var pendingToolCalls []ContentBlock
	existingResultIDs := map[string]bool{}

	flushOrphans := func() {
		for _, tc := range pendingToolCalls {
			if !existingResultIDs[tc.ID] {
				result = append(result, Message{
					Role:       RoleToolResult,
					ToolCallID: tc.ID,
					ToolName:   tc.Name,
					Content:    []ContentBlock{{Type: "text", Text: "No result provided"}},
					IsError:    true,
				})
			}
		}
		pendingToolCalls = nil
		existingResultIDs = map[string]bool{}
	}

	for _, msg := range messages {
		switch msg.Role {
		case RoleAssistant:
			flushOrphans()
			// Track tool calls
			for _, c := range msg.Content {
				if c.Type == "toolCall" {
					pendingToolCalls = append(pendingToolCalls, c)
				}
			}
			result = append(result, msg)

		case RoleToolResult:
			existingResultIDs[msg.ToolCallID] = true
			result = append(result, msg)

		case RoleUser:
			flushOrphans()
			result = append(result, msg)
		}
	}

	return result
}

// downgradeUnsupportedImages replaces image content with text placeholders
// for models that don't support image input.
func downgradeUnsupportedImages(messages []Message, model *Model) []Message {
	supportsImages := false
	for _, input := range model.Input {
		if input == "image" {
			supportsImages = true
			break
		}
	}
	if supportsImages {
		return messages
	}

	result := make([]Message, len(messages))
	for i, msg := range messages {
		if msg.Role == RoleUser || msg.Role == RoleToolResult {
			newContent := make([]ContentBlock, 0, len(msg.Content))
			prevWasPlaceholder := false
			for _, block := range msg.Content {
				if block.Type == "image" {
					if !prevWasPlaceholder {
						placeholder := "(image omitted: model does not support images)"
						if msg.Role == RoleToolResult {
							placeholder = "(tool image omitted: model does not support images)"
						}
						newContent = append(newContent, ContentBlock{Type: "text", Text: placeholder})
					}
					prevWasPlaceholder = true
					continue
				}
				newContent = append(newContent, block)
				prevWasPlaceholder = false
			}
			msg.Content = newContent
		}
		result[i] = msg
	}
	return result
}
