package tools

import (
	"context"
	"github.com/rcarmo/go-ai/durable"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedToolPathExpansionAndReadVariants(t *testing.T) {
	directory := t.TempDir()
	env := LocalEnv(directory)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"~", home}, {"~/file", filepath.Join(home, "file")}, {"file://" + filepath.ToSlash(filepath.Join(directory, "file name")), filepath.Join(directory, "file name")}} {
		actual, err := env.resolve(pair[0])
		if err != nil || actual != pair[1] {
			t.Fatal(pair, actual, err)
		}
	}
	target := filepath.Join(directory, "file name")
	uri := (&url.URL{Scheme: "file", Path: target}).String()
	got, err := env.resolve(uri)
	if err != nil || got != target {
		t.Fatal("escaped URL", got, err)
	}
	malformed := "file://remote/file"
	got, err = env.resolve(malformed)
	if err != nil || got != filepath.Join(directory, malformed) {
		t.Fatal("nonlocal URL rewritten", got, err)
	}
	result, err := Write(env).Execute(context.Background(), durable.JSON{"path": "@file\u00a0name", "content": "written"}, nil)
	if err != nil {
		t.Fatal(result, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "written" {
		t.Fatal(string(data), err)
	}
	for _, filename := range []string{"shot 10.00\u202fAM.png", "someone’s.txt"} {
		if err := os.WriteFile(filepath.Join(directory, filename), []byte("variant"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, filename := range []string{"shot 10.00 AM.png", "someone's.txt"} {
		result, err := Read(env).Execute(context.Background(), durable.JSON{"path": filename}, nil)
		if err != nil || result.Content != "variant" {
			t.Fatal(filename, result, err)
		}
	}
	missing, err := env.resolveReadToolPath("missing")
	if err != nil || missing != filepath.Join(directory, "missing") {
		t.Fatal(missing, err)
	}
}
