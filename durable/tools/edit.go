package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"io"
	"sort"
	"strings"
)

var editSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to edit (relative or absolute)"},"edits":{"type":"array","description":"One or more exact text replacements. Each edits[].oldText must match a unique, non-overlapping region of the original file.","minItems":1,"items":{"type":"object","properties":{"oldText":{"type":"string","description":"Exact text to replace once in the original file.","minLength":1},"newText":{"type":"string","description":"Replacement text for this edit."}},"required":["oldText","newText"],"additionalProperties":false}}},"required":["path","edits"],"additionalProperties":false}`)

// Edit returns a durable exact-replacement edit tool registration.
func Edit(env *Env) durable.ToolRegistration {
	return durable.ToolRegistration{
		Definition: goai.Tool{
			Name:        "edit",
			Description: "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file.",
			Parameters:  editSchema,
		},
		Implementation:   "durable.tools.edit",
		Version:          1,
		ReplaySafe:       false,
		PrepareArguments: prepareEditArguments,
		Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
			env, err := resolveToolEnv(ctx, env, api)
			if err != nil {
				return durable.ToolResult{}, err
			}
			path, ok := args["path"].(string)
			if !ok || path == "" {
				return durable.ToolResult{}, errors.New("path must be a string")
			}
			edits, err := decodeEdits(args["edits"])
			if err != nil {
				return durable.ToolResult{}, err
			}
			abs, err := env.resolveToolPath(path)
			if err != nil {
				return durable.ToolResult{}, err
			}
			var details durable.JSON
			if err := withSerializedMutation(abs, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				content, err := readTextFile(abs)
				if err != nil {
					return err
				}
				next, detail, err := applyEdits(content, edits, path)
				if err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := writeTextFileAtomic(abs, next); err != nil {
					return err
				}
				details = detail
				return nil
			}); err != nil {
				return durable.ToolResult{}, err
			}
			return durable.ToolResult{Content: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), path), Details: details}, nil
		},
	}
}

type replaceEdit struct {
	OldText string
	NewText string
}

type matchedEdit struct {
	replaceEdit
	start int
	end   int
}

func decodeEdits(value any) ([]replaceEdit, error) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil, errors.New("edits must contain at least one replacement")
	}
	edits := make([]replaceEdit, 0, len(raw))
	for i, entry := range raw {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit %d must be an object", i)
		}
		oldText, ok := m["oldText"].(string)
		if !ok || oldText == "" {
			return nil, fmt.Errorf("edit %d oldText must be a non-empty string", i)
		}
		newText, ok := m["newText"].(string)
		if !ok {
			return nil, fmt.Errorf("edit %d newText must be a string", i)
		}
		edits = append(edits, replaceEdit{OldText: oldText, NewText: newText})
	}
	return edits, nil
}

func applyEdits(content string, edits []replaceEdit, path string) (string, durable.JSON, error) {
	original := content
	bom := ""
	if strings.HasPrefix(content, "\ufeff") {
		bom = "\ufeff"
		content = strings.TrimPrefix(content, "\ufeff")
	}
	crlf := strings.Contains(content, "\r\n")
	content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\r", "\n")
	matches := make([]matchedEdit, 0, len(edits))
	for i, edit := range edits {
		edit.OldText = strings.ReplaceAll(strings.ReplaceAll(edit.OldText, "\r\n", "\n"), "\r", "\n")
		edit.NewText = strings.ReplaceAll(strings.ReplaceAll(edit.NewText, "\r\n", "\n"), "\r", "\n")
		start := strings.Index(content, edit.OldText)
		if start < 0 {
			return "", nil, fmt.Errorf("edit %d oldText not found in %s", i, path)
		}
		if next := strings.Index(content[start+1:], edit.OldText); next >= 0 {
			return "", nil, fmt.Errorf("edit %d oldText is not unique in %s", i, path)
		}
		matches = append(matches, matchedEdit{replaceEdit: edit, start: start, end: start + len(edit.OldText)})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	for i := 1; i < len(matches); i++ {
		if matches[i].start < matches[i-1].end {
			return "", nil, fmt.Errorf("edit %d overlaps another edit in %s", i, path)
		}
	}
	var out strings.Builder
	out.Grow(len(content))
	cursor := 0
	for _, match := range matches {
		out.WriteString(content[cursor:match.start])
		out.WriteString(match.NewText)
		cursor = match.end
	}
	out.WriteString(content[cursor:])
	firstLine := 1 + strings.Count(content[:matches[0].start], "\n")
	next := out.String()
	if crlf {
		next = strings.ReplaceAll(next, "\n", "\r\n")
	}
	next = bom + next
	if next == original {
		return "", nil, fmt.Errorf("no changes made to %s", path)
	}
	return next, durable.JSON{"path": path, "edits": len(edits), "firstChangedLine": firstLine}, nil
}

// Repair common legacy edit shapes on the harness's detached argument copy.
// Schema validation remains authoritative after preparation.
func prepareEditArguments(ctx context.Context, args durable.JSON) (durable.JSON, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Match pinned prepareEditArguments: repair a copy, then append the
	// legacy pair regardless of whether edits was already supplied.
	prepared := make(durable.JSON, len(args))
	for key, value := range args {
		prepared[key] = value
	}
	args = prepared
	edits := args["edits"]
	if text, ok := edits.(string); ok {
		decoder := json.NewDecoder(bytes.NewBufferString(text))
		decoder.UseNumber()
		var parsed any
		if err := decoder.Decode(&parsed); err == nil {
			var extra any
			if decoder.Decode(&extra) == io.EOF {
				if _, ok := parsed.([]any); ok {
					edits = parsed
					args["edits"] = parsed
				} else if singleEditInput(parsed) {
					edits = parsed
					args["edits"] = parsed
				}
			}
		}
	}
	if singleEditInput(edits) {
		args["edits"] = []any{edits}
	}
	old, oldOK := args["oldText"].(string)
	next, newOK := args["newText"].(string)
	if oldOK && newOK {
		array, _ := args["edits"].([]any)
		array = append(append([]any{}, array...), map[string]any{"oldText": old, "newText": next})
		args["edits"] = array
		delete(args, "oldText")
		delete(args, "newText")
	}
	return args, nil
}
func singleEditInput(value any) bool {
	var object map[string]any
	switch v := value.(type) {
	case map[string]any:
		object = v
	case durable.JSON:
		object = map[string]any(v)
	default:
		return false
	}
	_, old := object["oldText"].(string)
	_, next := object["newText"].(string)
	return old && next
}
