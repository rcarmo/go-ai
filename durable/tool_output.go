package durable

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
	"unicode/utf8"
)

// ToolOutputLimits bounds progress and final text independently. Zero bytes or
// lines retain no text. Native persisted receipts still cap bytes at 32 KiB.
type ToolOutputLimits struct {
	MaxBytes int    `json:"maxBytes"`
	MaxLines int    `json:"maxLines"`
	Retain   string `json:"retain"`
}

func validateOutputLimits(l ToolOutputLimits) error {
	if l.MaxBytes < 0 || l.MaxBytes > MaxToolOutputBytes || l.MaxLines < 0 || l.MaxLines > MaxToolOutputBytes || l.Retain != "head" && l.Retain != "tail" {
		return reject("invalid tool output limits")
	}
	return nil
}
func outputLines(s string) uint64 {
	if s == "" {
		return 0
	}
	n := uint64(strings.Count(s, "\n"))
	if s[len(s)-1] != '\n' {
		n++
	}
	return n
}

// boundToolText retains whole lines where possible, cutting an oversized single
// line only at a UTF-8 scalar boundary. It does not sanitize or mutate its input.
func boundToolText(text string, l ToolOutputLimits) string {
	if l.MaxBytes == 0 || l.MaxLines == 0 {
		return ""
	}
	if l.Retain == "head" {
		end := len(text)
		lines := 0
		for i := 0; i < len(text); i++ {
			if text[i] == '\n' {
				lines++
				if lines == l.MaxLines {
					end = i + 1
					break
				}
			}
		}
		if end > l.MaxBytes {
			if newline := strings.LastIndexByte(text[:l.MaxBytes], '\n'); newline >= 0 {
				end = newline + 1
			} else {
				end = l.MaxBytes
				for end > 0 && end < len(text) && text[end]&0xc0 == 0x80 {
					end--
				}
			}
		}
		return text[:end]
	}
	start, lines := 0, 1
	last := len(text) - 1
	if last >= 0 && text[last] == '\n' {
		last--
	}
	for i := last; i >= 0; i-- {
		if text[i] == '\n' {
			if lines == l.MaxLines {
				start = i + 1
				break
			}
			lines++
		}
	}
	if len(text)-start > l.MaxBytes {
		from := len(text) - l.MaxBytes
		newline := strings.IndexByte(text[from-1:], '\n')
		if newline >= 0 && from+newline < len(text) {
			start = from + newline
		} else {
			start = from
			for start < len(text) && text[start]&0xc0 == 0x80 {
				start++
			}
		}
	}
	return text[start:]
}
func sanitizeToolOutput(text string) string {
	return strings.Map(func(r rune) rune {
		if r <= 8 || r >= 11 && r <= 31 || r >= 0xfff9 && r <= 0xfffb {
			return -1
		}
		return r
	}, text)
}
func retainToolOutput(cp *toolCheckpoint, text string, l ToolOutputLimits, replace bool) error {
	if !utf8.ValidString(text) {
		return reject("invalid tool output text")
	}
	if replace {
		cp.OutputRaw = ""
		cp.OutputFull = false
		cp.OutputBytes = 0
		cp.OutputNewlines = 0
	}
	// Reject counter overflow rather than persist a wrapped count.
	n, newlines := uint64(len(text)), uint64(strings.Count(text, "\n"))
	if ^uint64(0)-cp.OutputBytes < n || ^uint64(0)-cp.OutputNewlines < newlines {
		return reject("tool output count overflow")
	}
	cp.OutputBytes += n
	cp.OutputNewlines += newlines
	if text != "" {
		cp.OutputTerminated = text[len(text)-1] == '\n'
	}
	if !cp.OutputFull {
		raw := cp.OutputRaw + text
		if l.Retain == "head" {
			cp.OutputFull = len(raw) > l.MaxBytes || strings.Count(raw, "\n") >= l.MaxLines
		}
		cp.OutputRaw = boundToolText(raw, l)
	}
	totalLines := cp.OutputNewlines
	if cp.OutputBytes > 0 && !cp.OutputTerminated {
		totalLines++
	}
	cp.DroppedBytes = cp.OutputBytes - uint64(len(cp.OutputRaw))
	cp.DroppedLines = totalLines - outputLines(cp.OutputRaw)
	cp.Output = sanitizeToolOutput(cp.OutputRaw)
	return nil
}
func boundToolBlocks(blocks []goai.ContentBlock, l ToolOutputLimits) []goai.ContentBlock {
	var text strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	joined := text.String()
	bounded := boundToolText(joined, l)
	if len(bounded) == len(joined) {
		return blocks
	}
	result := make([]goai.ContentBlock, 0, len(blocks))
	placed := false
	for i, b := range blocks {
		if b.Type != "text" {
			result = append(result, b)
			continue
		}
		if placed {
			continue
		}
		if l.Retain == "tail" {
			later := false
			for _, next := range blocks[i+1:] {
				if next.Type == "text" {
					later = true
					break
				}
			}
			if later {
				continue
			}
		}
		b.Text = bounded
		result = append(result, b)
		placed = true
	}
	return result
}
