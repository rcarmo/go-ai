package goai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSystemOne140ImageRequests(t *testing.T) {
	for _, tc := range []struct {
		name     string
		api      ClassifierApi
		provider ClassifierProvider
		id, mode string
		compat   *SystemOneCompat
		headers  map[string]string
		optIn    string
	}{
		{name: "typesafe-assets", api: ClassifierApiTypeSafeSystemOne, id: "vision", mode: "assets", optIn: "true"},
		{name: "openrouter-state", api: ClassifierApiTypeSafeSystemOne, provider: ClassifierProviderOpenRouter, id: "vision", mode: "state-image-urls"},
		{name: "clef-images", api: ClassifierApiCloudflareWorkersAI, id: "@cf/cloudflare/clef-flash", mode: "images"},
		{name: "cloudflare-assets", api: ClassifierApiCloudflareWorkersAI, id: "other-vision", mode: "assets"},
		{name: "compat-override", api: ClassifierApiTypeSafeSystemOne, id: "vision", mode: "images", compat: &SystemOneCompat{ImageInput: "images", SendImageOptInHeader: boolPtr(false)}},
		{name: "header-override", api: ClassifierApiTypeSafeSystemOne, id: "vision", mode: "assets", headers: map[string]string{"typesafe-danger-zone-images": "false"}, optIn: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("TypeSafe-Danger-Zone-Images"); got != tc.optIn {
					t.Errorf("image header %q want %q", got, tc.optIn)
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.api == ClassifierApiCloudflareWorkersAI {
					_, _ = w.Write([]byte(`{"success":true,"result":{"answers":{}}}`))
				} else {
					_, _ = w.Write([]byte(`{"answers":{}}`))
				}
			}))
			defer server.Close()
			model := &ClassifierModel{ID: tc.id, Api: tc.api, Provider: tc.provider, BaseURL: server.URL, Input: []string{"text", "image"}, Compat: tc.compat}
			state := map[string]any{"task": "describe"}
			input := ClassifierContext{State: state, Questions: map[string]ClassifierQuestion{}, Images: []ImageContent{{Type: "image", MimeType: "image/png", Data: "cG5n"}}}
			opts := &ClassifierOptions{APIKey: "test", Headers: tc.headers}
			out, err := Classify(model, input, opts)
			if err != nil || out.StopReason != StopReasonStop {
				t.Fatalf("result=%+v err=%v", out, err)
			}
			wire := body
			if tc.api == ClassifierApiCloudflareWorkersAI {
				wire = body["input"].(map[string]any)
			}
			want := map[string]any{"state": state, "questions": map[string]any{}}
			switch tc.mode {
			case "assets":
				want["state"] = []any{"<image-1>", state}
				want["assets"] = map[string]any{"<image-1>": map[string]any{"type": "image", "format": "png", "data_base64": "cG5n"}}
			case "images":
				want["images"] = []any{"data:image/png;base64,cG5n"}
			case "state-image-urls":
				want["state"] = []any{map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,cG5n"}}, state}
			}
			if tc.api != ClassifierApiCloudflareWorkersAI {
				want["model"] = tc.id
			}
			if !reflect.DeepEqual(wire, want) {
				t.Fatalf("wire=%#v want=%#v", wire, want)
			}
			if state["task"] != "describe" || len(opts.Headers) != len(tc.headers) {
				t.Fatal("caller input mutated")
			}
		})
	}
}

func TestSystemOne140ImageValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mode, mime string
		count            int
		input            []string
		want             string
	}{
		{"assets-webp", "assets", "image/webp", 1, []string{"image"}, "PNG and JPEG"},
		{"clef-gif", "images", "image/gif", 1, []string{"image"}, "PNG, JPEG, and WebP"},
		{"clef-too-many", "images", "image/png", 5, []string{"image"}, "at most 4"},
		{"text-only", "assets", "image/png", 1, []string{"text"}, "does not accept image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &ClassifierModel{ID: "vision", Api: ClassifierApiTypeSafeSystemOne, Input: tc.input, Compat: &SystemOneCompat{ImageInput: tc.mode}}
			input := ClassifierContext{State: map[string]any{}, Questions: map[string]ClassifierQuestion{}, Images: make([]ImageContent, tc.count)}
			for i := range input.Images {
				input.Images[i] = ImageContent{Type: "image", Data: "data", MimeType: tc.mime}
			}
			out, _ := Classify(model, input, &ClassifierOptions{APIKey: "test", OnPayload: func(map[string]any, *ClassifierModel) (map[string]any, error) {
				t.Fatal("invalid image reached payload hook")
				return nil, nil
			}})
			if out.StopReason != StopReasonError || !strings.Contains(out.ErrorMessage, tc.want) {
				t.Fatalf("%+v", out)
			}
		})
	}
}

func TestSystemOne140CompatDetached(t *testing.T) {
	original := &ClassifierModel{Compat: &SystemOneCompat{ImageInput: "images", SendImageOptInHeader: boolPtr(true)}}
	copy := cloneClassifierModel(original)
	copy.Compat.ImageInput = "assets"
	*copy.Compat.SendImageOptInHeader = false
	if original.Compat.ImageInput != "images" || !*original.Compat.SendImageOptInHeader {
		t.Fatal("compat aliases caller state")
	}
}

func TestSystemOne140CompatSchema(t *testing.T) {
	for _, mode := range []string{"assets", "images", "state-image-urls"} {
		data, err := json.Marshal(&SystemOneCompat{ImageInput: mode, SendImageOptInHeader: boolPtr(false)})
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateModelSchema("SystemOneCompatSchema", data); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	if err := ValidateModelSchema("SystemOneCompatSchema", []byte(`{"imageInput":"unsupported"}`)); err == nil {
		t.Fatal("invalid image mode accepted")
	}
}

func TestSystemOne140ImageOrderingAndDataURLLimits(t *testing.T) {
	images := []ImageContent{{MimeType: "image/jpeg", Data: "first"}, {MimeType: "image/png", Data: "second"}}
	model := &ClassifierModel{ID: "vision", Input: []string{"image"}}
	wire := map[string]any{"state": map[string]any{"task": "describe"}}
	if err := addSystemOneImages("test", model, "assets", images, wire); err != nil {
		t.Fatal(err)
	}
	if got := wire["state"].([]any); got[0] != "<image-1>" || got[1] != "<image-2>" {
		t.Fatal(got)
	}
	if wire["assets"].(map[string]any)["<image-1>"].(map[string]any)["format"] != "jpeg" {
		t.Fatal(wire)
	}
	for _, mode := range []string{"images", "state-image-urls"} {
		webp := []ImageContent{{MimeType: "image/webp", Data: "a"}, {MimeType: "image/png", Data: "b"}, {MimeType: "image/jpeg", Data: "c"}, {MimeType: "image/png", Data: "d"}}
		if err := addSystemOneImages("test", model, mode, webp, map[string]any{"state": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	if input, optIn := systemOneImageCompat(&ClassifierModel{Api: ClassifierApiTypeSafeSystemOne, BaseURL: "https://openrouter.ai/api/v1"}); input != "state-image-urls" || optIn {
		t.Fatal(input, optIn)
	}
}
