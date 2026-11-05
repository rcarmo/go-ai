package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source model-types.test.ts:72–73: legacy untyped chat equals explicit chat,
// but an image identity is distinct even when provider and ID match. The
// existing ModelsAreEqual API accepts *Model only, so the image type here is a
// labelled representation adaptation, not a cross-struct API parity claim.
func TestMainModelsAreEqualHonoursType(t *testing.T) {
	legacy := &goai.Model{ID: "shared", Provider: "provider"}
	chat := &goai.Model{ID: "shared", Provider: "provider", Type: "chat"}
	image := &goai.Model{ID: "shared", Provider: "provider", Type: "image"}
	if !goai.ModelsAreEqual(legacy, chat) || !goai.ModelsAreEqual(chat, legacy) {
		t.Fatal("legacy model should compare as chat")
	}
	if goai.ModelsAreEqual(legacy, image) || goai.ModelsAreEqual(image, legacy) {
		t.Fatal("different model types must not compare equal")
	}
	if goai.ModelsAreEqual(nil, chat) || goai.ModelsAreEqual(chat, nil) {
		t.Fatal("nil models must not compare equal")
	}
	if goai.ModelsAreEqual(chat, &goai.Model{ID: "other", Provider: "provider", Type: "chat"}) ||
		goai.ModelsAreEqual(chat, &goai.Model{ID: "shared", Provider: "other", Type: "chat"}) {
		t.Fatal("different IDs or providers must not compare equal")
	}
}

func TestV100UnifiedTypedLookupAndImageCompatibility(t *testing.T) {
	goai.ClearModels()
	goai.ClearImageModels()
	goai.ClearClassifierModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	t.Cleanup(goai.RegisterBuiltinImageModels)
	t.Cleanup(goai.RegisterBuiltinClassifierModels)
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinImageModels()
	goai.RegisterBuiltinClassifierModels()

	chat := goai.GetTypedModel(goai.TypedModelRef{Type: goai.ModelTypeChat, Provider: "openai", ID: "gpt-6.1-sol"})
	chatModel, ok := chat.(*goai.Model)
	if !ok || chatModel == nil || goai.GetModelType(chatModel) != goai.ModelTypeChat || !goai.IsModelType(chatModel, goai.ModelTypeChat) {
		t.Fatalf("chat lookup=%#v", chat)
	}

	imageRef, err := goai.ParseTypedModelRef("image:openrouter/black-forest-labs/flux.2-flex")
	if err != nil {
		t.Fatalf("parse image ref: %v", err)
	}
	image := goai.GetTypedModel(imageRef)
	imageModel, ok := image.(*goai.ImageModel)
	if !ok || imageModel == nil || goai.GetModelType(imageModel) != goai.ModelTypeImage || !goai.IsModelType(imageModel, goai.ModelTypeImage) {
		t.Fatalf("image lookup=%#v", image)
	}
	if imageModel.InputLimits == nil || imageModel.InputLimits.Images == nil || imageModel.InputLimits.Images.Resize == nil || imageModel.InputLimits.Images.Resize.MaxBytes != 4718592 {
		t.Fatalf("image resize limits=%#v", imageModel.InputLimits)
	}

	classifier := goai.GetTypedModel(goai.TypedModelRef{Type: goai.ModelTypeClassifier, Provider: "typesafe", ID: "jev-latest"})
	classifierModel, ok := classifier.(*goai.ClassifierModel)
	if !ok || classifierModel == nil || goai.GetModelType(classifierModel) != goai.ModelTypeClassifier || !goai.IsModelType(classifierModel, goai.ModelTypeClassifier) {
		t.Fatalf("classifier lookup=%#v", classifier)
	}

	if got := len(goai.ListTypedModels(goai.ModelTypeChat, "")); got != 1599 {
		t.Fatalf("typed chat list=%d", got)
	}
	if got := len(goai.ListTypedModels(goai.ModelTypeImage, "")); got != 63 {
		t.Fatalf("typed image list=%d", got)
	}
	if got := len(goai.ListTypedModels(goai.ModelTypeClassifier, "")); got != 51 {
		t.Fatalf("typed classifier list=%d", got)
	}
}
