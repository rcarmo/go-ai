package durable

import (
	"encoding/json"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Translate a canonical public argument-error entry into the existing native
// receipt representation before strict native decoding. The exceptional scope
// is derived from the typed entry/model/diagnostic union, never arbitrary data.
func decodePublicEntryDiagnosticInput(raw []byte, limits Limits) ([]byte, error) {
	if len(raw) > limits.MaxRecordBytes {
		return nil, reject("entry byte limit")
	}
	if err := preflightStreamingLimits(raw, limits); err != nil {
		return nil, err
	}
	value, err := jsonparse.ParseJSONValue(string(raw), true)
	if err != nil {
		return nil, err
	}
	entry, ok := value.(map[string]any)
	if !ok || entry["kind"] != toolResultEntryKind {
		return raw, nil
	}
	models, ok := entry["model"].([]any)
	if !ok || len(models) != 1 {
		return raw, nil
	}
	model, ok := models[0].(map[string]any)
	if !ok || model["role"] != "toolResult" || model["isError"] != true {
		return raw, nil
	}
	data, ok := entry["data"].(map[string]any)
	if !ok {
		return raw, nil
	}
	diagnostics, ok := data["diagnostics"].([]any)
	if !ok {
		return raw, nil
	}
	remarks := make([]ToolDiagnostic, len(diagnostics))
	exceptional := false
	for index, item := range diagnostics {
		diagnostic, ok := item.(map[string]any)
		if !ok {
			return raw, nil
		}
		severity, ok := diagnostic["severity"].(string)
		if !ok {
			return raw, nil
		}
		code, _ := diagnostic["code"].(string)
		var text string
		switch message := diagnostic["message"].(type) {
		case string:
			text = message
		case goai.UTF16String:
			text = message.WTF8()
		default:
			return raw, nil
		}
		remark := ToolDiagnostic{Severity: severity, Code: code, Message: text}
		if !utf8.ValidString(text) {
			if severity != "error" || code != "invalid_arguments" {
				return raw, nil
			}
			remark.argumentError = true
			diagnostic["message"] = ""
			exceptional = true
		}
		remarks[index] = remark
	}
	if !exceptional {
		return raw, nil
	}
	content, ok := model["content"].([]any)
	if !ok {
		return raw, nil
	}
	texts := make(map[int]string)
	for index, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			return raw, nil
		}
		if block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(goai.UTF16String); ok {
			texts[index] = text.WTF8()
			block["text"] = ""
		}
	}
	modelWire, err := jsonparse.MarshalParsedJSONValue(model)
	if err != nil {
		return nil, err
	}
	var receipt MessageReceipt
	if err := json.Unmarshal(modelWire, &receipt); err != nil {
		return nil, err
	}
	receipt.ErrorCode = "invalid_arguments"
	receipt.Diagnostics = remarks
	for index, text := range texts {
		receipt.Content[index].SetToolArgumentDiagnostic(text)
	}
	nativeWire, err := encodeBounded(receipt, limits, limits.MaxRecordBytes)
	if err != nil {
		return nil, err
	}
	native, err := jsonparse.ParseJSONValue(string(nativeWire), true)
	if err != nil {
		return nil, err
	}
	models[0] = native
	return jsonparse.MarshalParsedJSONValue(entry)
}
