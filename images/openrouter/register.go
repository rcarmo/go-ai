package openrouter

import "github.com/rcarmo/go-ai/images"

func init() {
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{
		Api:            images.ImagesApiOpenRouter,
		GenerateImages: GenerateImagesOpenRouter,
	})
}
