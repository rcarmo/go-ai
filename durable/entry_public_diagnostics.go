package durable

import (
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Native storage keeps diagnostic witnesses on the model receipt. Public entry
// data must expose the original diagnostic strings, never their placeholders.
func publicEntryDiagnosticData(entry Entry) (any, bool, error) {
	if entry.publicDiagnosticData {
		return entry.Data, true, nil
	}
	if entry.Kind != toolResultEntryKind || len(entry.Model) != 1 || !entry.HasData {
		return entry.Data, false, nil
	}
	data := generationObject(entry.Data)
	if data == nil {
		return entry.Data, false, nil
	}
	diagnostics, ok := data["diagnostics"].([]any)
	if !ok || len(diagnostics) != len(entry.Model[0].Diagnostics) {
		return entry.Data, false, nil
	}
	out := JSON{}
	for key, value := range data {
		out[key] = value
	}
	copied := append([]any(nil), diagnostics...)
	changed := false
	for index, diagnostic := range entry.Model[0].Diagnostics {
		if !diagnostic.argumentError || utf8.ValidString(diagnostic.Message) {
			continue
		}
		original := generationObject(diagnostics[index])
		if original == nil {
			continue
		}
		value := JSON{}
		for key, member := range original {
			value[key] = member
		}
		text, err := jsonparse.UTF16Text(diagnostic.Message)
		if err != nil {
			return nil, false, err
		}
		value["message"] = text
		copied[index] = value
		changed = true
	}
	if !changed {
		return entry.Data, false, nil
	}
	out["diagnostics"] = copied
	return out, true, nil
}

func exposeEntryDiagnostics(entry Entry) (Entry, error) {
	data, changed, err := publicEntryDiagnosticData(entry)
	if err != nil {
		return Entry{}, err
	}
	if changed {
		entry.Data = data
		entry.publicDiagnosticData = true
	}
	return entry, nil
}

func publicEntryDataJSON(entry Entry) ([]byte, bool, error) {
	data, changed, err := publicEntryDiagnosticData(entry)
	if err != nil || !changed {
		return nil, false, err
	}
	// Validate every other field under the ordinary strict-JSON policy. Only
	// the private, generated argument-diagnostic messages have string authority.
	checked := JSON{}
	for key, value := range generationObject(data) {
		checked[key] = value
	}
	diagnostics := append([]any(nil), checked["diagnostics"].([]any)...)
	for index, diagnostic := range entry.Model[0].Diagnostics {
		if !diagnostic.argumentError || utf8.ValidString(diagnostic.Message) {
			continue
		}
		value := JSON{}
		for key, member := range generationObject(diagnostics[index]) {
			value[key] = member
		}
		value["message"] = ""
		diagnostics[index] = value
	}
	checked["diagnostics"] = diagnostics
	limits := maximumDurableLimits()
	if _, err := encodeBounded(checked, limits, limits.MaxRecordBytes); err != nil {
		return nil, false, err
	}
	wire, err := jsonparse.MarshalParsedJSONValue(cloneJSONValue(reflect.ValueOf(data)))
	if err == nil && len(wire) > limits.MaxRecordBytes {
		return nil, false, reject("entry data byte limit")
	}
	return wire, true, err
}

func publicEntryModelJSON(messages []MessageReceipt) ([]byte, error) {
	wire := []byte{'['}
	limits := maximumDurableLimits()
	for index, receipt := range messages {
		if _, err := encodeBounded(receipt, limits, limits.MaxRecordBytes); err != nil {
			return nil, err
		}
		// These are library-owned, validated message values. The public pi-ai
		// codec emits real UTF-16 strings and omits native witness properties.
		value, err := json.Marshal(receiptMessage(receipt))
		if err != nil {
			return nil, err
		}
		if receipt.ContentText != nil {
			members, err := rangeObject(value, 0)
			if err != nil {
				return nil, err
			}
			var object orderedJSON
			for _, member := range members {
				if member.name == "content" {
					object.field(member.name, *receipt.ContentText)
				} else {
					object.raw(member.name, member.raw)
				}
			}
			value, err = object.bytes()
			if err != nil {
				return nil, err
			}
		}
		if index > 0 {
			wire = append(wire, ',')
		}
		wire = append(wire, value...)
	}
	wire = append(wire, ']')
	if len(wire) > limits.MaxRecordBytes {
		return nil, reject("entry model byte limit")
	}
	return wire, nil
}

// sourcePublicEntry is minted only from a validated library Entry codec. It
// carries canonical JS strings through public readonly maps, not native storage.
type sourcePublicEntry map[string]any

var sourcePublicEntryType = reflect.TypeFor[sourcePublicEntry]()

func (v sourcePublicEntry) MarshalJSON() ([]byte, error) {
	return jsonparse.MarshalParsedJSONValue(map[string]any(v))
}
func reflectedPublicEntry(value reflect.Value) (sourcePublicEntry, bool) {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if value.IsValid() && value.Type() == sourcePublicEntryType {
		return value.Interface().(sourcePublicEntry), true
	}
	return nil, false
}
func publicEntryObject(entry Entry, limits Limits) (sourcePublicEntry, error) {
	wire, err := entry.MarshalJSON()
	if err != nil {
		return nil, err
	}
	value, err := jsonparse.ParseJSONValue(string(wire), true)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, reject("public entry must be an object")
	}
	result := sourcePublicEntry(object)
	if _, err := encodeBounded(result, limits, limits.MaxRecordBytes); err != nil {
		return nil, err
	}
	return result, nil
}

// Public entry reads expose the restored strings. Returning such a record to
// Storage uses the existing receipt witness for physical diagnostic encoding.
func nativeEntryDiagnosticData(entry Entry) Entry {
	if entry.Kind != toolResultEntryKind || len(entry.Model) != 1 || !entry.HasData {
		return entry
	}
	data := generationObject(entry.Data)
	if data == nil {
		return entry
	}
	diagnostics, ok := data["diagnostics"].([]any)
	if !ok || len(diagnostics) != len(entry.Model[0].Diagnostics) {
		return entry
	}
	out := JSON{}
	for key, value := range data {
		out[key] = value
	}
	copied := append([]any(nil), diagnostics...)
	changed := false
	entry.Model = append([]MessageReceipt(nil), entry.Model...)
	entry.Model[0].Diagnostics = append([]ToolDiagnostic(nil), entry.Model[0].Diagnostics...)
	for index, diagnostic := range entry.Model[0].Diagnostics {
		if entry.publicDiagnosticData {
			item := generationObject(diagnostics[index])
			var text string
			switch value := item["message"].(type) {
			case string:
				text = value
			case jsonparse.UTF16String:
				text = value.WTF8()
			default:
				continue
			}
			diagnostic.Message = text
			diagnostic.argumentError = !utf8.ValidString(text) && diagnostic.Severity == "error" && diagnostic.Code == "invalid_arguments"
			entry.Model[0].Diagnostics[index] = diagnostic
		}
		if !diagnostic.argumentError || utf8.ValidString(diagnostic.Message) {
			continue
		}
		original := generationObject(diagnostics[index])
		if original == nil {
			continue
		}
		value := JSON{}
		for key, member := range original {
			value[key] = member
		}
		value["message"] = ""
		copied[index] = value
		changed = true
	}
	if changed {
		out["diagnostics"] = copied
		entry.Data = out
	}
	entry.publicDiagnosticData = false
	return entry
}
