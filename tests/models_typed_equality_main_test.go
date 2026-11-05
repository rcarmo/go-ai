package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source model-types.test.ts:72-73 compares legacy chat and real image models.
func TestMainTypedModelsAreEqual(t *testing.T) {
	chat := &goai.Model{Provider: "p", ID: "same"}
	image := &goai.ImageModel{Provider: "p", ID: "same"}
	classifier := &goai.ClassifierModel{Provider: "p", ID: "same"}
	if !goai.TypedModelsAreEqual(chat, &goai.Model{Provider: "p", ID: "same", Type: "chat"}) {
		t.Fatal("legacy and explicit chat differ")
	}
	if goai.TypedModelsAreEqual(chat, image) || goai.TypedModelsAreEqual(image, classifier) || goai.TypedModelsAreEqual(classifier, chat) {
		t.Fatal("different model types compare equal")
	}
	if !goai.TypedModelsAreEqual(image, goai.ImageModel{Provider: "p", ID: "same", Type: "image"}) ||
		!goai.TypedModelsAreEqual(classifier, &goai.ClassifierModel{Provider: "p", ID: "same", Type: "classifier"}) {
		t.Fatal("same typed identities differ")
	}
	var nilImage *goai.ImageModel
	if goai.TypedModelsAreEqual(nil, nil) || goai.TypedModelsAreEqual(nilImage, nilImage) || goai.TypedModelsAreEqual(chat, nilImage) || goai.TypedModelsAreEqual("invalid", "invalid") {
		t.Fatal("nil or unsupported models compare equal")
	}
	// Explicit type remains authoritative just as in the pinned helper.
	if !goai.TypedModelsAreEqual(image, &goai.Model{Provider: "p", ID: "same", Type: "image"}) {
		t.Fatal("same explicit type and identity differ")
	}
	if goai.TypedModelsAreEqual(image, &goai.ImageModel{Provider: "other", ID: "same"}) || goai.TypedModelsAreEqual(image, &goai.ImageModel{Provider: "p", ID: "other"}) {
		t.Fatal("different provider or ID compares equal")
	}
}
