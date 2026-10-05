package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"strings"
	"unicode/utf8"
)

const (
	MaxReadLines = 2000
	MaxReadBytes = 50 << 10
)

var readSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},"offset":{"type":"integer","description":"Line number to start reading from (1-indexed)","minimum":1},"limit":{"type":"integer","description":"Maximum number of lines to read","minimum":1,"maximum":2000}},"required":["path"],"additionalProperties":false}`)

// Read returns a durable read tool registration.
func Read(env *Env) durable.ToolRegistration {
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "read",
			Description: "Read a text file or PNG/JPEG/GIF image. Text output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
			Parameters:  readSchema,
		},
		Implementation: "durable.tools.read",
		Version:        1,
		ReplaySafe:     true,
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			env, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			if err := ctx.Err(); err != nil {
				return durable.ToolResult{}, err
			}
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return durable.ToolResult{}, errors.New("path must be a string")
			}
			abs, err := env.resolveReadToolPath(path)
			if err != nil {
				return durable.ToolResult{}, err
			}
			if isRasterImage(abs) {
				block, details, err := env.LoadImage(ctx, abs)
				if err != nil {
					return durable.ToolResult{}, err
				}
				return durable.ToolResult{Blocks: []goai.ContentBlock{block}, Details: details}, nil
			}
			text, err := readTextFile(abs)
			if err != nil {
				return durable.ToolResult{}, err
			}
			offset, err := intArg(args, "offset", 1)
			if err != nil {
				return durable.ToolResult{}, err
			}
			limit, err := intArg(args, "limit", 0)
			if err != nil {
				return durable.ToolResult{}, err
			}
			content, details, err := boundedRead(text, offset, limit)
			if err != nil {
				return durable.ToolResult{}, err
			}
			if details == nil {
				details = durable.JSON{}
			}
			// The harness receipt budget is narrower than the standalone reader.
			// Keep the complete source accessible rather than returning output_limit.
			if len(content) > durable.MaxToolOutputBytes {
				content = truncateUTF8(content, durable.MaxToolOutputBytes)
				details["truncated"] = true
				if details["truncatedBy"] == nil {
					details["truncatedBy"] = "toolLimit"
				}
				details["harnessOutputLimit"] = durable.MaxToolOutputBytes
			}
			if details["truncated"] == true {
				details["fullOutputPath"] = abs
			}
			details["path"] = path
			return durable.ToolResult{Content: content, Details: details}, nil
		},
	}
}

func boundedRead(text string, offset, limit int) (string, durable.JSON, error) {
	lines := splitLinesPreserve(text)
	if offset < 1 {
		return "", nil, errors.New("offset must be >= 1")
	}
	if limit < 0 {
		return "", nil, errors.New("limit must be >= 1")
	}
	if offset > len(lines) {
		return "", nil, fmt.Errorf("offset %d is beyond end of file (%d lines total)", offset, len(lines))
	}
	start := offset - 1
	selected := lines[start:]
	userLimited := false
	if limit > 0 && len(selected) > limit {
		selected = selected[:limit]
		userLimited = true
	}
	details := durable.JSON{}
	if len(selected) > MaxReadLines {
		selected = selected[:MaxReadLines]
		details["truncated"] = true
		details["truncatedBy"] = "lines"
		details["outputLines"] = len(selected)
		details["nextOffset"] = offset + len(selected)
	}
	content := strings.Join(selected, "")
	bytes := len([]byte(content))
	if bytes > MaxReadBytes {
		content = truncateUTF8(content, MaxReadBytes)
		details["truncated"] = true
		details["truncatedBy"] = "bytes"
		details["outputBytes"] = len([]byte(content))
		details["outputLines"] = 1 + strings.Count(content, "\n")
	}
	if len(details) == 0 && userLimited && start+len(selected) < len(lines) {
		details["nextOffset"] = offset + len(selected)
		details["remainingLines"] = len(lines) - (start + len(selected))
	}
	if len(details) == 0 {
		return content, nil, nil
	}
	if _, ok := details["outputBytes"]; !ok {
		details["outputBytes"] = len([]byte(content))
	}
	if _, ok := details["outputLines"]; !ok {
		details["outputLines"] = len(selected)
	}
	return content, details, nil
}

func splitLinesPreserve(text string) []string {
	parts := strings.SplitAfter(text, "\n")
	if len(parts) == 0 {
		return []string{""}
	}
	return parts
}

func truncateUTF8(text string, max int) string {
	if len([]byte(text)) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.ValidString(text[:cut]) {
		cut--
	}
	return text[:cut]
}
