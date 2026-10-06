package tools

import (
	"context"
	"fmt"
	"io"
	"strings"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type readerSection struct {
	ctx           context.Context
	reader        durable.BinaryReader
	position, end int64
}

func (r *readerSection) Read(data []byte) (int, error) {
	if r.position >= r.end {
		return 0, io.EOF
	}
	n := len(data)
	if int64(n) > r.end-r.position {
		n = int(r.end - r.position)
	}
	chunk, err := r.reader.Read(r.ctx, r.position, n)
	if err != nil {
		return 0, err
	}
	copy(data, chunk)
	r.position += int64(len(chunk))
	if len(chunk) == 0 {
		return 0, io.EOF
	}
	return len(chunk), nil
}

func readPositional(ctx context.Context, env durable.ExtendedFileSystem, abs, path string, args durable.JSON) (durable.ToolResult, error) {
	reader, err := env.OpenBinaryReader(ctx, abs, false)
	if err != nil {
		return durable.ToolResult{}, err
	}
	defer reader.Close(context.Background())
	for attempt := 0; attempt < 2; attempt++ {
		before, err := reader.Info(ctx)
		if err != nil {
			return durable.ToolResult{}, err
		}
		result, err := readPositionalText(ctx, reader, before.Size, path, args)
		if err != nil {
			return durable.ToolResult{}, err
		}
		after, err := reader.Info(ctx)
		if err != nil {
			return durable.ToolResult{}, err
		}
		if after.Size > before.Size || after.Size == before.Size && after.MtimeMs == before.MtimeMs {
			return result, nil
		}
	}
	return durable.ToolResult{}, fmt.Errorf("%s changed while it was read", path)
}
func readPositionalText(ctx context.Context, reader durable.BinaryReader, size int64, path string, args durable.JSON) (durable.ToolResult, error) {
	header, err := reader.Read(ctx, 0, 512)
	if err != nil {
		return durable.ToolResult{}, err
	}
	mime, err := detectImageMimeOf(ctx, reader, size, header)
	if err != nil {
		return durable.ToolResult{}, err
	}
	if mime != "" {
		return durable.ToolResult{Blocks: []goai.ContentBlock{}, IsError: true, Diagnostics: []durable.ToolDiagnostic{{Severity: "error", Code: "unsupported_image", Message: fmt.Sprintf("%s is an image (%s); reading images is not supported", path, mime)}}}, nil
	}
	offset, err := intArgMinimum(args, "offset", 1, 0)
	if err != nil {
		return durable.ToolResult{}, err
	}
	if offset < 1 {
		offset = 1
	}
	limit, err := intArgMinimum(args, "limit", 0, 0)
	if err != nil {
		return durable.ToolResult{}, err
	}
	var end *int64
	if _, ok := args["limit"]; ok {
		n := int64(offset-1) + int64(limit)
		if limit == 0 {
			n = int64(offset)
		}
		end = &n
	}
	scan, err := reader.ScanLines(ctx, int64(offset-1), end)
	if err != nil {
		return durable.ToolResult{}, err
	}
	total := int(scan.Newlines + 1)
	if offset > total {
		//lint:ignore ST1005 Exact upstream read offset diagnostic.
		return durable.ToolResult{}, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", offset, total)
	}
	selected := total - offset + 1
	if end != nil && limit < selected {
		selected = limit
	}
	section := &readerSection{ctx: ctx, reader: reader, position: scan.Start, end: scan.End}
	if section.position == 0 && len(header) >= 3 && header[0] == 0xef && header[1] == 0xbb && header[2] == 0xbf {
		section.position = 3
	}
	text := ""
	if selected > 0 {
		decoder := transform.NewReader(section, unicode.UTF8.NewDecoder())
		bytes := make([]byte, 64<<10)
		for {
			n, e := decoder.Read(bytes)
			text += string(bytes[:n])
			if e != nil {
				if e != io.EOF {
					return durable.ToolResult{}, e
				}
				break
			}
			if len(text) > MaxReadBytes+1 || strings.Count(text, "\n") >= MaxReadLines {
				break
			}
		}
	}
	totalsLines := selected
	if selected == 0 || scan.SelectedBytes == 0 {
		totalsLines = 0
	} else if scan.LastLineStart == scan.End && scan.LastLineStart > scan.Start {
		totalsLines--
	}
	totalsBytes := int(scan.SelectedBytes)
	if selected == 0 {
		totalsBytes = 0
	}
	truncation := TruncateHeadOf(text, TruncationTotals{Lines: totalsLines, Bytes: totalsBytes})
	content := truncation.Content
	result := durable.ToolResult{Content: content}
	if truncation.FirstLineExceedsLimit {
		content = truncateUTF8(strings.Split(text, "\n")[0], MaxReadBytes)
		result.Content = content
		truncation.OutputBytes = len(content)
		truncation.OutputLines = 1
		result.Diagnostics = []durable.ToolDiagnostic{{Severity: "warn", Code: "truncated", Message: fmt.Sprintf("Line %d is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d", offset, formatSize(int(scan.FirstLineBytes)), formatSize(MaxReadBytes), formatSize(len(content)), offset, path, len(content)+1)}}
	} else if truncation.Truncated {
		message := fmt.Sprintf("Showing lines %d-%d of %d", offset, offset+truncation.OutputLines-1, total)
		if truncation.TruncatedBy != nil && *truncation.TruncatedBy == "bytes" {
			message += " (50.0KB limit)"
		}
		message += fmt.Sprintf(". Use offset=%d to continue.", offset+truncation.OutputLines)
		result.Diagnostics = []durable.ToolDiagnostic{{Severity: "info", Code: "truncated", Message: message}}
	} else if end != nil && offset-1+selected < total {
		result.Diagnostics = []durable.ToolDiagnostic{{Severity: "info", Message: fmt.Sprintf("%d more lines in file. Use offset=%d to continue.", total-(offset-1+selected), offset+selected)}}
	}
	if truncation.Truncated {
		reason := any(nil)
		if truncation.TruncatedBy != nil {
			reason = *truncation.TruncatedBy
		}
		result.Details = durable.JSON{"truncation": durable.JSON{"truncated": true, "truncatedBy": reason, "maxLines": truncation.MaxLines, "maxBytes": truncation.MaxBytes, "totalLines": totalsLines, "totalBytes": totalsBytes, "outputLines": truncation.OutputLines, "outputBytes": truncation.OutputBytes, "lastLinePartial": truncation.LastLinePartial, "firstLineExceedsLimit": truncation.FirstLineExceedsLimit}}
	}
	if result.Content == "" {
		result.Blocks = []goai.ContentBlock{}
	}
	return result, nil
}
