package openai

import goai "github.com/rcarmo/go-ai"

func init() {
	goai.RegisterImagesApiProvider(&goai.ImagesApiProvider{
		Api:            goai.ImagesApiOpenRouter,
		GenerateImages: GenerateImagesOpenRouter,
	})
}
