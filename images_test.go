package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/openai"
)

func TestImageAPIProviderRegistered(t *testing.T) {
	p := goai.GetImagesApiProvider(goai.ImagesApiOpenRouter)
	if p == nil {
		t.Fatal("expected openrouter images provider to be registered")
	}
}
