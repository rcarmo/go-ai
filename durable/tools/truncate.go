package tools

import (
	"fmt"
	"strings"
)

// TruncationOptions uses pointers to distinguish omitted limits from zero.
type TruncationOptions struct {
	MaxLines *int
	MaxBytes *int
}
type TruncationResult struct {
	Content               string  `json:"content"`
	Truncated             bool    `json:"truncated"`
	TruncatedBy           *string `json:"truncatedBy"`
	TotalLines            int     `json:"totalLines"`
	TotalBytes            int     `json:"totalBytes"`
	OutputLines           int     `json:"outputLines"`
	OutputBytes           int     `json:"outputBytes"`
	LastLinePartial       bool    `json:"lastLinePartial"`
	FirstLineExceedsLimit bool    `json:"firstLineExceedsLimit"`
	MaxLines              int     `json:"maxLines"`
	MaxBytes              int     `json:"maxBytes"`
}

func formatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}

// TruncateHead retains complete lines, matching the pinned standalone helper.
// Inputs are UTF-8 Go strings. Negative limits retain no text.
func TruncateHead(content string, options ...TruncationOptions) TruncationResult {
	lines := countReadLines(content)
	return truncateHeadOf(content, lines, TruncationTotals{Lines: len(lines), Bytes: len(content)}, options...)
}

// TruncationTotals counts decoded UTF-8 bytes and lines, excluding the empty
// line after a trailing newline, just like TruncateHead.
type TruncationTotals struct{ Lines, Bytes int }

func countReadLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// TruncateHeadOf bounds a sufficient decoded prefix with whole-selection totals.
// Prefix must be the full text, exceed MaxBytes+1 bytes, or contain MaxLines
// newlines (using configured limits). Callers are responsible for exact totals.
func TruncateHeadOf(content string, totals TruncationTotals, options ...TruncationOptions) TruncationResult {
	return truncateHeadOf(content, countReadLines(content), totals, options...)
}

func truncateHeadOf(content string, lines []string, totals TruncationTotals, options ...TruncationOptions) TruncationResult {
	maximumLines, maximumBytes := MaxReadLines, MaxReadBytes
	if len(options) > 0 {
		if options[0].MaxLines != nil {
			maximumLines = max(0, *options[0].MaxLines)
		}
		if options[0].MaxBytes != nil {
			maximumBytes = max(0, *options[0].MaxBytes)
		}
	}
	result := TruncationResult{Content: content, TotalLines: totals.Lines, TotalBytes: totals.Bytes, OutputLines: totals.Lines, OutputBytes: totals.Bytes, MaxLines: maximumLines, MaxBytes: maximumBytes}
	if totals.Lines <= maximumLines && totals.Bytes <= maximumBytes {
		return result
	}
	result.Truncated = true
	reason := "lines"
	result.TruncatedBy = &reason
	if len(lines) > 0 && len(lines[0]) > maximumBytes {
		reason = "bytes"
		result.Content = ""
		result.OutputLines, result.OutputBytes = 0, 0
		result.FirstLineExceedsLimit = true
		return result
	}
	count, bytes := 0, 0
	for index, line := range lines {
		if index >= maximumLines {
			break
		}
		extra := len(line)
		if index > 0 {
			extra++
		}
		if bytes+extra > maximumBytes {
			reason = "bytes"
			break
		}
		bytes += extra
		count++
	}
	if reason != "bytes" && count >= totals.Lines {
		reason = "bytes"
	}
	result.Content = strings.Join(lines[:count], "\n")
	result.OutputLines = count
	result.OutputBytes = len(result.Content)
	return result
}
