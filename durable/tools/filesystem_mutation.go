package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
)

func filesystemMutationKey(ctx context.Context, fs durable.FileSystem, path string) (string, error) {
	canonical, err := fs.CanonicalPath(ctx, path)
	if err == nil {
		return canonical, nil
	}
	var failure *durable.FileError
	if !errors.As(err, &failure) || failure.Code != "not_found" && failure.Code != "not_supported" {
		return "", err
	}
	if failure.Code == "not_supported" {
		return path, nil
	}
	// Portable namespace paths use slash separators; the local implementation
	// handles its own native JoinPath representation.
	parent, err := fs.JoinPath(ctx, path, "..")
	if err != nil {
		return "", err
	}
	if parent == path {
		return path, nil
	}
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			name = path[i+1:]
			break
		}
	}
	base, err := filesystemMutationKey(ctx, fs, parent)
	if err != nil {
		return "", err
	}
	return fs.JoinPath(ctx, base, name)
}
func withFilesystemMutation(ctx context.Context, fs durable.FileSystem, path string, change func() error) error {
	canonical, err := filesystemMutationKey(ctx, fs, path)
	if err != nil {
		return err
	}
	key := fs.ID() + "\x00" + canonical
	lock := acquireMutationLock(key)
	lock.mu.Lock()
	defer func() { lock.mu.Unlock(); releaseMutationLock(key) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return change()
}
