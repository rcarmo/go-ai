package durable

import (
	"context"
	"fmt"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

const codemodeImageChunkKind = "pi.codemode.image-chunk"
const MaxCodemodeImageBytes = 16 << 20  // base64 text; includes upstream 4.5MiB read results
const MaxCodemodeImagesBytes = 16 << 20 // aggregate per result

type codemodeImageRef struct {
	MimeType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
	Chunks   []ID   `json:"chunks"`
}

func (r *TaskRuntime) persistCodemodeImages(ctx context.Context, key string, images []goai.ImageContent, l Limits) ([]codemodeImageRef, error) {
	if err := validateCodemodeImages(images, l); err != nil {
		return nil, err
	}
	return r.persistImageChunks(ctx, key, images, l, "task", r.taskID)
}

func (r *TaskRuntime) persistImageChunks(ctx context.Context, key string, images []goai.ImageContent, l Limits, scope string, owner ID) ([]codemodeImageRef, error) {
	if err := validateCodemodeImages(images, l); err != nil {
		return nil, err
	}
	size := l.MaxStringBytes / 2
	if size > l.MaxDocumentBytes/2 {
		size = l.MaxDocumentBytes / 2
	}
	if size < 4 {
		return nil, reject("image chunk budget too small")
	}
	refs := make([]codemodeImageRef, 0, len(images))
	for index, image := range images {
		ref := codemodeImageRef{MimeType: image.MimeType, Bytes: len(image.Data)}
		for offset := 0; offset < len(image.Data); offset += size {
			end := offset + size
			if end > len(image.Data) {
				end = len(image.Data)
			}
			chunkKey := fmt.Sprintf("%s/image/%d/%d", key, index, offset/size)
			var id ID
			err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				for _, doc := range tx.state.Documents {
					if !doc.Retired && doc.Scope == scope && doc.Owner == owner && doc.Kind == codemodeImageChunkKind && doc.Key == chunkKey {
						if doc.Value["data"] != image.Data[offset:end] {
							return nil, reject("codemode image chunk replay diverged")
						}
						id = doc.ID
						return nil, nil
					}
				}
				var err error
				id, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				_, err = tx.CreateDocument(Document{ID: id, Scope: scope, Owner: owner, Kind: codemodeImageChunkKind, Version: 1, Family: true, Key: chunkKey, Value: JSON{"data": image.Data[offset:end]}})
				return nil, err
			})
			if err != nil {
				return nil, err
			}
			ref.Chunks = append(ref.Chunks, id)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func readCodemodeImages(state Snapshot, owner ID, key string, refs []codemodeImageRef, l Limits) ([]goai.ImageContent, error) {
	return readImageChunks(state, "task", owner, key, refs, l)
}
func readImageChunks(state Snapshot, scope string, owner ID, key string, refs []codemodeImageRef, l Limits) ([]goai.ImageContent, error) {
	if len(refs) > l.MaxPage {
		return nil, reject("codemode image reference count")
	}
	images := make([]goai.ImageContent, 0, len(refs))
	total := 0
	for index, ref := range refs {
		if ref.Bytes < 1 || ref.Bytes > MaxCodemodeImageBytes || len(ref.Chunks) == 0 || len(ref.Chunks) > l.MaxMembers {
			return nil, reject("invalid codemode image references")
		}
		total += ref.Bytes
		if total > MaxCodemodeImagesBytes {
			return nil, reject("codemode image aggregate limit")
		}
		var data strings.Builder
		data.Grow(ref.Bytes)
		for position, id := range ref.Chunks {
			doc, ok := state.Documents[id]
			if !ok || doc.Retired || doc.Scope != scope || doc.Owner != owner || doc.Kind != codemodeImageChunkKind || doc.Version != 1 || !doc.Family || doc.Key != fmt.Sprintf("%s/image/%d/%d", key, index, position) {
				return nil, reject("invalid codemode image chunk ownership")
			}
			chunk, ok := doc.Value["data"].(string)
			if !ok || len(chunk) == 0 || data.Len()+len(chunk) > ref.Bytes {
				return nil, reject("invalid codemode image chunk")
			}
			data.WriteString(chunk)
		}
		if data.Len() != ref.Bytes {
			return nil, reject("codemode image chunks incomplete")
		}
		images = append(images, goai.ImageContent{Type: "image", MimeType: ref.MimeType, Data: data.String()})
	}
	if err := validateCodemodeImages(images, l); err != nil {
		return nil, err
	}
	return images, nil
}
