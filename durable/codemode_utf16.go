package durable

import (
	"reflect"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Decode only the VM's declared tool argument object through the lossless
// domain. Global values and other VM results retain their ordinary decoder.
func decodeQuickJSToolArguments(raw []byte) (JSON, error) {
	l := DefaultLimits()
	if len(raw) > l.MaxDocumentBytes {
		return nil, reject("tool arguments document limit")
	}
	if err := preflightStreamingLimits(raw, l); err != nil {
		return nil, err
	}
	value, err := jsonparse.ParseJSONValue(string(raw), true)
	if err != nil {
		return nil, reject("invalid tool arguments JSON")
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, reject("tool arguments must be an object")
	}
	return copyScopedToolArguments(JSON(object), true, l)
}
func ownCodemodeArguments(call CodemodeCall, l Limits) (JSON, error) {
	if call.Global && call.StreamingArguments {
		return nil, reject("typed tool arguments on global call")
	}
	return copyScopedToolArguments(call.Arguments, call.StreamingArguments, l)
}
func codemodeArgumentModeCompatible(a, b CodemodeCall) bool {
	// Old ordinary-JSON journals predate the encoding marker. Preserve their
	// admission mode on replay; encoding metadata is not a new call identity.
	return a.StreamingArguments == b.StreamingArguments || !containsUTF16(reflect.ValueOf(a.Arguments)) && !containsUTF16(reflect.ValueOf(b.Arguments))
}

// MarshalJSON uses the native Go protocol carrier, including for remote pipes.
// The bounded encoder handles this exact library type without invoking it.
func (call CodemodeCall) MarshalJSON() ([]byte, error) {
	l := DefaultLimits()
	if call.Global && call.StreamingArguments {
		return nil, reject("typed tool arguments on global call")
	}
	return encodeBounded(call, l, l.MaxDocumentBytes)
}

// UnmarshalJSON restores only the schema-owned native carrier. Raw unpaired
// strings outside that carrier retain generic JSON rejection.
func (call *CodemodeCall) UnmarshalJSON(raw []byte) error {
	type wire CodemodeCall
	var decoded wire
	l := DefaultLimits()
	if err := decodeStrict(raw, l, l.MaxDocumentBytes, &decoded); err != nil {
		return err
	}
	next := CodemodeCall(decoded)
	if next.Global && next.StreamingArguments {
		return reject("typed tool arguments on global call")
	}
	if err := restoreArgumentDTO(reflect.ValueOf(&next).Elem(), l); err != nil {
		return err
	}
	next.argumentsDecoded = len(next.UTF16Arguments) > 0
	*call = next
	return nil
}
