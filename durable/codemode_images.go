package durable

import (
	"encoding/base64"
	goai "github.com/rcarmo/go-ai"
)

func validateCodemodeImages(images []goai.ImageContent, l Limits) error {
	if len(images) > l.MaxPage {
		return reject("codemode image output count")
	}
	for _, image := range images {
		if image.Type != "image" || image.Data == "" || len(image.Data) > l.MaxStringBytes {
			return reject("invalid codemode image output")
		}
		switch image.MimeType {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
		default:
			return reject("invalid codemode image MIME")
		}
		if _, err := base64.StdEncoding.Strict().DecodeString(image.Data); err != nil {
			return reject("invalid codemode image base64")
		}
	}
	return nil
}
