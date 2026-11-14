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
func compactionRequestBlock(source string, instructions *string, native bool) goai.ContentBlock {
	block := goai.ContentBlock{Type: "text", Text: compactionRequestText(source, instructions)}
	if native {
		block.SetToolArgumentTranscript(block.Text)
	}
	return block
}

// Count a valid scalar as its source UTF-16 width; a carried isolated unit
// counts once instead of becoming three replacement runes.
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
