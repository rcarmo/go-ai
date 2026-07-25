package goai_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestModelsErrorPreservesCauseInMessageAndUnwrap(t *testing.T) {
	cause := errors.New("upstream exploded")
	err := goai.NewModelsError(goai.ModelsErrorAuth, "OAuth refresh failed for provider", cause)
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is failed for cause: %v", err)
	}
	if got := err.Error(); got != "OAuth refresh failed for provider: upstream exploded" {
		t.Fatalf("error message=%q", got)
	}
}

func TestModelCatalogRevalidationHeadersAndValidators(t *testing.T) {
	entry := &goai.ModelsStoreEntry{ETag: `"abc"`, LastModified: time.Date(2026, 7, 25, 18, 0, 0, 0, time.UTC).UnixMilli()}
	req, err := http.NewRequest(http.MethodGet, "https://models.example/catalog.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	goai.ApplyModelCatalogRevalidationHeaders(req, entry)
	if req.Header.Get("If-None-Match") != `"abc"` || req.Header.Get("If-Modified-Since") != "Sat, 25 Jul 2026 18:00:00 GMT" {
		t.Fatalf("headers=%#v", req.Header)
	}
	resp := &http.Response{Header: http.Header{"Etag": []string{`"def"`}, "Last-Modified": []string{"Sat, 25 Jul 2026 19:00:00 GMT"}}}
	goai.UpdateModelCatalogValidators(entry, resp)
	if entry.ETag != `"def"` || entry.LastModified != time.Date(2026, 7, 25, 19, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("entry=%#v", entry)
	}
}

func TestModelsStoreEntryPreservesETagAndLastModified(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	entry := &goai.ModelsStoreEntry{Models: []*goai.Model{{ID: "m", Provider: "p"}}, CheckedAt: 1, LastModified: 2, ETag: `"abc"`}
	if err := store.Write("p", entry); err != nil {
		t.Fatal(err)
	}
	entry.ETag = "mutated"
	got, err := store.Read("p")
	if err != nil {
		t.Fatal(err)
	}
	if got.ETag != `"abc"` || got.LastModified != 2 || got.CheckedAt != 1 {
		t.Fatalf("entry=%#v", got)
	}
}
