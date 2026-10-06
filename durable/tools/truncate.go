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
	maximumLines, maximumBytes := MaxReadLines, MaxReadBytes
	if len(options) > 0 {
		if options[0].MaxLines != nil {
			maximumLines = max(0, *options[0].MaxLines)
		}
		if options[0].MaxBytes != nil {
			maximumBytes = max(0, *options[0].MaxBytes)
		}
	}
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
		if strings.HasSuffix(content, "\n") {
			lines = lines[:len(lines)-1]
		}
	}
	result := TruncationResult{Content: content, TotalLines: len(lines), TotalBytes: len(content), OutputLines: len(lines), OutputBytes: len(content), MaxLines: maximumLines, MaxBytes: maximumBytes}
	if len(lines) <= maximumLines && len(content) <= maximumBytes {
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
	if reason != "bytes" && count == len(lines) {
		reason = "bytes"
	}
	result.Content = strings.Join(lines[:count], "\n")
	result.OutputLines = count
	result.OutputBytes = len(result.Content)
	return result
}
