package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/text/encoding/unicode"
	"strings"
	"unicode/utf8"
)

const (
	MaxReadLines = 2000
	MaxReadBytes = 50 << 10
)

var readSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},"offset":{"type":"integer","description":"Line number to start reading from (1-indexed)","minimum":1},"limit":{"type":"integer","description":"Maximum number of lines to read","minimum":1}},"required":["path"],"additionalProperties":false}`)

// Read returns the reference read tool, including supported inline images.
func Read(env durable.FileSystem) durable.ToolRegistration { return readTool(env, false, nil) }

// ReadWithImages additionally enables native image resizing/conversion.
func ReadWithImages(env durable.FileSystem) durable.ToolRegistration { return readTool(env, true, nil) }

// ReadWithPreparedImages opts into native bounded image preparation.
func ReadWithPreparedImages(env durable.FileSystem, limits ImageLimits) durable.ToolRegistration {
	return readTool(env, true, &limits)
}
func readTool(env durable.FileSystem, inlineImages bool, limits *ImageLimits) durable.ToolRegistration {
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "read",
			Description: "Read a text file. Text output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.",
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
			abs, err := resolveFSReadPath(ctx, env, path)
			if err != nil {
				return durable.ToolResult{}, err
			}
			// No custom filesystem path is inspected with a host file operation.
			if local, ok := env.(*Env); ok && isRasterImage(abs) {
				var block goai.ContentBlock
				var details durable.JSON
				var err error
				resolved, vision, e := invocationImageLimits(ctx, api)
				if e != nil {
					return durable.ToolResult{}, e
				}
				if !inlineImages {
					block, details, err = local.LoadInlineImage(ctx, abs, resolved)
				} else if limits == nil {
					block, details, err = local.LoadPreparedImage(ctx, abs, resolved)
				} else {
					block, details, err = local.LoadPreparedImage(ctx, abs, *limits)
				}
				if err != nil {
					if ctx.Err() != nil {
						return durable.ToolResult{}, ctx.Err()
					}
					return durable.ToolResult{Blocks: []goai.ContentBlock{}, IsError: true, Diagnostics: []durable.ToolDiagnostic{{Severity: "error", Code: "unsupported_image", Message: err.Error()}}}, nil
				}
				return imageReadResult(block, details, vision), nil
			}
			if positional, ok := env.(durable.ExtendedFileSystem); ok {
				return readPositional(ctx, positional, abs, path, args, api)
			}
			data, err := env.ReadBinaryFile(ctx, abs)
			if err != nil {
				return durable.ToolResult{}, err
			}
			if mime := detectImageMime(data); mime != "" {
				return inlineReadResult(ctx, data, api)
			}
			text, err := unicode.UTF8BOM.NewDecoder().String(string(data))
			if err != nil {
				return durable.ToolResult{}, err
			}
			offset, err := intArgMinimum(args, "offset", 1, 0)
			if err != nil {
				return durable.ToolResult{}, err
			}
			limit, err := intArgMinimum(args, "limit", 0, 0)
			if err != nil {
				return durable.ToolResult{}, err
			}
			var selectedLimit *int
			if _, present := args["limit"]; present {
				selectedLimit = &limit
			}
			if offset < 1 {
				offset = 1
			}
			content, details, err := boundedReadSelection(text, offset, selectedLimit)
			if err != nil {
				return durable.ToolResult{}, err
			}
			result := durable.ToolResult{Content: content}
			if content == "" {
				result.Blocks = []goai.ContentBlock{}
			}
			if details != nil {
				if details["truncated"] == true {
					result.Details = durable.JSON{"truncation": details}
					outputLines := details["outputLines"].(int)
					message := fmt.Sprintf("Showing lines %d-%d of %d", offset, offset+outputLines-1, len(strings.Split(text, "\n")))
					severity := "info"
					if details["truncatedBy"] == "bytes" {
						message += " (50.0KB limit)"
					}
					message += fmt.Sprintf(". Use offset=%d to continue.", offset+outputLines)
					if details["firstLineExceedsLimit"] == true {
						severity = "warn"
						message = fmt.Sprintf("Line %d is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d", offset, formatSize(len(strings.Split(text, "\n")[offset-1])), formatSize(MaxReadBytes), formatSize(len(content)), offset, path, len(content)+1)
					}
					result.Diagnostics = []durable.ToolDiagnostic{{Severity: severity, Code: "truncated", Message: message}}
				} else if next, ok := details["nextOffset"]; ok {
					result.Diagnostics = []durable.ToolDiagnostic{{Severity: "info", Message: fmt.Sprintf("%v more lines in file. Use offset=%v to continue.", details["remainingLines"], next)}}
				}
			}
			return result, nil
		},
	}
}

func boundedReadSelection(text string, offset int, limit *int) (string, durable.JSON, error) {
	lines := strings.Split(text, "\n")
	if offset < 1 {
		return "", nil, errors.New("offset must be >= 1")
	}
	if limit != nil && *limit < 0 {
		return "", nil, errors.New("limit must be >= 0")
	}
	if offset > len(lines) {
		// The capitalized diagnostic matches the reference tool.
		return "", nil, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", offset, len(lines)) //lint:ignore ST1005 reference command diagnostic
	}
	start := offset - 1
	selected := lines[start:]
	userLimited := false
	if limit != nil && len(selected) > *limit {
		selected = selected[:*limit]
		userLimited = true
	}
	truncation := TruncateHead(strings.Join(selected, "\n"))
	content := truncation.Content
	if truncation.FirstLineExceedsLimit {
		content = truncateUTF8(selected[0], MaxReadBytes)
		truncation.OutputBytes = len(content)
		truncation.OutputLines = 1
	}
	details := durable.JSON{"truncated": truncation.Truncated, "truncatedBy": nil, "maxLines": truncation.MaxLines, "maxBytes": truncation.MaxBytes, "totalLines": truncation.TotalLines, "totalBytes": truncation.TotalBytes, "outputLines": truncation.OutputLines, "outputBytes": truncation.OutputBytes, "lastLinePartial": truncation.LastLinePartial, "firstLineExceedsLimit": truncation.FirstLineExceedsLimit}
	if truncation.TruncatedBy != nil {
		details["truncatedBy"] = *truncation.TruncatedBy
	}
	if !truncation.Truncated && userLimited && start+len(selected) < len(lines) {
		details["nextOffset"] = offset + len(selected)
		details["remainingLines"] = len(lines) - (start + len(selected))
	}
	if details["truncated"] != true && !userLimited {
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
