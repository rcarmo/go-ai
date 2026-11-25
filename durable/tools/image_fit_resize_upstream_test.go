package tools_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// The exact encode(400, 100, "png") received by the pinned Photon worker.
const fitResizePNG = "iVBORw0KGgoAAAANSUhEUgAAAZAAAABkCAYAAACoy2Z3AAAEZUlEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibHHVVVddddVV/xpUi6uuuuqqq67616Jy1VVXXXXVVf96VK666qqrrrrqX4/KVVddddVVV/3rUbnqqquuuuqqfz0qV1111VVXXfWvR+Wqq6666qqr/vWoXHXVVVddddW/HpWrrrrqqquu+tejctVVV1111VX/elSuuuqqq6666l+PylVXXXXVVVf961G56qqrrrrqqn89KlddddVVV131r0flqquuuuqqq/71qFx11VVXXXXVvx6Vq6666qqrrvrXo3LVVVddddVV/3pUrrrqqquuuupfj8pVV1111VVX/etRueqqq6666qp/PSpXXXXVVVdd9a9H5aqrrrrqqqv+9ahcddVVV1111b8elauuuuqqq67616Ny1VVXXXXVVf96VK666qqrrrrqX4/KVVddddVVV/3rUbnqqquuuuqqfz0qV1111VVXXfWvR+Wqq6666qqr/vWoXHXVVVddddW/HpWrrrrqqquu+tejctVVV1111VX/elSuuuqqq6666l+PylVXXXXVVVf961G56qqrrrrqqn89KlddddVVV131r0flqquuuuqqq/71qFx11VVXXXXVvx6Vq6666qqrrvrXo3LVVVddddVV/3pUrrrqqquuuupfj8pVV1111VVX/etRueqqq6666qp/PSpXXXXVVVdd9a9H5aqrrrrqqqv+9ahcddVVV1111b8elauuuuqqq67616Ny1VVXXXXVVf96VK666qqrrrrqX4/KVVddddVVV/3rUbnqqquuuuqqfz0qV1111VVXXfWvR+Wqq6666qqr/vWoXHXVVVddddW/HpWrrrrqqquu+tejctVVV1111VX/elSuuuqqq6666l+PylVXXXXVVVf961G56qqrrrrqqn89KlddddVVV131r0flqquuuuqqq/71qFx11VVXXXXVvx6Vq6666qqrrvrXo3LVVVddddVV/3pUrrrqqquuuupfj8pVV1111VVX/etRueqqq6666qp/PSpXXXXVVVdd9a9H5aqrrrrqqqv+9ahcddVVV1111b8elauuuuqqq67616Ny1VVXXXXVVf96VK666qqrrrrqX4/KVVddddVVV/3rUbnqqquuuuqqfz0qV1111VVXXfWvR+Wqq6666qqr/vWoXHXVVVddddW/HpWrrrrqqquu+tejctVVV1111VX/elSuuuqqq6666l+PylVXXXXVVVf961G56qqrrrrqqn89KlddddVVV131r0flqquuuuqqq/71qFx11VVXXXXVvx6Vq6666qqrrvrXo3LVVVddddVV/3pUrrrqqquuuupfj8pVV1111VVX/etRueqqq6666qp/PSpXXXXVVVdd9a9H5aqrrrrqqqv+9ahcddVVV1111b8elauuuuqqq67616Ny1VVXXXXVVf96VK666qqrrrrqX49/BPl+BY/2VOLeAAAAAElFTkSuQmCC"

func TestSourceV160FitResizeOrderAndNote(t *testing.T) {
	input := []goai.ContentBlock{
		{Type: "text", Text: "before"},
		{Type: "image", MimeType: "image/png", Data: fitResizePNG},
		{Type: "text", Text: "after"},
	}
	original := append([]goai.ContentBlock(nil), input...)
	limits := tools.ImageLimits{MaxWidth: 200, MaxHeight: 2000, MaxBase64: 4718592}
	got := tools.FitImages(t.Context(), input, limits, tools.NativeImageProcessor)
	if !got.Changed { // images-fit.test.ts:106
		t.Fatal("oversized image returned unchanged")
	}
	if len(got.Content) != 3 || got.Content[0].Type != "text" || got.Content[1].Type != "image" || got.Content[2].Type != "text" { // L108
		t.Fatalf("fitted content did not preserve text/image/text order: %+v", got.Content)
	}
	// Go's ContentBlock is a value, so the original .toBe(text) identity
	// assertion (L109) maps to exact value preservation, not object identity.
	if !reflect.DeepEqual(got.Content[0], original[0]) || !reflect.DeepEqual(got.Content[2], original[2]) || !reflect.DeepEqual(input, original) {
		t.Error("fitting changed surrounding text blocks or caller input")
	}
	payload, err := base64.StdEncoding.DecodeString(got.Content[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Bounds().Size(); got != image.Pt(200, 50) { // L110
		t.Errorf("fitted dimensions = %v, want (200,50)", got)
	}
	if got.Content[1].MimeType != "image/"+format || len(got.Content[1].Data) > limits.MaxBase64 {
		t.Error("fitted payload does not satisfy its MIME or configured byte budget")
	}
	want := []string{"Resized from 400x100 to 200x50. Multiply coordinates by 2.00 to map them to the original."}
	if !reflect.DeepEqual(got.Notes, want) { // L111–113
		t.Errorf("resize notes = %q, want %q", got.Notes, want)
	}
}
