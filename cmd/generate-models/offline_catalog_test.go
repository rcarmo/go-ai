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

func TestSourceV170OfflineCatalogueChecksReleasedManifest(t *testing.T) {
	testReleasedManifest(t, "v1.7.0", "2026-11-27T16:40:10.684Z", "2026-11-27T16:40:10.685Z")
}

func TestV170OptionalMetadataProjection(t *testing.T) {
	cases := []struct {
		name, kind, api, field, metadata, want string
	}{
		{"image price absent", "image", "openrouter-images", "ImageOutput", `"cost":{}`, ""},
		{"image price zero", "image", "openrouter-images", "ImageOutput", `"cost":{"imageOutput":0}`, "ImageOutput: new(float64(0))"},
		{"image price decimal", "image", "openrouter-images", "ImageOutput", `"cost":{"imageOutput":9.820359}`, "ImageOutput: new(float64(9.820359))"},
		{"replay absent", "chat", "bedrock-converse-stream", "BedrockCompat", `"compat":{}`, ""},
		{"replay false", "chat", "bedrock-converse-stream", "BedrockCompat", `"compat":{"supportsReasoningReplay":false}`, "BedrockCompat: &BedrockCompat{SupportsReasoningReplay: boolPtr(false)"},
		{"replay true", "chat", "bedrock-converse-stream", "BedrockCompat", `"compat":{"supportsReasoningReplay":true}`, "BedrockCompat: &BedrockCompat{SupportsReasoningReplay: boolPtr(true)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var model modelEntry
			if err := json.Unmarshal([]byte(`{`+tc.metadata+`}`), &model); err != nil {
				t.Fatal(err)
			}
			model.ID, model.Provider, model.Api, model.Type = "fixture", "oracle", tc.api, tc.kind
			source := generateGoSource(map[string]map[string]modelEntry{"oracle": {"fixture": model}}, 1, tc.kind)
			if tc.want == "" && strings.Contains(source, tc.field) || tc.want != "" && !strings.Contains(source, tc.want) {
				t.Fatalf("projection does not preserve %s: want %q\n%s", tc.field, tc.want, source)
			}
		})
	}
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
