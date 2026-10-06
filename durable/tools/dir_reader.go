package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/rcarmo/go-ai/durable"
)

type localDirReader struct {
	mu           sync.Mutex
	file         *os.File
	path         string
	closed, done bool
}

func (e *Env) OpenDirReader(ctx context.Context, path string) (durable.DirReader, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, fileError(absolute, err)
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		file.Close()
		if err == nil {
			err = os.ErrInvalid
		}
		return nil, fileError(absolute, err)
	}
	return &localDirReader{file: file, path: absolute}, nil
}
func (r *localDirReader) Next(ctx context.Context, maximum int) (durable.DirPage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return durable.DirPage{}, err
	}
	if r.closed || maximum <= 0 {
		return durable.DirPage{}, fileError(r.path, os.ErrInvalid)
	}
	page := durable.DirPage{Entries: []durable.FileInfo{}, Done: r.done}
	if r.done {
		return page, nil
	}
	for len(page.Entries) < maximum {
		if err := ctx.Err(); err != nil {
			return durable.DirPage{}, err
		}
		entries, err := r.file.ReadDir(maximum - len(page.Entries))
		for _, entry := range entries {
			info, err := entry.Info()
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return durable.DirPage{}, fileError(r.path, err)
			}
			if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			page.Entries = append(page.Entries, localInfo(filepath.Join(r.path, entry.Name()), info))
		}
		if errors.Is(err, io.EOF) {
			r.done = true
			break
		}
		if err != nil {
			return durable.DirPage{}, fileError(r.path, err)
		}
		if len(entries) == 0 {
			r.done = true
			break
		}
	}
	page.Done = r.done
	return page, nil
}
func (r *localDirReader) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}
