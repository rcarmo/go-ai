// Message transformation for cross-provider compatibility.
package goai

// TransformMessages normalizes messages for the target model:
//   - Strips thinking blocks when switching providers (keeps for same model)
//   - Replaces images with placeholders for non-vision models
//   - Skips errored/aborted assistant messages
//   - Inserts synthetic tool results for orphaned tool calls
func TransformMessages(messages []Message, model *Model) []Message {
	if model == nil {
		logWarn("transform messages without model")
		return messages
	}

	messages, imageDowngrades := downgradeUnsupportedImages(messages, model)

	var transformed []Message
	toolCallIDMap := map[string]string{} // original → normalized
	_ = toolCallIDMap                    // used when normalizeToolCallId is provided
	trimmedAssistantErrors := 0
	crossProviderThinking := 0

	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			transformed = append(transformed, msg)

		case RoleToolResult:
			transformed = append(transformed, msg)

		case RoleAssistant:
			if msg.StopReason == StopReasonError || msg.StopReason == StopReasonAborted {
				trimmedAssistantErrors++
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
						crossProviderThinking++
						newContent = append(newContent, ContentBlock{Type: "text", Text: block.Thinking})
					}

				case "text":
					if isSameModel {
						newContent = append(newContent, block)
					} else {
						newContent = append(newContent, ContentBlock{Type: "text", Text: block.Text})
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

	result, syntheticResults := insertSyntheticToolResults(transformed)
	if imageDowngrades > 0 || trimmedAssistantErrors > 0 || crossProviderThinking > 0 || syntheticResults > 0 {
		logDebug("transform messages",
			"model", model.ID,
			"provider", model.Provider,
			"imageDowngrades", imageDowngrades,
			"trimmedAssistantErrors", trimmedAssistantErrors,
			"crossProviderThinking", crossProviderThinking,
			"syntheticToolResults", syntheticResults)
	}
	return result
}

func insertSyntheticToolResults(messages []Message) ([]Message, int) {
	var result []Message
	var pendingToolCalls []ContentBlock
	existingResultIDs := map[string]bool{}
	inserted := 0

	flushOrphans := func() {
		for _, tc := range pendingToolCalls {
			if !existingResultIDs[tc.ID] {
				inserted++
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
	flushOrphans()

	return result, inserted
}

// downgradeUnsupportedImages replaces image content with text placeholders
// for models that don't support image input.
func downgradeUnsupportedImages(messages []Message, model *Model) ([]Message, int) {
	supportsImages := false
	for _, input := range model.Input {
		if input == "image" {
			supportsImages = true
			break
		}
	}
	if supportsImages {
		return messages, 0
	}

	result := make([]Message, len(messages))
	replaced := 0
	for i, msg := range messages {
		if msg.Role == RoleUser || msg.Role == RoleToolResult {
			newContent := make([]ContentBlock, 0, len(msg.Content))
			prevWasPlaceholder := false
			for _, block := range msg.Content {
				if block.Type == "image" {
					replaced++
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
	return result, replaced
}
