package jsonparse_test

import (
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestParseCompleteJSON(t *testing.T) {
	result, ok := jsonparse.ParsePartialJSON(`{"name":"test","value":42}`)
	if !ok {
		t.Fatal("expected successful parse")
	}
	if result["name"] != "test" {
		t.Fatalf("expected name=test, got %v", result["name"])
	}
}

func TestParsePartialJSON(t *testing.T) {
	// Partial value after complete key — realistic streaming scenario
	result, ok := jsonparse.ParsePartialJSON(`{"name":"test","value":4`)
	if !ok {
		t.Fatal("expected successful partial parse")
	}
	if result["name"] != "test" {
		t.Fatalf("expected name=test, got %v", result["name"])
	}
}

func TestParseEmpty(t *testing.T) {
	_, ok := jsonparse.ParsePartialJSON("")
	if ok {
		t.Fatal("expected failure for empty string")
	}
}
