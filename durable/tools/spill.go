package tools

import (
	"context"
	"os"
	"path/filepath"
)

// Spill writes complete tool output to an owner-only file. The caller owns its
// lifetime; returning the path allows later bounded reads without losing data.
func (e *Env) Spill(ctx context.Context, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directory, err := e.resolve(".durable/output")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(directory, "output-*.txt")
	if err != nil {
		return "", err
	}
	path := file.Name()
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	success = true
	return filepath.Abs(path)
}
