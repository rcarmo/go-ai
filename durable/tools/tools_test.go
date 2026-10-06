package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func run(t *testing.T, reg durable.ToolRegistration, args durable.JSON) durable.ToolResult {
	t.Helper()
	result, err := reg.Execute(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRegistrationsRegister(t *testing.T) {
	env := LocalEnv(t.TempDir())
	registry := durable.NewRegistry()
	for _, reg := range []durable.ToolRegistration{Read(env), Write(env), Edit(env)} {
		if err := registry.Register(reg); err != nil {
			t.Fatalf("register %s: %v", reg.Definition.Name, err)
		}
	}
}

func TestReadBoundedOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := run(t, Read(LocalEnv(dir)), durable.JSON{"path": "sample.txt", "offset": 2, "limit": 2})
	if result.Content != "two\nthree" {
		t.Fatalf("content=%q", result.Content)
	}
	if result.Details != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "" || !strings.Contains(result.Diagnostics[0].Message, "offset=4") {
		t.Fatal(result)
	}
}

func TestReadTruncatesByLinesAndBytes(t *testing.T) {
	dir := t.TempDir()
	var many strings.Builder
	for i := 0; i < MaxReadLines+10; i++ {
		many.WriteString("line\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(many.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	result := run(t, Read(LocalEnv(dir)), durable.JSON{"path": "many.txt"})
	if got := result.Details["truncation"].(durable.JSON)["truncatedBy"]; got != "lines" {
		t.Fatalf("truncatedBy=%v", got)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "offset=2001") {
		t.Fatal(result)
	}
	if count := strings.Count(result.Content, "\n"); count != MaxReadLines-1 {
		t.Fatalf("line count=%d", count)
	}

	long := strings.Repeat("é", MaxReadBytes) + "tail"
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	result = run(t, Read(LocalEnv(dir)), durable.JSON{"path": "long.txt"})
	if got := result.Details["truncation"].(durable.JSON)["truncatedBy"]; got != "bytes" {
		t.Fatalf("truncatedBy=%v", got)
	}
	if len([]byte(result.Content)) > MaxReadBytes {
		t.Fatalf("bytes=%d", len([]byte(result.Content)))
	}
}

func TestWriteCreatesParentsAndSupportsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	env := LocalEnv(dir)
	result := run(t, Write(env), durable.JSON{"path": "nested/file.txt", "content": "alpha"})
	if !strings.Contains(result.Content, "nested/file.txt") {
		t.Fatalf("content=%q", result.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "nested", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha" {
		t.Fatalf("data=%q", data)
	}
	abs := filepath.Join(dir, "absolute.txt")
	run(t, Write(env), durable.JSON{"path": abs, "content": "beta"})
	data, err = os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "beta" {
		t.Fatalf("absolute data=%q", data)
	}
}

func TestEditAppliesUniqueReplacementsAgainstOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := run(t, Edit(LocalEnv(dir)), durable.JSON{"path": "edit.txt", "edits": []any{
		map[string]any{"oldText": "alpha", "newText": "one"},
		map[string]any{"oldText": "gamma", "newText": "three"},
	}})
	if got := result.Details["firstChangedLine"]; got != 1 {
		t.Fatalf("firstChangedLine=%v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\nbeta\nthree\n" {
		t.Fatalf("data=%q", data)
	}
}

func TestEditRejectsNonUniqueAndOverlappingReplacements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("repeat repeat\naaaa\nabcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Edit(LocalEnv(dir)).Execute(context.Background(), durable.JSON{"path": "edit.txt", "edits": []any{
		map[string]any{"oldText": "repeat", "newText": "x"},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be unique") {
		t.Fatalf("err=%v", err)
	}
	_, err = Edit(LocalEnv(dir)).Execute(context.Background(), durable.JSON{"path": "edit.txt", "edits": []any{
		map[string]any{"oldText": "aa", "newText": "x"},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be unique") {
		t.Fatalf("err=%v", err)
	}
	_, err = Edit(LocalEnv(dir)).Execute(context.Background(), durable.JSON{"path": "edit.txt", "edits": []any{
		map[string]any{"oldText": "abc", "newText": "x"},
		map[string]any{"oldText": "bcd", "newText": "y"},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("err=%v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "repeat repeat\naaaa\nabcdef\n" {
		t.Fatalf("mutated=%q", data)
	}
}

func TestSerializedMutationBlocksSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		err := withSerializedMutation(path, func() error {
			close(started)
			<-release
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}()
	<-started
	go func() {
		err := withSerializedMutation(path, func() error {
			close(finished)
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-finished:
		t.Fatal("second mutation entered before first released")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("second mutation did not proceed")
	}
}

func TestConcurrentEditsOnlyOneSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	if err := os.WriteFile(path, []byte("value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := Edit(LocalEnv(dir))
	var okCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := reg.Execute(context.Background(), durable.JSON{"path": "edit.txt", "edits": []any{map[string]any{"oldText": "value", "newText": string(rune('a' + i))}}}, nil)
			if err == nil {
				okCount.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := okCount.Load(); got != 1 {
		t.Fatalf("successes=%d", got)
	}
}

func TestMutationLockCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := withSerializedMutation(path, func() error { return errors.New("boom") }); err == nil {
		t.Fatal("expected error")
	}
	mutationLocks.Lock()
	defer mutationLocks.Unlock()
	if len(mutationLocks.m) != 0 {
		t.Fatalf("locks left=%d", len(mutationLocks.m))
	}
}

func TestSpillIsPrivateAndReadKeepsLargeSource(t *testing.T) {
	directory := t.TempDir()
	env := LocalEnv(directory)
	text := strings.Repeat("large line\n", 6000)
	path, err := env.Spill(context.Background(), []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("spill permissions", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != text {
		t.Fatal("spill lost source", err)
	}
	result, err := Read(env).Execute(context.Background(), durable.JSON{"path": path}, nil)
	if err != nil || len(result.Content) > durable.MaxToolOutputBytes || result.Details["truncation"] == nil || len(result.Diagnostics) != 1 {
		t.Fatal("large source unavailable", result.Details, err)
	}
}
