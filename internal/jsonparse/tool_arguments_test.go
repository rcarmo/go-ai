package jsonparse

import (
	"strings"
	"testing"
)

func TestCompletedToolArguments130(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{{"", true}, {"  ", true}, {`{"n":1}`, true}, {`{"text":"bad\u12ZZ"}`, true}, {"{\"text\":\"a\nb\"}", true}, {`{"n":`, false}, {`{"n":1`, false}, {`[]`, false}, {`null`, false}, {`42`, false}, {`true`, false}, {`{} trailing`, false}} {
		value, err := ParseToolCallArguments(tc.raw)
		if (err == "") != tc.valid || value == nil {
			t.Fatalf("%q: %#v %q", tc.raw, value, err)
		}
		if !tc.valid && !strings.Contains(err, tc.raw) {
			t.Fatal("raw arguments lost", err)
		}
	}
	value, err := ParseToolCallArguments(`{"text":"bad\u12ZZ"}`)
	if err != "" || value["text"] != `bad\u12ZZ` {
		t.Fatal(value, err)
	}
	value, err = ParseToolCallArguments(`{"text":"valid\u0041"}`)
	if err != "" || value["text"] != "validA" {
		t.Fatal(value, err)
	}
}
