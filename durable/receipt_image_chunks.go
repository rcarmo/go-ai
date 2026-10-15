package durable

import (
	"context"
	"fmt"

	goai "github.com/rcarmo/go-ai"
)

type receiptImageChunks struct {
	Index int              `json:"index"`
	Key   string           `json:"key"`
	Ref   codemodeImageRef `json:"ref"`
}

// Transcript chunks belong to the conversation, surviving retirement of the
// originating tool task. Stored receipts retain only bounded image placeholders.
func (r *TaskRuntime) chunkTranscriptImages(ctx context.Context, blocks []goai.ContentBlock) ([]goai.ContentBlock, []receiptImageChunks, error) {
	owned := append([]goai.ContentBlock(nil), blocks...)
	var refs []receiptImageChunks
	for index, block := range owned {
		if block.Type != "image" || len(block.Data) <= r.harness.session.limits.MaxStringBytes/2 {
			continue
		}
		key := fmt.Sprintf("transcript-%d-%d", r.taskID, index)
		images, err := r.persistImageChunks(ctx, key, []goai.ImageContent{{Type: "image", Data: block.Data, MimeType: block.MimeType}}, r.harness.session.limits, "conversation", r.conversation)
		if err != nil {
			return nil, nil, err
		}
		owned[index].Data = ""
		refs = append(refs, receiptImageChunks{Index: index, Key: key, Ref: images[0]})
	}
	return owned, refs, nil
}

func hydrateReceiptImages(receipt MessageReceipt, state Snapshot, conversation ID, l Limits) (MessageReceipt, error) {
	if len(receipt.ImageChunks) == 0 {
		return receipt, nil
	}
	receipt.Content = append([]goai.ContentBlock(nil), receipt.Content...)
	seen := map[int]bool{}
	if len(receipt.ImageChunks) > l.MaxPage {
		return MessageReceipt{}, reject("transcript image count exceeds limit")
	}
	total := 0
	for _, ref := range receipt.ImageChunks {
		if ref.Ref.Bytes < 1 || ref.Ref.Bytes > MaxCodemodeImageBytes || total > MaxCodemodeImagesBytes-ref.Ref.Bytes {
			return MessageReceipt{}, reject("transcript images exceed aggregate limit")
		}
		total += ref.Ref.Bytes
		if ref.Index < 0 || ref.Index >= len(receipt.Content) || seen[ref.Index] || receipt.Content[ref.Index].Type != "image" || receipt.Content[ref.Index].Data != "" {
			return MessageReceipt{}, reject("invalid transcript image reference")
		}
		seen[ref.Index] = true
		images, err := readImageChunks(state, "conversation", conversation, ref.Key, []codemodeImageRef{ref.Ref}, l)
		if err != nil {
			return MessageReceipt{}, err
		}
		if receipt.Content[ref.Index].MimeType != images[0].MimeType {
			return MessageReceipt{}, reject("transcript image MIME mismatch")
		}
		receipt.Content[ref.Index].Data = images[0].Data
	}
	receipt.ImageChunks = nil
	return receipt, nil
}
