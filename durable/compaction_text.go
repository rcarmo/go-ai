package durable

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sort"
	"strings"
	"unicode/utf8"
)

const compactionSystemPrompt = "Summarise this conversation as a context summarization assistant. Create a structured checkpoint for another assistant to continue this conversation. Do not answer questions from the conversation or continue it. Fold any earlier summary into the checkpoint."
const compactionPrompt = "Create a concise checkpoint with these sections: Goals and intent; Constraints and preferences; Progress (done, in progress, blocked); Decisions; Next steps; Critical context. Preserve exact file paths, function names and error messages."

// SerializeConversation presents model context as an inert text transcript.
// System contributions and image payloads are omitted. Tool results retain at
// most 2000 UTF-16 units, cutting only at a complete Unicode scalar in Go.
func SerializeConversation(messages []MessageReceipt, limits Limits) (string, error) {
	parts := []string{}
	for _, message := range messages {
		text := []string{}
		thinking := []string{}
		calls := []string{}
		for _, block := range message.Content {
			switch block.Type {
			case "text":
				text = append(text, block.Text)
			case "thinking":
				thinking = append(thinking, block.Thinking)
			case "toolCall":
				args := []string{}
				keys := make([]string, 0, len(block.Arguments))
				for key := range block.Arguments {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					value, err := encodeBounded(block.Arguments[key], limits, limits.MaxDocumentBytes)
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
			if len(thinking) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinking, "\n"))
			}
			if len(text) > 0 {
				parts = append(parts, "[Assistant]: "+joined)
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case goai.RoleToolResult:
			if joined != "" {
				parts = append(parts, "[Tool result]: "+truncateCompactionText(joined, 2000))
			}
		}
	}
	transcript := strings.Join(parts, "\n\n")
	if !utf8.ValidString(transcript) || len(transcript) > limits.MaxStringBytes {
		return "", reject("compaction transcript limit")
	}
	return transcript, nil
}
func truncateCompactionText(text string, maximum int) string {
	units, cut := 0, len(text)
	for offset, r := range text {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > maximum && cut == len(text) {
			cut = offset
		}
		units += width
	}
	if cut == len(text) {
		return text
	}
	kept := 0
	for _, r := range text[:cut] {
		kept++
		if r > 0xffff {
			kept++
		}
	}
	return text[:cut] + fmt.Sprintf("\n\n[... %d more characters truncated]", units-kept)
}
func compactionRequestText(source, instructions string) string {
	focus := ""
	if instructions != "" {
		focus = "\n\nAdditional focus: " + instructions
	}
	return "<conversation>\n" + source + "\n</conversation>\n\n" + compactionPrompt + focus
}
