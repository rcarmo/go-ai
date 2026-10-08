package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

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

	if got := len(goai.ListTypedModels(goai.ModelTypeChat, "")); got != 1563 {
		t.Fatalf("typed chat list=%d", got)
	}
	if got := len(goai.ListTypedModels(goai.ModelTypeImage, "")); got != 61 {
		t.Fatalf("typed image list=%d", got)
	}
	if got := len(goai.ListTypedModels(goai.ModelTypeClassifier, "")); got != 26 {
		t.Fatalf("typed classifier list=%d", got)
	}
}
