package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

type cancellingMutationFS struct {
	*memoryToolFS
	cancel     context.CancelFunc
	cancelRead bool
}

func (fs *cancellingMutationFS) ReadTextFile(ctx context.Context, path string) (string, error) {
	text, err := fs.memoryToolFS.ReadTextFile(ctx, path)
	if fs.cancelRead {
		fs.cancel()
	}
	return text, err
}
func (fs *cancellingMutationFS) WriteFile(ctx context.Context, path string, data []byte) error {
	err := fs.memoryToolFS.WriteFile(ctx, path, data)
	fs.cancel()
	return err
}

func TestFileMutationRejectsCancellationObservedAfterHostReadOrWrite(t *testing.T) {
	for _, operation := range []string{"write", "edit-write", "edit-read"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fs := &cancellingMutationFS{memoryToolFS: &memoryToolFS{id: operation, files: map[string][]byte{"/virtual/file": []byte("before")}}, cancel: cancel, cancelRead: operation == "edit-read"}
			tool, args := Write(fs), durable.JSON{"path": "file", "content": "after"}
			if operation != "write" {
				tool, args = Edit(fs), durable.JSON{"path": "file", "edits": []any{map[string]any{"oldText": "before", "newText": "after"}}}
			}
			result, err := tool.Execute(ctx, args, nil)
			if !errors.Is(err, context.Canceled) || result.Content != "" {
				t.Fatal("reported success after host cancellation", result, err)
			}
			if operation == "edit-read" {
				if fs.writes != 0 || string(fs.files["/virtual/file"]) != "before" {
					t.Fatal("wrote after cancelled read", fs.writes, fs.files)
				}
			} else if fs.writes != 1 || string(fs.files["/virtual/file"]) != "after" {
				t.Fatal("host write lost", fs.writes, fs.files)
			}
		})
	}
}
