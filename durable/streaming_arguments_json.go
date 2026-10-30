package durable

import (
	"reflect"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

var nativeToolCallType = reflect.TypeFor[goai.ToolCall]()
var nativeContentBlockType = reflect.TypeFor[goai.ContentBlock]()
var nativeUTF16Type = reflect.TypeFor[goai.UTF16String]()
var nativeArgumentsType = reflect.TypeFor[goai.StreamingArgumentsJSON]()

func isNativeJSONType(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t == nativeUsageType || t == nativeToolCallType || t == nativeContentBlockType || t == nativeUTF16Type || t == nativeArgumentsType
}
func nativeStreamingFields(v reflect.Value) bool {
	if nativeArgumentDTO(v.Type()) {
		return v.FieldByName("StreamingArguments").Bool()
	}
	switch v.Type() {
	case nativeToolCallType:
		return v.Interface().(goai.ToolCall).HasStreamingArguments()
	case nativeContentBlockType:
		return v.Interface().(goai.ContentBlock).HasStreamingArguments()
	}
	return false
}
func nativeJSONField(v reflect.Value, field jsonStructField) (reflect.Value, error) {
	value := v.Field(field.index)
	if !nativeStreamingFields(v) {
		return value, nil
	}
	switch field.name {
	case "arguments":
		if nativeArgumentDTO(v.Type()) {
			return reflect.ValueOf(goai.StreamingArgumentsJSON(value.Interface().(JSON))), nil
		}
		return reflect.ValueOf(goai.StreamingArgumentsJSON(value.Interface().(map[string]any))), nil
	case "argumentsError":
		text, err := jsonparse.UTF16Text(value.String())
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(text), nil
	}
	return value, nil
}
func nativeFieldPresent(v reflect.Value, name string) bool {
	if usageZeroIsPresent(v, name) {
		return true
	}
	if (v.Type() == nativeToolCallType || v.Type() == nativeContentBlockType) && name == "arguments" && nativeStreamingFields(v) {
		return true
	}
	if v.Type() != nativeToolCallType && v.Type() != nativeContentBlockType {
		return false
	}
	flags := map[string]string{"thoughtSignature": "ThoughtSignaturePresent", "namespace": "NamespacePresent", "textSignature": "TextSignaturePresent", "thinkingSignature": "ThinkingSignaturePresent", "redacted": "RedactedPresent"}
	if flag := flags[name]; flag != "" {
		f := v.FieldByName(flag)
		return f.IsValid() && f.Bool()
	}
	return false
}
func (b *jsonBudget) key(text string) error {
	if !b.utf16Keys {
		return b.str(text)
	}
	b.hint(len(text) + 2)
	if len(text) > b.l.MaxStringBytes {
		return reject("JSON string byte limit")
	}
	if err := jsonparse.ValidateUTF16Key(text); err != nil {
		return reject("invalid UTF-16 argument key")
	}
	return nil
}
func encodeKey(b *limitedJSON, text string) error {
	if b.utf16Keys {
		return jsonparse.WriteUTF16Key(b, text)
	}
	return encodeString(b, text)
}

func (b *jsonBudget) nativeString(v reflect.Value) error {
	if !b.utf16Keys && !b.utf16Text {
		return reject("UTF-16 scalar outside typed arguments")
	}
	s := v.Interface().(goai.UTF16String)
	size := s.UTF8Size()
	b.hint(size + 2)
	if size > b.l.MaxStringBytes {
		return reject("JSON string byte limit")
	}
	return nil
}
func encodeUTF16Value(b *limitedJSON, v reflect.Value) error {
	return v.Interface().(goai.UTF16String).WriteJSON(b)
}
func restoreStreamingArguments(p []byte, target any) error {
	if err := goai.RestoreStreamingArgumentsJSON(p, target, true); err != nil {
		return reject("streaming argument JSON rejected")
	}
	return nil
}

func (b *jsonBudget) walkNativeField(v reflect.Value, field jsonStructField, depth int, objectsOnly bool) error {
	if nativeArgumentDTO(v.Type()) && (field.name == "utf16Arguments" || field.name == "streamingArguments") {
		return nil
	}
	if v.Type() == nativeReceiptType && field.name == "utf16Arguments" {
		return nil
	}
	value, err := nativeJSONField(v, field)
	if err != nil {
		return err
	}
	previousArgument, previousText := b.nativeArgument, b.utf16Text
	if nativeStreamingFields(v) {
		if field.name == "arguments" {
			b.nativeArgument = true
		}
		if field.name == "argumentsError" {
			b.utf16Text = true
		}
	}
	err = b.walk(value, depth, objectsOnly)
	b.nativeArgument, b.utf16Text = previousArgument, previousText
	return err
}
func decodeUTF16JSONString(raw []byte) (string, error) {
	value, err := jsonparse.ParseJSONValue(string(raw), true)
	if err != nil {
		return "", err
	}
	switch text := value.(type) {
	case string:
		return text, nil
	case goai.UTF16String:
		return text.WTF8(), nil
	default:
		return "", reject("expected UTF-16 JSON string")
	}
}
