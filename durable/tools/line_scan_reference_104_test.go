package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

func TestLineScan104PinnedIndependentReferenceCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/line-scan-104.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision string
		Cases    []struct {
			Bytes []int
			Start int64
			End   *int64
			Scan  struct{ Newlines, Start, End, FirstLineEnd, LastLineStart, SelectedBytes, FirstLineBytes int64 }
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Revision != "7c10bd4337495ee613f2224843ecdf349b80d1df" || len(fixture.Cases) != 240 {
		t.Fatal("fixture provenance", fixture.Revision, len(fixture.Cases))
	}
	dir := t.TempDir()
	env := LocalEnv(dir)
	ctx := context.Background()
	for index, tc := range fixture.Cases {
		bytes := make([]byte, len(tc.Bytes))
		for i, b := range tc.Bytes {
			bytes[i] = byte(b)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), bytes, 0600); err != nil {
			t.Fatal(err)
		}
		reader, err := env.OpenBinaryReader(ctx, "file", false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := reader.ScanLines(ctx, tc.Start, tc.End)
		reader.Close(ctx)
		if err != nil {
			t.Fatal(index, err)
		}
		want := durable.LineScan{Newlines: tc.Scan.Newlines, Start: tc.Scan.Start, End: tc.Scan.End, FirstLineEnd: tc.Scan.FirstLineEnd, LastLineStart: tc.Scan.LastLineStart, SelectedBytes: tc.Scan.SelectedBytes, FirstLineBytes: tc.Scan.FirstLineBytes}
		if got != want {
			t.Fatalf("reference case %d bytes=%v start=%d end=%v got=%+v want=%+v", index, bytes, tc.Start, tc.End, got, want)
		}
	}
}
