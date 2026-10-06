package tools

import (
	"context"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadReferenceWholeLineByteTruncationAndFirstLineDiagnostic(t *testing.T) {
	dir := t.TempDir()
	env := LocalEnv(dir)
	text := "small\n" + strings.Repeat("b", MaxReadBytes+1) + "\ntail"
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Read(env).Execute(context.Background(), durable.JSON{"path": "file"}, nil)
	if err != nil || result.Content != "small" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != "info" || result.Diagnostics[0].Message != "Showing lines 1-1 of 3 (50.0KB limit). Use offset=2 to continue." {
		t.Fatal(result, err)
	}
	trunc := result.Details["truncation"].(durable.JSON)
	if trunc["outputLines"] != 1 || trunc["outputBytes"] != 5 || trunc["firstLineExceedsLimit"] != false {
		t.Fatal(trunc)
	}
	result, err = Read(env).Execute(context.Background(), durable.JSON{"path": "file", "offset": 2}, nil)
	if err != nil || len(result.Content) != MaxReadBytes || len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != "warn" || !strings.Contains(result.Diagnostics[0].Message, "tail -c +51201") {
		t.Fatal(result, err)
	}
	trunc = result.Details["truncation"].(durable.JSON)
	if trunc["firstLineExceedsLimit"] != true || trunc["outputLines"] != 1 {
		t.Fatal(trunc)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.png"), append([]byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 'I', 'H', 'D', 'R'}, make([]byte, 13)...), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = Read(env).Execute(context.Background(), durable.JSON{"path": "image.png"}, nil)
	if err != nil || !result.IsError || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unsupported_image" || len(result.Blocks) != 0 {
		t.Fatal(result, err)
	}
}

func TestReadReferenceEmptyLimitTrailingNewlineAndUTF8Diagnostics(t *testing.T) {
	dir := t.TempDir()
	env := LocalEnv(dir)
	for _, tc := range []struct {
		name, text          string
		args                durable.JSON
		content, diagnostic string
		empty               bool
	}{
		{"empty", "", durable.JSON{}, "", "", true},
		{"zero", "one\ntwo\n", durable.JSON{"limit": 0}, "", "3 more lines in file. Use offset=1 to continue.", true},
		{"offset-zero", "one\ntwo\n", durable.JSON{"offset": 0, "limit": 1}, "one", "2 more lines in file. Use offset=2 to continue.", false},
		{"newline", "one\n", durable.JSON{}, "one\n", "", false},
		{"utf8", strings.Repeat("a", MaxReadBytes-1) + "€", durable.JSON{}, strings.Repeat("a", MaxReadBytes-1), "Line 1 is 50.0KB, exceeds the 50.0KB limit; showing its first 50.0KB. Use bash: sed -n '1p' utf8 | tail -c +51200", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, tc.name), []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			tc.args["path"] = tc.name
			result, err := Read(env).Execute(context.Background(), tc.args, nil)
			if err != nil || result.Content != tc.content {
				t.Fatal(result, err)
			}
			if tc.empty && result.Blocks == nil {
				t.Fatal("explicit empty output missing", result)
			}
			if tc.diagnostic == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result)
				}
			} else if len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != tc.diagnostic {
				t.Fatal(result)
			}
			if tc.name == "utf8" {
				nested := result.Details["truncation"].(durable.JSON)
				if nested["outputBytes"] != MaxReadBytes-1 || nested["totalBytes"] != MaxReadBytes+2 || nested["outputLines"] != 1 || nested["firstLineExceedsLimit"] != true {
					t.Fatal(nested)
				}
			}
		})
	}
}
