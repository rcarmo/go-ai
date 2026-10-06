package tools

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"path"
	"sync"
	"testing"
)

// Unsupported methods are inherited only to keep this fake small; read/write/
// edit must never call them or touch host files.
type memoryToolFS struct {
	durable.FileSystem
	mu     sync.Mutex
	files  map[string][]byte
	id     string
	writes int
}

func (fs *memoryToolFS) ID() string  { return fs.id }
func (fs *memoryToolFS) Cwd() string { return "/virtual" }
func (fs *memoryToolFS) AbsolutePath(ctx context.Context, p string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if path.IsAbs(p) {
		return path.Clean(p), nil
	}
	return path.Join(fs.Cwd(), p), nil
}
func (fs *memoryToolFS) JoinPath(ctx context.Context, parts ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return path.Join(parts...), nil
}
func (fs *memoryToolFS) CanonicalPath(ctx context.Context, p string) (string, error) {
	absolute, err := fs.AbsolutePath(ctx, p)
	if err != nil {
		return "", err
	}
	return absolute, nil
}
func (fs *memoryToolFS) Exists(ctx context.Context, p string) (bool, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	_, ok := fs.files[p]
	return ok, ctx.Err()
}
func (fs *memoryToolFS) ReadTextFile(ctx context.Context, p string) (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	data, ok := fs.files[p]
	if !ok {
		return "", &durable.FileError{Code: "not_found", Path: p}
	}
	return string(data), ctx.Err()
}
func (fs *memoryToolFS) ReadBinaryFile(ctx context.Context, p string) ([]byte, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	data, ok := fs.files[p]
	if !ok {
		return nil, &durable.FileError{Code: "not_found", Path: p}
	}
	return append([]byte{}, data...), ctx.Err()
}
func (fs *memoryToolFS) WriteFile(ctx context.Context, p string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.files[p] = append([]byte(nil), data...)
	fs.writes++
	return nil
}
func (fs *memoryToolFS) FileInfo(ctx context.Context, p string) (durable.FileInfo, error) {
	exists, err := fs.Exists(ctx, p)
	if err != nil {
		return durable.FileInfo{}, err
	}
	if !exists {
		return durable.FileInfo{}, &durable.FileError{Code: "not_found", Path: p}
	}
	return durable.FileInfo{Path: p, Kind: "file"}, nil
}
func TestCustomFilesystemFactoryProductionWriteEditReadNoHostIO(t *testing.T) {
	fs := &memoryToolFS{files: map[string][]byte{}, id: "memory:test"}
	registry := durable.NewRegistry()
	for _, tool := range []durable.ToolRegistration{Read(fs), Write(fs), Edit(fs)} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	api := goai.Api("custom-filesystem")
	requests := 0
	calls := []struct {
		name string
		args map[string]any
	}{{"write", map[string]any{"path": "file", "content": "hello"}}, {"edit", map[string]any{"path": "file", "edits": []any{map[string]any{"oldText": "hello", "newText": "world"}}}}, {"read", map[string]any{"path": "file"}}}
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		index := requests
		requests++
		ch := make(chan goai.Event, 1)
		message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if index < len(calls) {
			call := calls[index]
			message.StopReason = goai.StopReasonToolUse
			message.Content = []goai.ContentBlock{{Type: "toolCall", ID: call.name + "-call", Name: call.name, Arguments: call.args}}
		} else {
			last := input.Messages[len(input.Messages)-1]
			if last.Role != goai.RoleToolResult || last.Content[0].Text != "world" {
				t.Error("custom read receipt", last)
			}
		}
		ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	factoryCalls := 0
	store, _ := durable.NewMemory()
	h, err := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
		return &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api, ContextWindow: 128000, MaxTokens: 100}
	}, Env: func(context.Context, durable.EnvTarget) (durable.ExecutionEnvironment, error) {
		factoryCalls++
		return fs, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	c, err := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: goai.ProviderOpenAI, ID: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c.Submit(context.Background(), durable.Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := sub.Wait(context.Background())
	if err != nil || settled.Submission.Status != "done" || factoryCalls != 7 {
		t.Fatal(settled, err, factoryCalls)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if string(fs.files["/virtual/file"]) != "world" || fs.writes != 2 {
		t.Fatal("custom filesystem bypassed", fs.files, fs.writes)
	}
}
func TestFilesystemNamespaceLocksDoNotConflateDistinctIDs(t *testing.T) {
	fs1 := &memoryToolFS{files: map[string][]byte{}, id: "one"}
	fs2 := &memoryToolFS{files: map[string][]byte{}, id: "two"}
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withFilesystemMutation(ctx, fs1, "/same", func() error { close(entered); <-release; return nil })
	}()
	<-entered
	if err := withFilesystemMutation(ctx, fs2, "/same", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
