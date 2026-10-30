package jsonparse

import (
	"reflect"
	"strings"
	"testing"
)

func TestGrammarStreamUTF16Main(t *testing.T) {
	high, _ := ParseJSONValue(`"a\ud800"`, true)
	low, _ := ParseJSONValue(`"\udc00\u0000\udfff"`, true)
	s := GrammarToolInputStream{Property: "code"}
	first, err := s.Append(previewText(high))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append(previewText(low))
	if err != nil {
		t.Fatal(err)
	}
	final := "a𐀀\x00" + surrogateBytes(0xdfff)
	last, err := s.Close(&final)
	if err != nil {
		t.Fatal("code-unit prefix reconciliation failed", err)
	}
	parsed, err := ParseJSONValue(first+second+last, true)
	if err != nil {
		t.Fatal("invalid generated JSON", err)
	}
	value := parsed.(map[string]any)["code"]
	if !reflect.DeepEqual(previewUnits(value), []uint16{'a', 0xd800, 0xdc00, 0, 0xdfff}) {
		t.Fatal("generated input units lost", value)
	}
	if !strings.Contains(first+second+last, `\u0000`) {
		t.Fatal("control character was not JSON escaped")
	}
	again, err := s.Close(&final)
	if err != nil || again != "" {
		t.Fatal("idempotent close", again, err)
	}
	if _, err = s.Append(""); err == nil {
		t.Fatal("append after close accepted")
	}
	changed := "changed"
	if _, err = s.Close(&changed); err == nil {
		t.Fatal("changed closed input accepted")
	}
}

func TestGrammarStreamFinalPrefixMain(t *testing.T) {
	s := GrammarToolInputStream{}
	high := surrogateBytes(0xd800)
	first, err := s.Append("a" + high)
	if err != nil {
		t.Fatal(err)
	}
	final := "a𐀀"
	tail, err := s.Close(&final)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseJSONValue(first+tail, true)
	if err != nil || parsed.(map[string]any)["input"] != final {
		t.Fatal("split pair completion", parsed, err)
	}
	empty := GrammarToolInputStream{Property: "code"}
	if delta, err := empty.Append(""); err != nil || delta != "" {
		t.Fatal(delta, err)
	}
	if delta, err := empty.Close(nil); err != nil || delta != `{"code":""}` {
		t.Fatal(delta, err)
	}
	other := GrammarToolInputStream{}
	other.Append("abc")
	changed := "abd"
	if _, err = other.Close(&changed); err == nil {
		t.Fatal("divergent final accepted")
	}
	if _, err = other.Append(string([]byte{255})); err == nil {
		t.Fatal("invalid non-WTF-8 accepted")
	}
	tail, err = other.Close(nil)
	if err != nil || tail != `"}` {
		t.Fatal("failed reconciliation mutated input", tail, err)
	}
}
