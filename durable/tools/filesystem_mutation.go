package tools

import (
	"context"
	"errors"
	"strings"

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
	// Let this filesystem identify the parent. A filename can contain a
	// character that another filesystem treats as a separator.
	parent, err := fs.JoinPath(ctx, path, "..")
	if err != nil {
		return "", err
	}
	if parent == path || !strings.HasPrefix(path, parent) {
		return path, nil
	}
	nameStart := len(parent)
	if !strings.HasSuffix(parent, "/") && !strings.HasSuffix(parent, "\\") {
		nameStart++
	}
	name := path[nameStart:]
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
