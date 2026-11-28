package tools_test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var lineContractPieces = [][]byte{
	{0x0a}, {0x0a}, {0x61}, {0x62, 0x63}, {0xef, 0xbb, 0xbf},
	{0xc3, 0xa9}, {0xe2, 0x82, 0xac}, {0xf0, 0x9f, 0x98, 0x80},
	{0xe2, 0x82}, {0xff}, {0x80}, {0xf0, 0x9f}, {0x0d, 0x0a},
}

type contractRandom struct{ state uint32 }

func (r *contractRandom) next() float64 {
	r.state += 0x6d2b79f5
	t := r.state
	t = (t ^ (t >> 15)) * (t | 1)
	t ^= t + ((t ^ (t >> 7)) * (t | 61))
	return float64((t ^ (t >> 14))) / 4294967296
}

func randomContractFile(r *contractRandom) []byte {
	count := int(r.next() * 60)
	var data []byte
	for range count {
		data = append(data, lineContractPieces[int(r.next()*float64(len(lineContractPieces)))]...)
	}
	return data
}

func decodeContractRange(data []byte, from, to int64) string {
	decoded, _, err := transform.Bytes(unicode.UTF8.NewDecoder(), data[from:to])
	if err != nil {
		panic(err)
	}
	if from == 0 && strings.HasPrefix(string(decoded), "\ufeff") {
		decoded = decoded[len("\ufeff"):]
	}
	return string(decoded)
}

func TestEnvLineScanWholeFileAgreement(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := tools.LocalEnv(dir)
	for seed := 1; seed <= 5000; seed++ {
		rng := &contractRandom{state: uint32(seed)}
		data := randomContractFile(rng)
		whole := decodeContractRange(data, 0, int64(len(data)))
		lines := strings.Split(whole, "\n")
		start := int64(rng.next() * float64(len(lines)+2))
		var end *int64
		if rng.next() >= .3 {
			value := start + 1 + int64(rng.next()*float64(len(lines)+1))
			end = &value
		}
		if err := os.WriteFile(filepath.Join(dir, "input"), data, 0600); err != nil {
			t.Fatal(err)
		}
		reader, err := env.OpenBinaryReader(ctx, "input", false)
		if err != nil {
			t.Fatal(err)
		}
		scan, err := reader.ScanLines(ctx, start, end)
		if closeErr := reader.Close(ctx); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if scan.Newlines != int64(strings.Count(whole, "\n")) {
			t.Fatalf("seed %d newline count = %d, want %d", seed, scan.Newlines, strings.Count(whole, "\n"))
		}
		selectedEnd := len(lines)
		if end != nil && *end < int64(selectedEnd) {
			selectedEnd = int(*end)
		}
		selectedStart := len(lines)
		if start < int64(selectedStart) {
			selectedStart = int(start)
		}
		selected := strings.Join(lines[selectedStart:selectedEnd], "\n")
		if got := decodeContractRange(data, scan.Start, scan.End); got != selected {
			t.Fatalf("seed %d selected text = %q, want %q", seed, got, selected)
		}
		if scan.SelectedBytes != int64(len(selected)) {
			t.Fatalf("seed %d selected bytes = %d, want %d", seed, scan.SelectedBytes, len(selected))
		}
		if start < int64(len(lines)) {
			first := lines[start]
			if got := decodeContractRange(data, scan.Start, scan.FirstLineEnd); got != first {
				t.Fatalf("seed %d first line = %q, want %q", seed, got, first)
			}
			if scan.FirstLineBytes != int64(len(first)) {
				t.Fatalf("seed %d first bytes = %d, want %d", seed, scan.FirstLineBytes, len(first))
			}
			last := selectedEnd - 1
			lastEnd := len(data)
			if last+1 < len(lines) {
				needle := int(scan.LastLineStart)
				for needle < len(data) && data[needle] != '\n' {
					needle++
				}
				lastEnd = needle
			}
			if got := decodeContractRange(data, scan.LastLineStart, int64(lastEnd)); got != lines[last] {
				t.Fatalf("seed %d last line = %q, want %q", seed, got, lines[last])
			}
		} else if scan.Start != int64(len(data)) || scan.End != int64(len(data)) || scan.SelectedBytes != 0 {
			t.Fatalf("seed %d out-of-range scan = %+v", seed, scan)
		}
	}
	reader, err := env.OpenBinaryReader(ctx, "input", false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	for _, bounds := range [][2]*int64{{contractInt64(2), contractInt64(2)}, {contractInt64(-1), nil}, {nil, contractInt64(-1)}} {
		start := int64(0)
		if bounds[0] != nil {
			start = *bounds[0]
		}
		if _, err := reader.ScanLines(ctx, start, bounds[1]); err == nil {
			t.Fatal("invalid range accepted", bounds)
		}
	}
}

func contractInt64(value int64) *int64 { return &value }

func TestEnvPublicFileContracts(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := tools.LocalEnv(dir)
	var filesystem durable.FileSystem = env
	if err := filesystem.CreateDir(ctx, "nested/child", true); err != nil {
		t.Fatal(err)
	}
	if err := filesystem.WriteFile(ctx, "nested/child/file.bin", []byte{0x61, 0xc3, 0xa9, '\n'}); err != nil {
		t.Fatal(err)
	}
	if err := filesystem.AppendFile(ctx, "nested/child/file.bin", []byte{'b'}); err != nil {
		t.Fatal(err)
	}
	if err := filesystem.TruncateFile(ctx, "nested/child/file.bin", 2); err != nil {
		t.Fatal(err)
	}
	if got, err := filesystem.ReadBinaryFile(ctx, "nested/child/file.bin"); err != nil || !reflect.DeepEqual(got, []byte{0x61, 0xc3}) {
		t.Fatal(got, err)
	}
	if err := filesystem.TruncateFile(ctx, "nested/child/file.bin", 5); err != nil {
		t.Fatal(err)
	}
	if got, err := filesystem.ReadBinaryFile(ctx, "nested/child/file.bin"); err != nil || !reflect.DeepEqual(got, []byte{0x61, 0xc3, 0, 0, 0}) {
		t.Fatal(got, err)
	}
	if err := filesystem.TruncateFile(ctx, "nested/child/file.bin", 0); err != nil {
		t.Fatal(err)
	}
	if info, err := filesystem.FileInfo(ctx, "nested/child/file.bin"); err != nil || info.Size != 0 {
		t.Fatal(info, err)
	}
	for _, size := range []int64{-1, math.MaxInt64} {
		if err := filesystem.TruncateFile(ctx, "nested/child/file.bin", size); err == nil {
			t.Fatal("accepted invalid size", size)
		}
	}
	if err := filesystem.TruncateFile(ctx, "missing", 0); err == nil {
		t.Fatal("truncated a missing file")
	} else {
		var file *durable.FileError
		if !errors.As(err, &file) || file.Code != "not_found" {
			t.Fatal(err)
		}
	}
	if err := filesystem.FlushFile(ctx, "nested/child/file.bin"); err != nil {
		t.Fatal(err)
	}
	if err := filesystem.FlushFile(ctx, "nested/child"); err == nil {
		var file *durable.FileError
		if !errors.As(err, &file) || file.Code != "is_directory" {
			t.Fatal(err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := filesystem.WriteFile(cancelled, "aborted", []byte("no")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if ok, err := filesystem.Exists(ctx, "aborted"); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestEnvShellSpillWaitsForInheritedWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("timed inherited-pipe spill contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := tools.LocalEnv(t.TempDir())
	var shell durable.Shell = env
	var output strings.Builder
	result, err := shell.Exec(ctx, "printf '%020d' 0 | tr 0 a; (sleep 0.2; printf '%01000d' 0 | tr 0 b) &", durable.ShellExecOptions{
		Spill:    &durable.ShellSpillOptions{AfterBytes: 10, AfterLines: 10},
		OnOutput: func(_ context.Context, text string) error { output.WriteString(text); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 1020 || result.SpillPath == "" {
		t.Fatalf("output=%d spill=%q", output.Len(), result.SpillPath)
	}
	if data, err := os.ReadFile(result.SpillPath); err != nil || string(data) != strings.Repeat("a", 20)+strings.Repeat("b", 1000) {
		t.Fatalf("spill length=%d err=%v", len(data), err)
	}
	if err := os.Remove(result.SpillPath); err != nil {
		t.Fatal(err)
	}
}

func TestTruncateHeadContract(t *testing.T) {
	lines, bytes := 10, 100
	result := tools.TruncateHead("aé🙂\nb", tools.TruncationOptions{MaxLines: &lines, MaxBytes: &bytes})
	if result.Truncated || result.TotalBytes != 9 || result.OutputBytes != 9 {
		t.Fatal(result)
	}
	for _, test := range []struct {
		content, want, reason string
		bytes, count          int
		first                 bool
	}{
		{"hello\nworld\n", "hello\nworld", "bytes", 11, 2, false},
		{"éé\nabc", "éé", "bytes", 4, 1, false},
		{"éé\nabc", "", "bytes", 3, 0, true},
	} {
		limit := test.bytes
		got := tools.TruncateHead(test.content, tools.TruncationOptions{MaxLines: &lines, MaxBytes: &limit})
		if got.Content != test.want || !got.Truncated || got.TruncatedBy == nil || *got.TruncatedBy != test.reason || got.OutputLines != test.count || got.FirstLineExceedsLimit != test.first {
			t.Fatal(test, got)
		}
	}
}
