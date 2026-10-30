package responsestream

import (
	"reflect"
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestCustomInitialAndFinalMain(t *testing.T) {
	s, err := NewCustomInput([]byte(`{"input":"a\ud800"}`), "code")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Arguments()["code"].(jsonparse.UTF16String); !ok {
		t.Fatal("initial argument ownership lost")
	}
	first, err := s.Append([]byte(`{"delta":"\udc00\u0000"}`))
	if err != nil {
		t.Fatal(err)
	}
	last, err := s.Close([]byte(`{"input":"a\ud800\udc00\u0000\udfff"}`))
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonparse.ParseJSONValue(first+last, false)
	if err != nil || !reflect.DeepEqual(value, s.Arguments()) {
		t.Fatal("initial/final JSON mismatch", value, err)
	}
	if repeated, err := s.Close([]byte(`{}`)); err != nil || repeated != "" {
		t.Fatal("missing final input should close idempotently", repeated, err)
	}
	other, err := NewCustomInput([]byte(`{"input":"abc"}`), "input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Close([]byte(`{"input":""}`)); err == nil {
		t.Fatal("explicit empty final bypassed prefix check")
	}
	tail, err := other.Close([]byte(`{}`))
	if err != nil || tail != `{"input":"abc"}` {
		t.Fatal("failed final discarded unsent initial input", tail, err)
	}
	if _, err = other.Append([]byte(`{"delta":""}`)); err == nil {
		t.Fatal("append after close accepted")
	}
	for _, raw := range []string{`{"input":42}`, `{"input":{}}`} {
		if _, err = NewCustomInput([]byte(raw), "input"); err == nil {
			t.Fatal("nonstring initial input accepted")
		}
	}
}
