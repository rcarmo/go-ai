package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLocalFilesystemPinnedMethodsErrorsLinesAndTempOwnership(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	env := LocalEnv(directory)
	if joined, err := env.JoinPath(ctx); err != nil || joined != "." {
		t.Fatal("empty join", joined, err)
	}
	if env.ID() != LocalEnv(t.TempDir()).ID() {
		t.Fatal("local namespace differs with cwd")
	}
	if err := env.CreateDir(ctx, "parent/child", true); err != nil {
		t.Fatal(err)
	}
	text := "\ufeffone\r\n\ntwo\npartial"
	if err := env.WriteFile(ctx, "parent/child/file", []byte(text)); err != nil {
		t.Fatal(err)
	}
	whole, err := env.ReadTextFile(ctx, "parent/child/file")
	if err != nil || whole != text {
		t.Fatal("whole file BOM", whole, err)
	}
	reader, err := env.OpenTextLineReader(ctx, "parent/child/file")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []durable.TextLine{{Text: "one\r", Terminated: true}, {Text: "", Terminated: true}, {Text: "two", Terminated: true}, {Text: "partial"}} {
		got, ok, err := reader.ReadLine(ctx)
		if err != nil || !ok || got != want {
			t.Fatal("line", got, want, ok, err)
		}
	}
	if _, ok, err := reader.ReadLine(ctx); err != nil || ok {
		t.Fatal("EOF", ok, err)
	}
	if err := reader.Close(ctx); err != nil {
		t.Fatal(err)
	}
	max := 2
	lines, err := env.ReadTextLines(ctx, "parent/child/file", &max)
	if err != nil || !reflect.DeepEqual(lines, []string{"one\r", ""}) {
		t.Fatal(lines, err)
	}
	max = 0
	lines, err = env.ReadTextLines(ctx, "parent/child/file", &max)
	if err != nil || len(lines) != 0 {
		t.Fatal(lines, err)
	}
	if err := env.AppendFile(ctx, "parent/child/file", []byte("more")); err != nil {
		t.Fatal(err)
	}
	if err := env.TruncateFile(ctx, "parent/child/file", 3); err != nil {
		t.Fatal(err)
	}
	if err := env.FlushFile(ctx, "parent/child/file"); err != nil {
		t.Fatal(err)
	}
	bytes, err := env.ReadBinaryFile(ctx, "parent/child/file")
	if err != nil || len(bytes) != 3 {
		t.Fatal(bytes, err)
	}
	if err := env.RenameFile(ctx, "parent/child/file", "parent/child/renamed"); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "alias")
	if err := os.Symlink(filepath.Join(directory, "parent/child/renamed"), alias); err != nil {
		t.Fatal(err)
	}
	info, err := env.FileInfo(ctx, alias)
	if err != nil || info.Kind != "symlink" || info.Name != "alias" {
		t.Fatal(info, err)
	}
	canonical, err := env.CanonicalPath(ctx, alias)
	expected, expectedErr := filepath.EvalSymlinks(filepath.Join(directory, "parent/child/renamed"))
	if err != nil || expectedErr != nil || canonical != expected {
		t.Fatal(canonical, err)
	}
	items, err := env.ListDir(ctx, "parent/child")
	if err != nil || len(items) != 1 || items[0].Name != "renamed" {
		t.Fatal(items, err)
	}
	dangling := filepath.Join(directory, "dangling")
	if err := os.Symlink(filepath.Join(directory, "nonexistent"), dangling); err != nil {
		t.Fatal(err)
	}
	if exists, err := env.Exists(ctx, dangling); err != nil || !exists {
		t.Fatal("dangling symlink exists", exists, err)
	}
	if ok, err := env.Exists(ctx, "missing"); err != nil || ok {
		t.Fatal(ok, err)
	}
	_, err = env.ReadTextFile(ctx, "missing")
	var failure *durable.FileError
	if !errors.As(err, &failure) || failure.Code != "not_found" {
		t.Fatal("portable error", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := env.ReadBinaryFile(cancelled, "parent/child/renamed"); !errors.Is(err, context.Canceled) {
		t.Fatal("aborted error", err)
	}
	temp, err := env.CreateTempFile(ctx, "native-", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(temp)
	if filepath.Ext(temp) != ".txt" {
		t.Fatal(temp)
	}
	tempDir, err := env.CreateTempDir(ctx, "native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	if err := env.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temp); err != nil {
		t.Fatal("cleanup deleted caller temp", err)
	}
	if err := env.Remove(ctx, "parent", true, false); err != nil {
		t.Fatal(err)
	}
	if err := env.Remove(ctx, "missing", false, true); err != nil {
		t.Fatal(err)
	}
}
func TestFilesystemNodeCompatibleInvalidUTF8AndBOM(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte{0xff, 0xff, '\n', 0xe2, 0x82}...)
	if err := env.WriteFile(ctx, "invalid", data); err != nil {
		t.Fatal(err)
	}
	text, err := env.ReadTextFile(ctx, "invalid")
	if err != nil || text != "\ufeff��\n�" {
		t.Fatal("whole file UTF8 replacement", text, err)
	}
	reader, err := env.OpenTextLineReader(ctx, "invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	first, ok, err := reader.ReadLine(ctx)
	if err != nil || !ok || first.Text != "��" || !first.Terminated {
		t.Fatal(first, err)
	}
	last, ok, err := reader.ReadLine(ctx)
	if err != nil || !ok || last.Text != "�" || last.Terminated {
		t.Fatal(last, err)
	}
}
func TestFilesystemLongLineUTF8BoundaryAndNonPositiveLimit(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	long := strings.Repeat("x", (64<<10)-1) + "🙂" + strings.Repeat("tail", 50000)
	if err := env.WriteFile(ctx, "long", []byte(long+"\nend")); err != nil {
		t.Fatal(err)
	}
	reader, err := env.OpenTextLineReader(ctx, "long")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	line, ok, err := reader.ReadLine(ctx)
	if err != nil || !ok || line.Text != long || !line.Terminated {
		t.Fatal("long/split UTF8 line", len(line.Text), ok, err)
	}
	line, ok, err = reader.ReadLine(ctx)
	if err != nil || !ok || line.Text != "end" || line.Terminated {
		t.Fatal(line, err)
	}
	limit := -1
	lines, err := env.ReadTextLines(ctx, "does-not-exist", &limit)
	if err != nil || len(lines) != 0 {
		t.Fatal("negative limit should not open file", lines, err)
	}
}
func TestFilesystemLineReaderCancellationRetainsOffset(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	if err := env.WriteFile(ctx, "lines", []byte("first\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	reader, err := env.OpenTextLineReader(ctx, "lines")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := reader.ReadLine(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	line, ok, err := reader.ReadLine(ctx)
	if err != nil || !ok || line.Text != "first" {
		t.Fatal("cancel skipped line", line, err)
	}
}
