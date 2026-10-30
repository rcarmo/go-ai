package durable

import (
	"reflect"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func encodeCompactionArgument(value any, native bool, l Limits) ([]byte, error) {
	if !native {
		return encodeBounded(value, l, l.MaxDocumentBytes)
	}
	budget := jsonBudget{l: l, nativeArgument: true, utf16Keys: true, active: map[uintptr]bool{}}
	v := reflect.ValueOf(value)
	if err := budget.walk(v, 1, true); err != nil {
		return nil, err
	}
	out := limitedJSON{max: l.MaxDocumentBytes, l: l, utf16Keys: true}
	if err := encodeValue(&out, v); err != nil {
		return nil, err
	}
	return out.data, nil
}

// Native text is admitted only after each ordinary source/control field has
// passed UTF-8 validation. The final check also rejects non-WTF-8 byte sequences.
func compactionSourceValid(text string, native bool, l Limits) error {
	if len(text) > l.MaxStringBytes {
		return reject("compaction transcript limit")
	}
	if utf8.ValidString(text) {
		return nil
	}
	if !native {
		return reject("compaction transcript limit")
	}
	if _, err := jsonparse.UTF16Text(text); err != nil {
		return reject("invalid compaction argument text")
	}
	return nil
}
func compactionTextValue(text string) (any, error) { return jsonparse.UTF16Text(text) }
func compactionTextString(value any) (string, error) {
	switch x := value.(type) {
	case string:
		return x, nil
	case goai.UTF16String:
		return x.WTF8(), nil
	}
	return "", reject("invalid compaction source value")
}
func storeCompactionSource(cp JSON, text string, l Limits) error {
	if utf8.ValidString(text) {
		cp["source"] = text
		return nil
	}
	value, err := compactionTextValue(text)
	if err != nil {
		return err
	}
	state, err := dtoObject(toolArgumentState{StreamingArguments: true, Arguments: JSON{"source": value}}, l)
	if err != nil {
		return err
	}
	cp["sourceState"] = state
	return nil
}
func loadCompactionSource(cp JSON, l Limits) (string, bool, error) {
	raw, present := cp["sourceState"]
	if !present {
		source, ok := cp["source"].(string)
		if !ok {
			return "", false, reject("missing compaction source")
		}
		if !utf8.ValidString(source) {
			return "", false, reject("invalid ordinary compaction source")
		}
		return source, false, nil
	}
	if _, duplicate := cp["source"]; duplicate {
		return "", false, reject("mixed compaction source representations")
	}
	object, ok := raw.(map[string]any)
	if named, yes := raw.(JSON); yes {
		object = map[string]any(named)
		ok = true
	}
	if !ok {
		return "", false, reject("invalid compaction source state")
	}
	var state toolArgumentState
	if err := fromObject(JSON(object), &state, l); err != nil {
		return "", false, err
	}
	if !state.StreamingArguments || len(state.Arguments) != 1 {
		return "", false, reject("invalid compaction source owner")
	}
	text, err := compactionTextString(state.Arguments["source"])
	if err != nil {
		return "", false, err
	}
	if err = compactionSourceValid(text, true, l); err != nil {
		return "", false, err
	}
	return text, true, nil
}
func compactionRequestBlock(source, instructions string, native bool) goai.ContentBlock {
	block := goai.ContentBlock{Type: "text", Text: compactionRequestText(source, instructions)}
	if native {
		block.SetToolArgumentTranscript(block.Text)
	}
	return block
}

// A valid pair remains a complete scalar at the existing Go cut boundary;
// an isolated unit counts once instead of becoming three replacement runes.
func compactionRune(text string) (rune, int, int) {
	r, size := utf8.DecodeRuneInString(text)
	if r == utf8.RuneError && size == 1 && len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2] >= 0x80 && text[2] <= 0xbf {
		return rune(text[0]&15)<<12 | rune(text[1]&63)<<6 | rune(text[2]&63), 3, 1
	}
	units := 1
	if r > 0xffff {
		units = 2
	}
	return r, size, units
}
