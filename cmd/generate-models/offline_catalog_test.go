package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The released manifest must reach per-provider integrity checks without being
// mistaken for an unsupported catalogue. Every provider in this copy is wrong.
func TestSourceV152OfflineCatalogueChecksReleasedManifest(t *testing.T) {
	testReleasedManifest(t, "v1.5.2", "2026-11-10T12:05:37.628Z", "2026-11-10T12:05:37.629Z")
}

func TestSourceV160OfflineCatalogueChecksReleasedManifest(t *testing.T) {
	testReleasedManifest(t, "v1.6.0", "2026-11-20T10:48:49.496Z", "2026-11-20T10:48:49.497Z")
}

func TestSourceV161OfflineCatalogueChecksReleasedManifest(t *testing.T) {
	testReleasedManifest(t, "v1.6.1", "2026-11-25T13:18:26.631Z", "2026-11-25T13:18:26.632Z")
}

func testReleasedManifest(t *testing.T, version, generatedAt, altered string) {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog-" + version + "-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name := range manifest.Files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOfflineCatalog(dir); err == nil || err.Error() != "provider hash mismatch: amazon-bedrock.json" {
		t.Fatal(err)
	}
	mutated := []byte(strings.Replace(string(data), generatedAt, altered, 1))
	if err := os.WriteFile(filepath.Join(dir, ".manifest.json"), mutated, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readOfflineCatalog(dir); err == nil || err.Error() != "invalid "+version+" catalog manifest digest" {
		t.Fatal(err)
	}
}
