package durable

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCodemode120TranscriptImageReferencesRejectForeignAndCorruptChunks(t *testing.T) {
	original := Document{ID: 1, Scope: "conversation", Owner: 7, Kind: codemodeImageChunkKind, Version: 1, Family: true, Key: "transcript-9-1/image/0/0", Value: JSON{"data": "YWJj"}}
	receipt := MessageReceipt{Role: goai.RoleToolResult, Content: []goai.ContentBlock{{Type: "image", MimeType: "image/png"}}, ImageChunks: []receiptImageChunks{{Index: 0, Key: "transcript-9-1", Ref: codemodeImageRef{MimeType: "image/png", Bytes: 4, Chunks: []ID{1}}}}}
	for _, kind := range []string{"valid", "foreign-conversation", "task-scope", "retired", "wrong-key", "missing", "short", "wrong-mime"} {
		t.Run(kind, func(t *testing.T) {
			doc := original
			doc.Value = JSON{"data": "YWJj"}
			switch kind {
			case "foreign-conversation":
				doc.Owner = 8
			case "task-scope":
				doc.Scope = "task"
			case "retired":
				doc.Retired = true
			case "wrong-key":
				doc.Key = "other/image/0/0"
			case "short":
				doc.Value["data"] = "YW"
			}
			state := Snapshot{Documents: map[ID]Document{1: doc}}
			if kind == "missing" {
				delete(state.Documents, 1)
			}
			copy := receipt
			copy.Content = append([]goai.ContentBlock(nil), receipt.Content...)
			if kind == "wrong-mime" {
				copy.Content[0].MimeType = "image/jpeg"
			}
			result, err := hydrateReceiptImages(copy, state, 7, DefaultLimits())
			if kind == "valid" {
				if err != nil || result.Content[0].Data != "YWJj" || len(result.ImageChunks) != 0 {
					t.Fatal(result, err)
				}
				if receipt.Content[0].Data != "" {
					t.Fatal("hydration mutated stored receipt")
				}
			} else if err == nil {
				t.Fatal("invalid image reference accepted", kind)
			}
		})
	}
}
