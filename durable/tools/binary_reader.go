package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/rcarmo/go-ai/durable"
)

type localBinaryReader struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	closed bool
}

func (e *Env) OpenBinaryReader(ctx context.Context, path string, noFollow bool) (durable.BinaryReader, error) {
	absolute, err := e.AbsolutePath(ctx, path)
	if err != nil {
		return nil, err
	}
	file, err := openRegularBinary(absolute, noFollow)
	if err != nil {
		return nil, fileError(absolute, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fileError(absolute, err)
	}
	if info.IsDir() {
		file.Close()
		return nil, fileError(absolute, syscall.EISDIR)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fileError(absolute, os.ErrInvalid)
	}
	return &localBinaryReader{file: file, path: absolute}, nil
}
func (r *localBinaryReader) Info(ctx context.Context) (durable.FileInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return durable.FileInfo{}, err
	}
	if r.closed {
		return durable.FileInfo{}, fileError(r.path, os.ErrClosed)
	}
	info, err := r.file.Stat()
	if err != nil {
		return durable.FileInfo{}, fileError(r.path, err)
	}
	return localInfo(r.path, info), nil
}
func (r *localBinaryReader) Read(ctx context.Context, offset int64, length int) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.closed {
		return nil, fileError(r.path, os.ErrClosed)
	}
	if offset < 0 || length < 0 {
		return nil, fileError(r.path, os.ErrInvalid)
	}
	data := make([]byte, length)
	n, err := r.file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fileError(r.path, err)
	}
	return data[:n], nil
}
func (r *localBinaryReader) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}

// utf8Size tracks TextDecoder replacement sizes across chunks without retaining
// file content. A truncated or invalid scalar counts as U+FFFD (three bytes).
type utf8Size struct {
	pending  [utf8.UTFMax]byte
	pendingN int
	bytes    int64
}

func (d *utf8Size) add(data []byte, final bool) {
	if d.pendingN > 0 {
		for len(data) > 0 && !utf8.FullRune(d.pending[:d.pendingN]) {
			d.pending[d.pendingN] = data[0]
			d.pendingN++
			data = data[1:]
		}
		if !utf8.FullRune(d.pending[:d.pendingN]) && !final {
			return
		}
		buffered, n := d.pending, d.pendingN
		d.pendingN = 0
		d.add(buffered[:n], final && len(data) == 0)
		d.add(data, final)
		return
	}
	for len(data) > 0 {
		if !utf8.FullRune(data) && !final {
			d.pendingN = copy(d.pending[:], data)
			return
		}
		rune, size := utf8.DecodeRune(data)
		if rune == utf8.RuneError && size == 1 {
			// TextDecoder replaces an incomplete valid prefix once, not once per byte.
			expected := 0
			switch {
			case data[0] >= 0xc2 && data[0] <= 0xdf:
				expected = 2
			case data[0] >= 0xe0 && data[0] <= 0xef:
				expected = 3
			case data[0] >= 0xf0 && data[0] <= 0xf4:
				expected = 4
			}
			if expected > 0 {
				size = 1
				for size < len(data) && size < expected {
					next := data[size]
					valid := next >= 0x80 && next <= 0xbf
					if size == 1 {
						valid = valid && !(data[0] == 0xe0 && next < 0xa0) && !(data[0] == 0xed && next > 0x9f) && !(data[0] == 0xf0 && next < 0x90) && !(data[0] == 0xf4 && next > 0x8f)
					}
					if !valid {
						break
					}
					size++
				}
			}
			d.bytes += 3
		} else {
			d.bytes += int64(size)
		}
		data = data[size:]
	}
}
func (r *localBinaryReader) ScanLines(ctx context.Context, startLine int64, endLine *int64) (durable.LineScan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if startLine < 0 || endLine != nil && *endLine <= startLine {
		return durable.LineScan{}, fileError(r.path, os.ErrInvalid)
	}
	if r.closed {
		return durable.LineScan{}, fileError(r.path, os.ErrClosed)
	}
	info, err := r.file.Stat()
	if err != nil {
		return durable.LineScan{}, fileError(r.path, err)
	}
	scan := durable.LineScan{Start: info.Size(), End: info.Size(), FirstLineEnd: info.Size(), LastLineStart: info.Size()}
	buffer := make([]byte, 64<<10)
	position, line, lastStart := int64(0), int64(0), int64(0)
	selected, first := utf8Size{}, utf8Size{}
	begun, ended, firstEnded := false, false, false
	bom := int64(0)
	header := make([]byte, 3)
	if n, _ := r.file.ReadAt(header, 0); n == 3 && header[0] == 0xef && header[1] == 0xbb && header[2] == 0xbf {
		bom = 3
	}
	for {
		if err := ctx.Err(); err != nil {
			return durable.LineScan{}, err
		}
		n, readErr := r.file.ReadAt(buffer, position)
		for i, b := range buffer[:n] {
			at := position + int64(i)
			if !begun && line == startLine {
				begun = true
				scan.Start = lastStart
			}
			if b == '\n' {
				scan.Newlines++
				if begun && !firstEnded {
					scan.FirstLineEnd = at
					firstEnded = true
				}
				if begun && !ended && endLine != nil && line+1 == *endLine {
					scan.End = at
					scan.LastLineStart = lastStart
					ended = true
				}
				if begun && !ended && at >= bom {
					selected.add(buffer[i:i+1], false)
				}
				line++
				lastStart = at + 1
			} else if begun && !ended && at >= bom {
				selected.add(buffer[i:i+1], false)
				if !firstEnded {
					first.add(buffer[i:i+1], false)
				}
			}
		}
		position += int64(n)
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return durable.LineScan{}, fileError(r.path, readErr)
			}
			break
		}
		if n == 0 {
			break
		}
	}
	// An empty final line is selectable, including an empty file.
	if !begun && line == startLine {
		begun = true
		scan.Start = lastStart
	}
	if begun {
		if !ended {
			scan.End = position
			scan.LastLineStart = lastStart
		}
		selected.add(nil, true)
		first.add(nil, true)
		scan.SelectedBytes = selected.bytes
		scan.FirstLineBytes = first.bytes
	}
	return scan, nil
}
