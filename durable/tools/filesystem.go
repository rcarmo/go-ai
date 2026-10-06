package tools

import (
	"bytes"
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/text/encoding/unicode"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

func fileError(path string, err error) error {
	if err == nil {
		return nil
	}
	var known *durable.FileError
	if errors.As(err, &known) {
		return err
	}
	code := "unknown"
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		code = "aborted"
	case errors.Is(err, os.ErrNotExist):
		code = "not_found"
	case errors.Is(err, os.ErrPermission):
		code = "permission_denied"
	case errors.Is(err, syscall.ENOTDIR):
		code = "not_directory"
	case errors.Is(err, syscall.EISDIR):
		code = "is_directory"
	case errors.Is(err, os.ErrInvalid):
		code = "invalid"
	case errors.Is(err, errors.ErrUnsupported):
		code = "not_supported"
	}
	return &durable.FileError{Code: code, Path: path, Cause: err}
}
func contextFile(ctx context.Context, path string) error {
	if ctx == nil {
		return fileError(path, os.ErrInvalid)
	}
	return fileError(path, ctx.Err())
}
func (e *Env) ID() string { return "go:local" }
func (e *Env) AbsolutePath(ctx context.Context, path string) (string, error) {
	if err := contextFile(ctx, path); err != nil {
		return "", err
	}
	value, err := e.resolve(path)
	if err == nil {
		value, err = filepath.Abs(value)
	}
	return value, fileError(path, err)
}
func (e *Env) JoinPath(ctx context.Context, parts ...string) (string, error) {
	if err := contextFile(ctx, ""); err != nil {
		return "", err
	}
	if len(parts) == 0 {
		return ".", nil
	}
	return filepath.Join(parts...), nil
}
func (e *Env) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fileError(absolute, err)
	}
	if err := contextFile(ctx, absolute); err != nil {
		return nil, err
	}
	return data, nil
}
func (e *Env) ReadTextFile(ctx context.Context, path string) (string, error) {
	data, err := e.ReadBinaryFile(ctx, path)
	if err != nil {
		return "", err
	}
	decoded, err := unicode.UTF8.NewDecoder().Bytes(data)
	return string(decoded), fileError(path, err)
}

type localLineReader struct {
	mu          sync.Mutex
	file        *os.File
	path        string
	offset      int64
	data        []byte
	buffer      [64 << 10]byte
	scan        int
	closed, eof bool
}

func (e *Env) OpenTextLineReader(ctx context.Context, path string) (durable.TextLineReader, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, fileError(absolute, err)
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		file.Close()
		if err == nil {
			err = syscall.EISDIR
		}
		return nil, fileError(absolute, err)
	}
	return &localLineReader{file: file, path: absolute}, nil
}
func (r *localLineReader) ReadLine(ctx context.Context) (durable.TextLine, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := contextFile(ctx, r.path); err != nil {
		return durable.TextLine{}, false, err
	}
	if r.closed {
		return durable.TextLine{}, false, fileError(r.path, os.ErrClosed)
	}
	for {
		if relative := bytes.IndexByte(r.data[r.scan:], '\n'); relative >= 0 {
			index := r.scan + relative
			line := string(r.data[:index])
			r.data = r.data[index+1:]
			r.scan = 0
			decoded, err := unicode.UTF8.NewDecoder().String(line)
			return durable.TextLine{Text: decoded, Terminated: true}, true, fileError(r.path, err)
		}
		r.scan = len(r.data)
		if r.eof {
			if len(r.data) == 0 {
				return durable.TextLine{}, false, nil
			}
			line, err := unicode.UTF8.NewDecoder().Bytes(r.data)
			if err != nil {
				return durable.TextLine{}, false, fileError(r.path, err)
			}
			r.data = nil
			r.scan = 0
			return durable.TextLine{Text: string(line)}, true, nil
		}
		chunk := r.buffer[:]
		n, err := r.file.ReadAt(chunk, r.offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return durable.TextLine{}, false, fileError(r.path, err)
		}
		if err := contextFile(ctx, r.path); err != nil {
			return durable.TextLine{}, false, err
		}
		r.offset += int64(n)
		if r.offset == int64(n) {
			chunk = chunk[:n]
			if len(chunk) >= 3 && string(chunk[:3]) == "\ufeff" {
				chunk = chunk[3:]
				n -= 3
			}
		}
		r.data = append(r.data, chunk[:n]...)
		r.eof = errors.Is(err, io.EOF)
	}
}
func (r *localLineReader) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return fileError(r.path, r.file.Close())
}
func (e *Env) ReadTextLines(ctx context.Context, path string, max *int) ([]string, error) {
	if err := contextFile(ctx, path); err != nil {
		return nil, err
	}
	if max != nil && *max <= 0 {
		return []string{}, nil
	}
	reader, err := e.OpenTextLineReader(ctx, path)
	if err != nil {
		return nil, err
	}
	defer reader.Close(ctx)
	var lines []string
	for max == nil || len(lines) < *max {
		line, ok, err := reader.ReadLine(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		lines = append(lines, line.Text)
	}
	return lines, nil
}
func (e *Env) WriteFile(ctx context.Context, path string, data []byte) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	if err := writeTextFileAtomic(absolute, string(data)); err != nil {
		return fileError(absolute, err)
	}
	return contextFile(ctx, absolute)
}
func (e *Env) AppendFile(ctx context.Context, path string, data []byte) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return fileError(absolute, err)
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return fileError(absolute, err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fileError(absolute, err)
	}
	return contextFile(ctx, absolute)
}
func (e *Env) TruncateFile(ctx context.Context, path string, size int64) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	if size < 0 {
		return fileError(absolute, os.ErrInvalid)
	}
	if err := os.Truncate(absolute, size); err != nil {
		return fileError(absolute, err)
	}
	return contextFile(ctx, absolute)
}
func (e *Env) FlushFile(ctx context.Context, path string) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(absolute, os.O_RDWR, 0)
	if err != nil {
		return fileError(absolute, err)
	}
	syncErr := file.Sync()
	if err := errors.Join(syncErr, file.Close()); err != nil {
		return fileError(absolute, err)
	}
	return contextFile(ctx, absolute)
}
func (e *Env) RenameFile(ctx context.Context, from, to string) error {
	source, err := e.AbsolutePath(ctx, from)
	if err != nil {
		return err
	}
	target, err := e.AbsolutePath(ctx, to)
	if err != nil {
		return err
	}
	return fileError(source, os.Rename(source, target))
}
func localInfo(path string, info os.FileInfo) durable.FileInfo {
	kind := "file"
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	} else if info.IsDir() {
		kind = "directory"
	}
	return durable.FileInfo{Name: filepath.Base(path), Path: path, Kind: kind, Size: info.Size(), MtimeMs: info.ModTime().UnixMilli()}
}
func (e *Env) FileInfo(ctx context.Context, path string) (durable.FileInfo, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return durable.FileInfo{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return durable.FileInfo{}, fileError(absolute, err)
	}
	return localInfo(absolute, info), nil
}
func (e *Env) ListDir(ctx context.Context, path string) ([]durable.FileInfo, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		return nil, fileError(absolute, err)
	}
	items := make([]durable.FileInfo, 0, len(entries))
	for _, entry := range entries {
		item, err := e.FileInfo(ctx, filepath.Join(absolute, entry.Name()))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
func (e *Env) CanonicalPath(ctx context.Context, path string) (string, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	return canonical, fileError(absolute, err)
}
func (e *Env) Exists(ctx context.Context, path string) (bool, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return false, err
	}
	// Pinned exists uses lstat via fileInfo, so a dangling symlink exists.
	_, err = os.Lstat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, fileError(absolute, err)
}
func (e *Env) CreateDir(ctx context.Context, path string, recursive bool) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	if recursive {
		return fileError(absolute, os.MkdirAll(absolute, 0755))
	}
	return fileError(absolute, os.Mkdir(absolute, 0755))
}
func (e *Env) Remove(ctx context.Context, path string, recursive, force bool) error {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return err
	}
	if recursive {
		if !force {
			if _, err := os.Lstat(absolute); err != nil {
				return fileError(absolute, err)
			}
		}
		return fileError(absolute, os.RemoveAll(absolute))
	}
	err = os.Remove(absolute)
	if force && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fileError(absolute, err)
}
func (e *Env) CreateTempDir(ctx context.Context, prefix string) (string, error) {
	if err := contextFile(ctx, ""); err != nil {
		return "", err
	}
	if prefix == "" {
		prefix = "tmp-"
	}
	path, err := os.MkdirTemp("", prefix)
	return path, fileError(path, err)
}
func (e *Env) CreateTempFile(ctx context.Context, prefix, suffix string) (string, error) {
	if err := contextFile(ctx, ""); err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", prefix+"*"+suffix)
	if err != nil {
		return "", fileError("", err)
	}
	path := file.Name()
	return path, fileError(path, file.Close())
}
func (e *Env) Cleanup(context.Context) error { e.cleanupProcesses(); return nil }

var _ durable.FileSystem = (*Env)(nil)
