package tools

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type editReferenceCase struct {
	Content string `json:"content"`
	Edits   []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	} `json:"edits"`
	Result struct {
		BaseContent string `json:"baseContent"`
		NewContent  string `json:"newContent"`
	} `json:"result"`
	Display struct {
		Diff             string `json:"diff"`
		FirstChangedLine int    `json:"firstChangedLine"`
	} `json:"display"`
	Patch string `json:"patch"`
	Error string `json:"error"`
}

func loadEditReference(t *testing.T) []editReferenceCase {
	t.Helper()
	data, err := os.ReadFile("testdata/edit-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision    string              `json:"revision"`
		DiffVersion string              `json:"diffVersion"`
		Cases       []editReferenceCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Revision != "a13d35a742c6ef8462812a28fbe1d8c8b7431c32" || fixture.DiffVersion != "8.0.4" || len(fixture.Cases) != 212 {
		t.Fatal("fixture provenance")
	}
	return fixture.Cases
}
func TestEditFuzzyProductionDiffAndSymlinkTargetPreserved(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.txt")
	alias := filepath.Join(directory, "alias.txt")
	original := "keep “smart”  \nfix “quote” – here  \ntail\n"
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	result, err := Edit(LocalEnv(directory)).Execute(context.Background(), durable.JSON{"path": "alias.txt", "edits": []any{map[string]any{"oldText": "fix \"quote\" - here", "newText": "fixed"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "keep “smart”  \nfixed\ntail\n" {
		t.Fatal("symlink target stale", string(data), err)
	}
	info, err := os.Lstat(alias)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced", info, err)
	}
	if result.Details["firstChangedLine"] != 2 || !strings.Contains(result.Details["diff"].(string), "+2 fixed") || !strings.HasPrefix(result.Details["patch"].(string), "--- alias.txt\n+++ alias.txt\n") {
		t.Fatal("production diff details", result.Details)
	}
}
func TestEditFuzzyMatchesPinnedReferenceAndPreservesUntouchedLines(t *testing.T) {
	for index, c := range loadEditReference(t) {
		edits := make([]replaceEdit, len(c.Edits))
		for i, e := range c.Edits {
			edits[i] = replaceEdit{e.OldText, e.NewText}
		}
		output, details, err := applyEdits(c.Content, edits, "file")
		if c.Error != "" {
			if err == nil || err.Error() != strings.TrimPrefix(c.Error, "Error: ") {
				t.Fatalf("case%d reference error got %v want %q", index, err, c.Error)
			}
			continue
		}
		if err != nil || output != c.Result.NewContent {
			t.Fatalf("case%d: got%q want%q err%v", index, output, c.Result.NewContent, err)
		}
		if details["diff"] != c.Display.Diff || details["patch"] != c.Patch || details["firstChangedLine"] != c.Display.FirstChangedLine {
			t.Fatalf("case%d diff=%q want%q patch=%q want%q line%v want%d", index, details["diff"], c.Display.Diff, details["patch"], c.Patch, details["firstChangedLine"], c.Display.FirstChangedLine)
		}
	}
	if normalizeFuzzy("\ufeffＡ “q” – x\u00a0  \n") != "\ufeffA \"q\" - x\n" {
		t.Fatal("NFKC and explicit transforms")
	}
	if !strings.Contains(normalizeFuzzy("exact\nnext"), "\n") {
		t.Fatal("line boundary lost")
	}
}

func TestEditReferenceFilesystemDiagnosticsPreservePortableCause(t *testing.T) {
	directory := t.TempDir()
	_, err := Edit(LocalEnv(directory)).Execute(context.Background(), durable.JSON{"path": "missing", "edits": []any{map[string]any{"oldText": "a", "newText": "b"}}}, nil)
	var failure *durable.FileError
	if err == nil || err.Error() != "Could not edit file: missing. Error code: not_found." || !errors.As(err, &failure) {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err = Edit(LocalEnv(directory)).Execute(context.Background(), durable.JSON{"path": "folder", "edits": []any{map[string]any{"oldText": "a", "newText": "b"}}}, nil)
	if err == nil || err.Error() != "Could not edit file: folder. Path is not a file." {
		t.Fatal(err)
	}
}
