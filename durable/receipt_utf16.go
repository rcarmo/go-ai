package durable

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// ReceiptUTF16Arguments is a storage adaptation for final argument values and
// diagnostics. Chunks contain base64 JSON, never the incremental parser's raw
// source/state. The stored content slot is an empty-object placeholder.
type ReceiptUTF16Arguments struct {
	Kind   string   `json:"kind,omitempty"`
	Index  int      `json:"index"`
	Chunks []string `json:"chunks"`
}

var nativeReceiptType = reflect.TypeFor[MessageReceipt]()

type receiptWireView struct {
	diagnostics []ToolDiagnostic
	content     []goai.ContentBlock
	arguments   []ReceiptUTF16Arguments
}

func containsUTF16(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	if v.Type() == nativeUTF16Type {
		return true
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			return containsUTF16(v.Elem())
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if it.Key().Kind() == reflect.String && !utf8.ValidString(it.Key().String()) || containsUTF16(it.Value()) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if containsUTF16(v.Index(i)) {
				return true
			}
		}
	case reflect.Struct:
		for _, field := range structJSONFields(v.Type()) {
			if containsUTF16(v.Field(field.index)) {
				return true
			}
		}
	}
	return false
}

func copyToolArguments(block goai.ContentBlock, l Limits) (JSON, error) {
	if !block.HasStreamingArguments() {
		return copyObject(JSON(block.Arguments), l)
	}
	value := reflect.ValueOf(goai.StreamingArgumentsJSON(block.Arguments))
	budget := jsonBudget{l: l, nativeArgument: true, active: map[uintptr]bool{}}
	if err := budget.walk(value, 1, true); err != nil {
		return nil, err
	}
	out := limitedJSON{max: l.MaxDocumentBytes, l: l}
	if err := encodeValue(&out, value); err != nil {
		return nil, err
	}
	parsed, err := jsonparse.ParseJSONValue(string(out.data), true)
	if err != nil {
		return nil, reject("invalid streaming arguments")
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return nil, reject("tool arguments must be object")
	}
	return JSON(object), nil
}

func makeReceiptWire(r MessageReceipt, l Limits, max int) (*receiptWireView, error) {
	if max > l.MaxDocumentBytes {
		max = l.MaxDocumentBytes
	}
	if max < 1 {
		return nil, reject("UTF-16 argument storage limit")
	}
	remaining := max
	view := &receiptWireView{content: r.Content, diagnostics: r.Diagnostics}
	for i, block := range r.Content {
		if block.Type != "toolCall" || !block.HasStreamingArguments() || !containsUTF16(reflect.ValueOf(block.Arguments)) && utf8.ValidString(block.ArgumentsError) {
			continue
		}
		chunks, err := encodeArgumentCarrier(JSON(block.Arguments), block.ArgumentsError, l, remaining)
		if err != nil {
			return nil, err
		}
		for _, chunk := range chunks {
			remaining -= len(chunk)
		}
		if view.arguments == nil {
			view.content = append([]goai.ContentBlock(nil), r.Content...)
		}
		view.arguments = append(view.arguments, ReceiptUTF16Arguments{Index: i, Chunks: chunks})
		placeholder := block
		placeholder.SetStreamingArguments(map[string]any{}, "")
		view.content[i] = placeholder
	}
	if err := appendDiagnosticWire(view, r, l, remaining); err != nil {
		return nil, err
	}
	return view, nil
}

func (b *limitedJSON) receiptJSONField(v reflect.Value, field jsonStructField) (reflect.Value, error) {
	if v.Type() != nativeReceiptType || field.name != "content" && field.name != "utf16Arguments" && field.name != "diagnostics" {
		return nativeJSONField(v, field)
	}
	if b.receiptViews == nil {
		b.receiptViews = map[reflect.Value]*receiptWireView{}
	}
	view := b.receiptViews[v]
	if view == nil {
		var err error
		view, err = makeReceiptWire(v.Interface().(MessageReceipt), b.l, b.max-len(b.data))
		if err != nil {
			return reflect.Value{}, err
		}
		b.receiptViews[v] = view
	}
	if len(view.arguments) > 0 {
		b.projectedReceipt = true
	}
	if field.name == "diagnostics" {
		return reflect.ValueOf(view.diagnostics), nil
	}
	if field.name == "content" {
		return reflect.ValueOf(view.content), nil
	}
	return reflect.ValueOf(view.arguments), nil
}

func restoreReceiptUTF16(r *MessageReceipt, l Limits) error {
	type slot struct {
		kind  string
		index int
	}
	seen := map[slot]bool{}
	type diagnosticValue struct {
		w    ReceiptUTF16Arguments
		text string
	}
	var diagnosticValues []diagnosticValue
	type restored struct {
		index      int
		args       map[string]any
		diagnostic string
	}
	var values []restored
	for _, witness := range r.UTF16Arguments {
		key := slot{witness.Kind, witness.Index}
		if seen[key] {
			return reject("duplicate UTF-16 argument witness")
		}
		seen[key] = true
		if witness.Kind != "" {
			text, err := validateDiagnosticWitness(r, witness, l)
			if err != nil {
				return err
			}
			diagnosticValues = append(diagnosticValues, diagnosticValue{witness, text})
			continue
		}
		if r.Role != goai.RoleAssistant || witness.Index < 0 || witness.Index >= len(r.Content) || len(witness.Chunks) == 0 {
			return reject("invalid UTF-16 argument witness")
		}
		block := r.Content[witness.Index]
		if block.Type != "toolCall" || block.ID == "" || block.Name == "" || block.Arguments == nil || len(block.Arguments) != 0 || block.ArgumentsError != "" {
			return reject("inconsistent UTF-16 argument placeholder")
		}
		carrier, err := decodeArgumentCarrier(witness.Chunks, l)
		if err != nil {
			return err
		}
		// A stored JSON decimal remains exact, while actual float infinities are
		// rejected on the logical write path before a witness can be generated.
		values = append(values, restored{witness.Index, carrier.Arguments, carrier.ArgumentsError})
	}
	for _, value := range diagnosticValues {
		if value.w.Kind == "text" {
			r.Content[value.w.Index].SetToolArgumentDiagnostic(value.text)
		} else {
			r.Diagnostics[value.w.Index].Message = value.text
			r.Diagnostics[value.w.Index].argumentError = true
		}
	}
	for _, value := range values {
		r.Content[value.index].SetStreamingArguments(value.args, value.diagnostic)
	}
	return nil
}

func scanReceiptProjection(raw []byte, l Limits, t reflect.Type) error {
	scanner := jsonScanner{data: raw, l: l}
	if bytes.Contains(raw, []byte(`\ud`)) || bytes.Contains(raw, []byte(`\uD`)) {
		if err := preflightStreamingLimits(raw, l); err != nil {
			return err
		}
		ranges, err := streamingArgumentRanges(raw, t, l)
		if err != nil {
			return err
		}
		scanner.utf16Ranges = ranges
	}
	scanner.space()
	if err := scanner.value(1); err != nil {
		return err
	}
	scanner.space()
	if scanner.pos != len(raw) {
		return reject("trailing receipt JSON")
	}
	return nil
}

func encodeArgumentCarrier(args JSON, diagnostic string, l Limits, max int) ([]string, error) {
	if max > l.MaxDocumentBytes {
		max = l.MaxDocumentBytes
	}
	if max < 1 {
		return nil, reject("UTF-16 argument storage limit")
	}
	// Logical arguments were checked by the enclosing budget before this
	// projection. The carrier independently enforces the same finite/authority
	// and resource contracts before its bounded JSON is base64-encoded.
	argumentBudget := jsonBudget{l: l, nativeArgument: true, active: map[uintptr]bool{}}
	if err := argumentBudget.walk(reflect.ValueOf(goai.StreamingArgumentsJSON(args)), 1, true); err != nil {
		return nil, err
	}
	carrier := goai.ToolCall{Type: "toolCall"}
	carrier.SetStreamingArguments(args, diagnostic)
	payload, err := encodeBoundedOrder(carrier, l, max, true)
	if err != nil {
		return nil, err
	}
	size := base64.RawStdEncoding.EncodedLen(len(payload))
	if size > max || l.MaxStringBytes < 1 {
		return nil, reject("UTF-16 argument storage limit")
	}
	count := (size-1)/l.MaxStringBytes + 1
	if count > l.MaxMembers || count > l.MaxNodes {
		return nil, reject("UTF-16 argument chunk limit")
	}

	encoded := base64.RawStdEncoding.EncodeToString(payload)
	var chunks []string
	for len(encoded) > 0 {
		n := l.MaxStringBytes
		if n > len(encoded) {
			n = len(encoded)
		}
		chunks = append(chunks, encoded[:n])
		encoded = encoded[n:]
	}

	return chunks, nil
}

func decodeArgumentCarrier(chunks []string, l Limits) (goai.ToolCall, error) {
	if len(chunks) == 0 {
		return goai.ToolCall{}, reject("missing UTF-16 argument carrier")
	}
	size := 0
	for _, chunk := range chunks {
		if len(chunk) == 0 || len(chunk) > l.MaxStringBytes || len(chunk) > l.MaxDocumentBytes-size {
			return goai.ToolCall{}, reject("UTF-16 argument witness limit")
		}
		size += len(chunk)
	}
	encoded := strings.Join(chunks, "")
	decodedSize := base64.RawStdEncoding.DecodedLen(size)
	if decodedSize > l.MaxDocumentBytes {
		return goai.ToolCall{}, reject("UTF-16 argument witness limit")
	}
	payload, err := base64.RawStdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return goai.ToolCall{}, reject("invalid UTF-16 argument witness encoding")
	}
	var carrier goai.ToolCall
	if err = decodeStrict(payload, l, l.MaxDocumentBytes, &carrier); err != nil {
		return goai.ToolCall{}, err
	}
	if carrier.Type != "toolCall" || carrier.ID != "" || carrier.Name != "" || carrier.Arguments == nil || carrier.ThoughtSignature != "" || carrier.Namespace != "" || !containsUTF16(reflect.ValueOf(carrier.Arguments)) && utf8.ValidString(carrier.ArgumentsError) {
		return goai.ToolCall{}, reject("invalid UTF-16 argument carrier")
	}

	return carrier, nil
}
