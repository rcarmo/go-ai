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
func Edit(env durable.FileSystem) durable.ToolRegistration {
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
			abs, err := resolveFSPath(ctx, env, path)
			if err != nil {
				return durable.ToolResult{}, err
			}
			var details durable.JSON
			if err := withFilesystemMutation(ctx, env, abs, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				info, err := env.FileInfo(ctx, abs)
				if err != nil {
					return editAccessError(path, err)
				}
				if info.Kind != "file" && info.Kind != "symlink" {
					return editCommandError(fmt.Sprintf("Could not edit file: %s. Path is not a file.", path))
				}
				content, err := env.ReadTextFile(ctx, abs)
				if err != nil {
					return editAccessError(path, err)
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				next, detail, err := applyEdits(content, edits, path)
				if err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := env.WriteFile(ctx, abs, []byte(next)); err != nil {
					return editAccessError(path, err)
				}
				if err := ctx.Err(); err != nil {
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
	index int // Original argument index, preserved across positional sorting.
}

type editFileAccessError struct {
	message string
	cause   error
}

func (err *editFileAccessError) Error() string { return err.message }
func (err *editFileAccessError) Unwrap() error { return err.cause }
func editAccessError(path string, err error) error {
	var failure *durable.FileError
	if !errors.As(err, &failure) {
		return err
	}
	return &editFileAccessError{message: fmt.Sprintf("Could not edit file: %s. Error code: %s.", path, failure.Code), cause: err}
}

type editCommandError string

func (err editCommandError) Error() string { return string(err) }

func editMissingError(path string, index, total int) error {
	if total == 1 {
		return editCommandError(fmt.Sprintf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path))
	}
	return editCommandError(fmt.Sprintf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", index, path))
}
func editDuplicateError(path string, index, total, count int) error {
	if total == 1 {
		return editCommandError(fmt.Sprintf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", count, path))
	}
	return editCommandError(fmt.Sprintf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", count, index, path))
}
func editNoChangeError(path string, total int) error {
	if total == 1 {
		return editCommandError(fmt.Sprintf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path))
	}
	return editCommandError(fmt.Sprintf("No changes made to %s. The replacements produced identical content.", path))
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
	normalised := make([]replaceEdit, len(edits))
	fuzzy := false
	for i, edit := range edits {
		edit.OldText = strings.ReplaceAll(strings.ReplaceAll(edit.OldText, "\r\n", "\n"), "\r", "\n")
		edit.NewText = strings.ReplaceAll(strings.ReplaceAll(edit.NewText, "\r\n", "\n"), "\r", "\n")
		if edit.OldText == "" {
			if len(edits) == 1 {
				return "", nil, editCommandError(fmt.Sprintf("oldText must not be empty in %s.", path))
			}
			return "", nil, editCommandError(fmt.Sprintf("edits[%d].oldText must not be empty in %s.", i, path))
		}
		normalised[i] = edit
		_, _, used := editMatch(content, edit.OldText)
		fuzzy = fuzzy || used
	}
	base := content
	if fuzzy {
		base = normalizeFuzzy(content)
	}
	matches := make([]matchedEdit, 0, len(edits))
	for i, edit := range normalised {
		edit.OldText = strings.ReplaceAll(strings.ReplaceAll(edit.OldText, "\r\n", "\n"), "\r", "\n")
		edit.NewText = strings.ReplaceAll(strings.ReplaceAll(edit.NewText, "\r\n", "\n"), "\r", "\n")
		start, length, _ := editMatch(base, edit.OldText)
		if start < 0 {
			return "", nil, editMissingError(path, i, len(edits))
		}
		needle := normalizeFuzzy(edit.OldText)
		if occurrences := strings.Count(normalizeFuzzy(base), needle); needle != "" && occurrences > 1 {
			return "", nil, editDuplicateError(path, i, len(edits), occurrences)
		}
		matches = append(matches, matchedEdit{replaceEdit: edit, start: start, end: start + length, index: i})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	for i := 1; i < len(matches); i++ {
		if matches[i].start < matches[i-1].end {
			return "", nil, editCommandError(fmt.Sprintf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.", matches[i-1].index, matches[i].index, path))
		}
	}
	next := preserveUnchangedEditLines(content, base, matches)
	if next == content {
		return "", nil, editNoChangeError(path, len(edits))
	}
	diff, patch, firstLine, err := editDiffDetails(path, content, next)
	if err != nil {
		return "", nil, err
	}
	if crlf {
		next = strings.ReplaceAll(next, "\n", "\r\n")
	}
	next = bom + next
	if next == original {
		return "", nil, editNoChangeError(path, len(edits))
	}
	return next, durable.JSON{"path": path, "edits": len(edits), "firstChangedLine": firstLine, "diff": diff, "patch": patch}, nil
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
