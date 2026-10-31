package responsestream

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
)

// SetToolNamespace preserves explicit empty strings and absent-field updates.
func SetToolNamespace(block *goai.ContentBlock, namespace *string) {
	if namespace != nil {
		block.Namespace = *namespace
		block.NamespacePresent = true
	}
}

func ApplyToolNamespace(raw []byte, block *goai.ContentBlock) {
	var item struct {
		Namespace *string `json:"namespace"`
	}
	if json.Unmarshal(raw, &item) == nil {
		SetToolNamespace(block, item.Namespace)
	}
}

// ReplayToolNamespace retains provider metadata only for the same provenance.
func ReplayToolNamespace(block goai.ContentBlock, source goai.Message, target *goai.Model) (string, bool) {
	return block.Namespace, (block.NamespacePresent || block.Namespace != "") && source.Model == target.ID && source.Provider == target.Provider && source.Api == target.Api
}
