package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

// Source: pi v1.6.0 packages/durable/test/tools.test.ts:201-544 (portable selection).
func TestSourceV160PortableFileTools(t *testing.T) {
	ctx := t.Context()
	seed := func(t *testing.T, name, text string) *Env {
		t.Helper()
		env := LocalEnv(t.TempDir())
		if err := env.WriteFile(ctx, name, []byte(text)); err != nil {
			t.Fatal(err)
		}
		return env
	}
	lineText := func(first, last int) string {
		lines := make([]string, 0, last-first+1)
		for n := first; n <= last; n++ {
			lines = append(lines, fmt.Sprintf("Line %d", n))
		}
		return strings.Join(lines, "\n")
	}
	run := func(t *testing.T, reg durable.ToolRegistration, args durable.JSON) durable.ToolResult {
		t.Helper()
		out, err := reg.Execute(ctx, args, nil)
		if err != nil || out.IsError {
			t.Fatalf("result=%+v error=%v", out, err)
		}
		return out
	}
	message := func(t *testing.T, out durable.ToolResult, want string) {
		t.Helper()
		if len(out.Diagnostics) != 1 || out.Diagnostics[0].Message != want {
			t.Errorf("diagnostics=%+v; want only %q", out.Diagnostics, want)
		}
	}
	truncation := func(t *testing.T, out durable.ToolResult, want durable.JSON) {
		t.Helper()
		details, ok := out.Details["truncation"].(durable.JSON)
		if !ok {
			t.Fatalf("no truncation details: %+v", out.Details)
		}
		for key, value := range want {
			if details[key] != value {
				t.Errorf("truncation.%s=%v; want %v", key, details[key], value)
			}
		}
	}
	t.Run("no-environment", func(t *testing.T) { // 201
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("read panicked instead of returning an ordinary error: %v", recovered)
			}
		}()
		_, err := Read(nil).Execute(ctx, durable.JSON{"path": "x"}, nil)
		const want = "No execution environment is configured"
		if err == nil || err.Error() != want {
			t.Errorf("error=%v; want %q", err, want)
		}
	})
	t.Run("offsets-and-limits", func(t *testing.T) { // 210
		env := seed(t, "test.txt", lineText(1, 100))
		out := run(t, Read(env), durable.JSON{"path": "test.txt", "offset": 41, "limit": 20})
		if want := lineText(41, 60); out.Content != want {
			t.Errorf("content=%q; want %q", out.Content, want)
		}
		message(t, out, "40 more lines in file. Use offset=61 to continue.")
	})
	t.Run("line-truncation", func(t *testing.T) { // 229
		out := run(t, Read(seed(t, "large.txt", lineText(1, 2500))), durable.JSON{"path": "large.txt"})
		message(t, out, "Showing lines 1-2000 of 2500. Use offset=2001 to continue.")
		if len(out.Diagnostics) != 1 || out.Diagnostics[0].Code != "truncated" {
			t.Errorf("diagnostics=%+v; want truncated", out.Diagnostics)
		}
		truncation(t, out, durable.JSON{"truncated": true, "truncatedBy": "lines", "totalLines": 2500, "outputLines": 2000})
	})
	t.Run("trailing-newline", func(t *testing.T) { // 249
		out := run(t, Read(seed(t, "exact.txt", strings.Repeat("x\n", 2000))), durable.JSON{"path": "exact.txt"})
		if out.Details != nil || !reflect.DeepEqual(out.Diagnostics, []durable.ToolDiagnostic{}) {
			t.Errorf("details=%v diagnostics=%#v; want absent details and empty diagnostic slice", out.Details, out.Diagnostics)
		}
	})
	t.Run("long-utf8-line", func(t *testing.T) { // 263
		env := seed(t, "long.txt", strings.Repeat("é", 40000)+"\nnext\n")
		out := run(t, Read(env), durable.JSON{"path": "long.txt"})
		if out.Content != strings.Repeat("é", 25600) {
			t.Errorf("unexpected prefix, bytes=%d", len(out.Content))
		}
		message(t, out, "Line 1 is 78.1KB, exceeds the 50.0KB limit; showing its first 50.0KB. Use bash: sed -n '1p' long.txt | tail -c +51201")
		truncation(t, out, durable.JSON{"truncated": true, "firstLineExceedsLimit": true, "outputBytes": 51200, "outputLines": 1})
		if details, ok := out.Details["truncation"].(durable.JSON); ok {
			if _, hasContent := details["content"]; hasContent {
				t.Error("truncation details include the payload")
			}
		}
	})
	t.Run("offset-beyond-file", func(t *testing.T) { // 282
		env := seed(t, "short.txt", "one\ntwo\nthree")
		_, err := Read(env).Execute(ctx, durable.JSON{"path": "short.txt", "offset": 100}, nil)
		if err == nil || !strings.Contains(err.Error(), "Offset 100 is beyond end of file (3 lines total)") {
			t.Errorf("error=%v", err)
		}
	})
	t.Run("growing-log", func(t *testing.T) { // 290
		local := seed(t, "app.log", "one\ntwo\n")
		env := &appendingReadEnv{Env: local}
		out := run(t, Read(env), durable.JSON{"path": "app.log", "limit": 2})
		if out.Content != "one\ntwo" || env.appends == 0 {
			t.Errorf("content=%q appends=%d", out.Content, env.appends)
		}
	})
	t.Run("write-with-parent-directories", func(t *testing.T) { // 324
		env := LocalEnv(t.TempDir())
		out := run(t, Write(env), durable.JSON{"path": "nested/dir/file.txt", "content": "hello"})
		if out.Content != "Successfully wrote to nested/dir/file.txt" {
			t.Errorf("content=%q", out.Content)
		}
		if text, err := env.ReadTextFile(ctx, "nested/dir/file.txt"); err != nil || text != "hello" {
			t.Errorf("file=%q error=%v", text, err)
		}
	})
	t.Run("edit-patch", func(t *testing.T) { // 354
		const original, edited = "alpha\nbeta\ngamma\ndelta\n", "ALPHA\nbeta\nGAMMA\ndelta\n"
		env := seed(t, "edit.txt", original)
		args := durable.JSON{"path": "edit.txt", "edits": []any{map[string]any{"oldText": "alpha\n", "newText": "ALPHA\n"}, map[string]any{"oldText": "gamma\n", "newText": "GAMMA\n"}}}
		out := run(t, Edit(env), args)
		if out.Content != "Successfully replaced 2 block(s) in edit.txt." {
			t.Errorf("content=%q", out.Content)
		}
		diff, _ := out.Details["diff"].(string)
		patch, _ := out.Details["patch"].(string)
		if !strings.Contains(diff, "ALPHA") || !strings.Contains(diff, "GAMMA") {
			t.Errorf("diff=%q", diff)
		}
		if applied := applyPortableToolFixturePatch(t, original, patch); applied != edited {
			t.Errorf("patch result=%q; want %q", applied, edited)
		}
		if text, err := env.ReadTextFile(ctx, "edit.txt"); err != nil || text != edited {
			t.Errorf("file=%q error=%v", text, err)
		}
	})
	t.Run("edit-argument-repair", func(t *testing.T) { // 377
		prepare := Edit(nil).PrepareArguments
		edit := durable.JSON{"oldText": "a", "newText": "b"}
		for _, tc := range []struct {
			input, want durable.JSON
		}{
			{durable.JSON{"path": "f", "edits": `[{"oldText":"a","newText":"b"}]`}, durable.JSON{"path": "f", "edits": []any{edit}}},
			{durable.JSON{"path": "f", "edits": `{"oldText":"a","newText":"b"}`}, durable.JSON{"path": "f", "edits": []any{edit}}},
			{durable.JSON{"path": "f", "edits": edit}, durable.JSON{"path": "f", "edits": []any{edit}}},
			{durable.JSON{"path": "f", "edits": []any{edit}, "oldText": "c", "newText": "d"}, durable.JSON{"path": "f", "edits": []any{edit, durable.JSON{"oldText": "c", "newText": "d"}}}},
			{durable.JSON{"path": "f", "edits": "not json"}, durable.JSON{"path": "f", "edits": "not json"}},
		} {
			before, _ := json.Marshal(tc.input)
			out, err := prepare(ctx, tc.input)
			got, marshalErr := json.Marshal(out)
			want, _ := json.Marshal(tc.want)
			after, _ := json.Marshal(tc.input)
			if err != nil || marshalErr != nil || !bytes.Equal(got, want) || !bytes.Equal(after, before) {
				t.Errorf("before=%s after=%s got=%s want=%s errors=%v/%v", before, after, got, want, err, marshalErr)
			}
		}
	})
	t.Run("edit-overlap", func(t *testing.T) { // 392
		const original = "one\ntwo\nthree\n"
		env := seed(t, "edit.txt", original)
		edits := []any{map[string]any{"oldText": "one\ntwo\n", "newText": "ONE\nTWO\n"}, map[string]any{"oldText": "two\nthree\n", "newText": "TWO\nTHREE\n"}}
		_, err := Edit(env).Execute(ctx, durable.JSON{"path": "edit.txt", "edits": edits}, nil)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("error=%v", err)
		}
		if text, err := env.ReadTextFile(ctx, "edit.txt"); err != nil || text != original {
			t.Errorf("file=%q error=%v", text, err)
		}
	})
	t.Run("edit-missing-and-duplicate", func(t *testing.T) { // 411
		env := seed(t, "edit.txt", "foo foo foo")
		for _, tc := range []struct{ old, want string }{{"bar", "Could not find the exact text"}, {"foo", "Found 3 occurrences"}} {
			edits := []any{map[string]any{"oldText": tc.old, "newText": "baz"}}
			_, err := Edit(env).Execute(ctx, durable.JSON{"path": "edit.txt", "edits": edits}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error=%v; want %q", err, tc.want)
			}
		}
	})
	t.Run("edit-preserves-bom-and-crlf", func(t *testing.T) { // 544
		env := seed(t, "edit.txt", "\uFEFFone\r\ntwo\r\n")
		run(t, Edit(env), durable.JSON{"path": "edit.txt", "edits": []any{map[string]any{"oldText": "two", "newText": "TWO"}}})
		if data, err := os.ReadFile(filepath.Join(env.Cwd(), "edit.txt")); err != nil || string(data) != "\uFEFFone\r\nTWO\r\n" {
			t.Errorf("file=%q error=%v", data, err)
		}
	})
}

// Reapply the single complete hunk of the four-line source fixture without
// using the production diff algorithm or invoking an external program.
func applyPortableToolFixturePatch(t *testing.T, original, patch string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	if len(lines) < 4 || !strings.HasPrefix(lines[0], "--- ") || !strings.HasPrefix(lines[1], "+++ ") || lines[2] != "@@ -1,4 +1,4 @@" {
		t.Fatalf("unexpected patch: %q", patch)
	}
	old := strings.Split(strings.TrimSuffix(original, "\n"), "\n")
	var next []string
	for _, line := range lines[3:] {
		if line == "" || !strings.ContainsRune(" +-", rune(line[0])) {
			t.Fatalf("invalid hunk row: %q", line)
		}
		if line[0] != '+' {
			if len(old) == 0 || old[0] != line[1:] {
				t.Fatalf("hunk does not match original: %q", line)
			}
			old = old[1:]
		}
		if line[0] != '-' {
			next = append(next, line[1:])
		}
	}
	if len(old) != 0 || len(next) != 4 {
		t.Fatalf("hunk did not cover four lines: remains=%v output=%v", old, next)
	}
	return strings.Join(next, "\n") + "\n"
}

type appendingReadEnv struct {
	*Env
	appends int
}

type appendingReader struct {
	durable.BinaryReader
	env  *appendingReadEnv
	path string
}

func (e *appendingReadEnv) OpenBinaryReader(ctx context.Context, path string, noFollow bool) (durable.BinaryReader, error) {
	r, err := e.Env.OpenBinaryReader(ctx, path, noFollow)
	if err != nil {
		return nil, err
	}
	return &appendingReader{BinaryReader: r, env: e, path: path}, nil
}

func (r *appendingReader) Read(ctx context.Context, at int64, length int) ([]byte, error) {
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	_, err = file.WriteString("more\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	r.env.appends++
	return r.BinaryReader.Read(ctx, at, length)
}
