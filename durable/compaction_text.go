package durable

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sort"
	"strings"
	"unicode/utf8"
)

const compactionSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`
const compactionPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work. If the conversation starts with an earlier summary, preserve its information and fold the newer messages into it.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// SerializeConversation presents model context as an inert text transcript.
// System contributions, thinking and image payloads are omitted. Tool results retain at
// most 2000 UTF-16 units, using the source String.slice boundary.
// Isolated units from slicing or explicitly typed arguments/diagnostics use WTF-8 in the
// returned string; compaction persists them through its scoped source state.
func SerializeConversation(messages []MessageReceipt, limits Limits) (string, error) {
	parts := []string{}
	nativeText := false
	for _, message := range messages {
		text := []string{}
		if message.ContentText != nil {
			if err := message.validateContent(); err != nil {
				return "", err
			}
			if !utf8.ValidString(*message.ContentText) {
				return "", reject("invalid compaction text")
			}
			text = append(text, *message.ContentText)
		}
		calls := []string{}
		for _, block := range message.Content {
			switch block.Type {
			case "text":
				if !utf8.ValidString(block.Text) {
					if !block.HasToolArgumentDiagnostic() && !block.HasToolArgumentTranscript() {
						return "", reject("invalid compaction text")
					}
					nativeText = true
				}
				text = append(text, block.Text)
			case "toolCall":
				if !utf8.ValidString(block.Name) {
					return "", reject("invalid compaction tool name")
				}
				owned, err := copyToolArguments(block, limits)
				if err != nil {
					return "", err
				}
				args := []string{}
				keys := make([]string, 0, len(block.Arguments))
				for key := range block.Arguments {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if !utf8.ValidString(key) {
						nativeText = true
					}
					value, err := encodeCompactionArgument(owned[key], block.HasStreamingArguments(), limits)
					if err != nil {
						return "", err
					}
					args = append(args, key+"="+string(value))
				}
				calls = append(calls, block.Name+"("+strings.Join(args, ", ")+")")
			}
		}
		joined := strings.Join(text, "\n")
		switch message.Role {
		case goai.RoleUser:
			if joined != "" {
				parts = append(parts, "[User]: "+joined)
			}
		case goai.RoleAssistant:
			if len(text) > 0 {
				parts = append(parts, "[Assistant]: "+joined)
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case goai.RoleToolResult:
			if joined != "" {
				truncated := truncateCompactionText(joined, 2000)
				if !utf8.ValidString(truncated) {
					nativeText = true // validated text can be sliced between surrogate units
				}
				parts = append(parts, "[Tool result]: "+truncated)
			}
		}
	}
	transcript := strings.Join(parts, "\n\n")
	if err := compactionSourceValid(transcript, nativeText, limits); err != nil {
		return "", err
	}
	return transcript, nil
}
func truncateCompactionText(text string, maximum int) string {
	units, kept, cut := 0, 0, len(text)
	partial := ""
	for offset := 0; offset < len(text); {
		r, size, width := compactionRune(text[offset:])
		if units+width > maximum && cut == len(text) {
			cut = offset
			kept = units
			if width == 2 && maximum-units == 1 {
				high := rune(0xd800) + (r-0x10000)>>10
				partial = string([]byte{0xe0 | byte(high>>12), 0x80 | byte(high>>6)&0x3f, 0x80 | byte(high)&0x3f})
				kept++
			}
		}
		units += width
		offset += size
	}
	if cut == len(text) {
		return text
	}
	return text[:cut] + partial + fmt.Sprintf("\n\n[... %d more characters truncated]", units-kept)
}

func compactionRequestText(source string, instructions *string) string {
	focus := ""
	if instructions != nil {
		focus = "\n\nAdditional focus: " + *instructions
	}
	return "<conversation>\n" + source + "\n</conversation>\n\n" + compactionPrompt + focus
}
