package durable

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
)

// Pinned harness/{agent,live,inbox,provider,usage}.ts. These definitions are
// authority for builtin materialisation, not inferred from a stored base.
var mountedDocumentDefinitions = []*DocumentDefinition{
	builtinDocumentDefinition("pi.agent", 1, "rewindable", "asOf", nil, func(JSON) (JSON, error) { return JSON{}, nil }, func(JSON, []Operation, CheckpointInfo) (bool, error) { return true, nil }),
	builtinDocumentDefinition("pi.live", 3, "latest", "initial", migrateLiveDocument, func(JSON) (JSON, error) { return JSON{}, nil }, checkpointLiveDocument),
	builtinDocumentDefinition("pi.inbox", 1, "latest", "initial", nil, func(JSON) (JSON, error) { return JSON{"items": []any{}}, nil }, func(v JSON, _ []Operation, _ CheckpointInfo) (bool, error) {
		items, ok := v["items"].([]any)
		if !ok {
			return false, reject("invalid inbox items")
		}
		return len(items) == 0, nil
	}),
	builtinDocumentDefinition("pi.provider", 1, "latest", "initial", nil, func(JSON) (JSON, error) { return JSON{"sessionId": goai.UUIDv7()}, nil }, func(JSON, []Operation, CheckpointInfo) (bool, error) { return true, nil }),
	builtinDocumentDefinition("pi.usage", 1, "latest", "initial", nil, func(JSON) (JSON, error) { return JSON{"models": JSON{}, "tools": JSON{}}, nil }, func(JSON, []Operation, CheckpointInfo) (bool, error) { return true, nil }),
}

func builtinDocumentDefinition(kind string, version uint64, history, fork string, migrate func(JSON, uint64) (JSON, error), initial func(JSON) (JSON, error), checkpoint func(JSON, []Operation, CheckpointInfo) (bool, error)) *DocumentDefinition {
	return &DocumentDefinition{options: DefinitionOptions{Kind: kind, Version: version, Scope: "conversation", History: history, Fork: fork, Initial: initial, Migrate: migrate, CheckpointWhen: checkpoint}}
}
func migrateLiveDocument(value JSON, _ uint64) (JSON, error) {
	if raw, ok := value["run"]; ok {
		run, ok := raw.(map[string]any)
		if !ok {
			return nil, reject("invalid live run")
		}
		// Source checks typeof number, not an ID/rational-range predicate.
		// definitionValue owns/normalises stored numbers before migration.
		if _, number := run["turns"].(json.Number); !number {
			run["turns"] = 0
		}
	}
	rename := func(raw any) (any, error) {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, reject("invalid live compaction")
		}
		if reason, ok := object["reason"]; ok {
			object["trigger"] = reason
			delete(object, "reason")
		}
		return object, nil
	}
	if raw, ok := value["compactions"]; ok {
		items, ok := raw.([]any)
		if !ok {
			return nil, reject("invalid live compactions")
		}
		for i, item := range items {
			v, err := rename(item)
			if err != nil {
				return nil, err
			}
			items[i] = v
		}
	}
	if raw, ok := value["compactionFailure"]; ok {
		v, err := rename(raw)
		if err != nil {
			return nil, err
		}
		value["compactionFailure"] = v
	}
	return value, nil
}
func checkpointLiveDocument(value JSON, _ []Operation, _ CheckpointInfo) (bool, error) {
	if _, present := value["generation"]; present {
		return false, nil
	}
	for _, key := range []string{"tools", "nestedTools"} {
		if raw, present := value[key]; present && raw != nil {
			items, ok := raw.([]any)
			if !ok {
				return false, reject("invalid live tool slots")
			}
			for _, item := range items {
				slot, ok := item.(map[string]any)
				if !ok {
					return false, reject("invalid live tool slot")
				}
				if slot["status"] == "running" {
					return false, nil
				}
			}
		}
	}
	return true, nil
}
