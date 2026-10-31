package errortext

import (
	"testing"
	"unicode/utf8"
)

func TestDecodeUTF8(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"hello", "hello"},
		{"\ufeffhello", "hello"},
		{"\ufeff\ufeffhello", "\ufeffhello"},
		{string([]byte{0xc0, 0xaf}), "��"},
		{string([]byte{0xed, 0xa0, 0x80}), "���"},
		{string([]byte{0xe2, 0x82}), "�"},
		{string([]byte{0xf0, 0x9f, 0x92}), "�"},
		{string([]byte{0xf0, 0x9f, 0x41}), "�A"},
		{string([]byte{0xe2, 0x28, 0xa1}), "�(�"},
		{string([]byte{0xf4, 0x90, 0x80, 0x80}), "����"},
		{"😃é", "😃é"},
	} {
		if got := DecodeUTF8([]byte(tc.raw)); got != tc.want || !utf8.ValidString(got) {
			t.Fatalf("%x: got %q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestTrim(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"\ufeff \tword\n\ufeff", "word"}, {"\u0085", "\u0085"}, {"\u2028word\u2029", "word"}} {
		if got := Trim(tc.input); got != tc.want {
			t.Fatal("JS whitespace", got, tc.want)
		}
	}
}
