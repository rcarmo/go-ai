package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

// The legacy interface path independently decodes the entire fixture. The
// positional path must return the same native results while retaining a head.
type legacyRead104 struct{ durable.FileSystem }
type countedReader104 struct {
	durable.BinaryReader
	bytes, largest int
}

func (r *countedReader104) Read(ctx context.Context, at int64, length int) ([]byte, error) {
	r.largest = max(r.largest, length)
	data, err := r.BinaryReader.Read(ctx, at, length)
	r.bytes += len(data)
	return data, err
}

type countedEnv104 struct {
	*Env
	opened *countedReader104
}

func (e *countedEnv104) OpenBinaryReader(ctx context.Context, path string, noFollow bool) (durable.BinaryReader, error) {
	reader, err := e.Env.OpenBinaryReader(ctx, path, noFollow)
	if err != nil {
		return nil, err
	}
	e.opened = &countedReader104{BinaryReader: reader}
	return e.opened, nil
}
func TestRead104BoundedProductionHeadAndDecoderBoundaries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := &countedEnv104{Env: LocalEnv(dir)}
	inputs := [][]byte{
		[]byte("\ufefffirst\n\ufeffsecond\nend"),
		append([]byte(strings.Repeat("a", 4095)), 0xe2, 0x82, 0xac, 0xff, 0xe2, 0x82),
		[]byte(strings.Repeat("short\n", MaxReadLines+3)),
		[]byte(strings.Repeat("€", (MaxReadBytes/3)+3) + "\ntail"),
		append([]byte(strings.Repeat("a", MaxReadBytes-1)), 0xff, 0xe2, 0x82),
		[]byte("\ufeff\n\ufeff\n"),
	}
	for index, data := range inputs {
		name := fmt.Sprint(index)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range []durable.JSON{{"path": name}, {"path": name, "offset": 2}, {"path": name, "limit": 1}} {
			got, gotErr := Read(env).Execute(ctx, args, nil)
			want, wantErr := Read(legacyRead104{env.Env}).Execute(ctx, args, nil)
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("case %d args=%v\ngot=%+v err=%v\nwant=%+v err=%v", index, args, got, gotErr, want, wantErr)
			}
		}
	}
	large := []byte(strings.Repeat("x", 8<<20))
	if err := os.WriteFile(filepath.Join(dir, "large"), large, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(env).Execute(ctx, durable.JSON{"path": "large"}, nil)
	if err != nil || len(got.Content) != MaxReadBytes {
		t.Fatal("bounded output", len(got.Content), err)
	}
	// ScanLines counts without retaining file content; subsequent positional reads
	// consume at most one 64KiB head plus the signature probe, regardless of size.
	if env.opened.bytes > (64<<10)+512 || env.opened.largest > 64<<10 {
		t.Fatal("unbounded head read", env.opened.bytes, env.opened.largest)
	}
}

func BenchmarkLineScan104UTF8(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "scan")
	data := []byte(strings.Repeat("€\xff\n", 1<<17))
	if err := os.WriteFile(path, data, 0600); err != nil {
		b.Fatal(err)
	}
	reader, err := LocalEnv(dir).OpenBinaryReader(context.Background(), "scan", false)
	if err != nil {
		b.Fatal(err)
	}
	defer reader.Close(context.Background())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reader.ScanLines(context.Background(), 0, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRead104ExactPrefixLineBoundaryDropsContinuationNewline(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := LocalEnv(dir)
	// transform.Reader exposes 4KiB chunks. Exactly 2000 newlines in that
	// prefix must still join only complete lines when totals imply truncation.
	data := strings.Repeat("ab\n", 96) + strings.Repeat("x\n", 1904) + "continuation"
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(env).Execute(ctx, durable.JSON{"path": "file"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, wantErr := Read(legacyRead104{env}).Execute(ctx, durable.JSON{"path": "file"}, nil)
	if wantErr != nil {
		t.Fatal(wantErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefix totals must choose whole-text truncation result: content lengths %d/%d, details %v/%v", len(got.Content), len(want.Content), got.Details, want.Details)
	}
}
