package durable

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Large returned strings are immutable task-owned chunks, independent of the
// inline projection. Ordinary document/string limits remain unchanged.
func (r *TaskRuntime) persistCodemodeStringValue(ctx context.Context, key string, value any, l Limits) ([]ID, error) {
	text, ok := value.(string)
	if !ok || len(text) <= l.MaxStringBytes/2 {
		return nil, nil
	}
	if len(text) > MaxCodemodeFullOutputBytes || !utf8.ValidString(text) {
		return nil, reject("invalid codemode string value")
	}
	var ids []ID
	for offset := 0; offset < len(text); {
		end := min(len(text), offset+min(l.MaxStringBytes/2, l.MaxDocumentBytes/4))
		for end < len(text) && end > offset && !utf8.RuneStart(text[end]) {
			end--
		}
		if end <= offset {
			return nil, reject("codemode value chunk limit")
		}
		chunk := text[offset:end]
		index := len(ids)
		var id ID
		err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			address := fmt.Sprintf("%s/value/%d", key, index)
			for _, doc := range tx.state.Documents {
				if doc.Scope == "task" && doc.Owner == r.taskID && doc.Kind == codemodeImageChunkKind && doc.Key == address && !doc.Retired {
					if doc.Value["data"] != chunk {
						return nil, reject("codemode value chunk mismatch")
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
			_, err = tx.CreateDocument(Document{ID: id, Scope: "task", Owner: r.taskID, Kind: codemodeImageChunkKind, Family: true, Key: address, Version: 1, Value: JSON{"data": chunk}})
			return nil, err
		})
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		offset = end
	}
	return ids, nil
}

func readCodemodeStringValue(s Snapshot, owner ID, key string, ids []ID, l Limits) (string, error) {
	return inspectCodemodeStringValue(s, owner, key, ids, l, true)
}

// Integrity validation checks all chunks without repeatedly materialising the
// complete returned string on every unrelated commit.
func inspectCodemodeStringValue(s Snapshot, owner ID, key string, ids []ID, l Limits, materialize bool) (string, error) {
	if len(ids) == 0 || len(ids) > l.MaxPage {
		return "", reject("codemode value chunk count")
	}
	var out strings.Builder
	total := 0
	seen := map[ID]bool{}
	for i, id := range ids {
		doc, ok := s.Documents[id]
		if !ok || seen[id] || doc.Retired || doc.Scope != "task" || doc.Owner != owner || doc.Kind != codemodeImageChunkKind || !doc.Family || doc.Version != 1 || doc.Key != fmt.Sprintf("%s/value/%d", key, i) {
			return "", reject("invalid codemode value chunk")
		}
		seen[id] = true
		text, ok := doc.Value["data"].(string)
		if !ok || text == "" || !utf8.ValidString(text) || len(text) > l.MaxStringBytes/2 || total > MaxCodemodeFullOutputBytes-len(text) {
			return "", reject("invalid codemode value data")
		}
		total += len(text)
		if materialize {
			out.WriteString(text)
		}
	}
	return out.String(), nil
}
