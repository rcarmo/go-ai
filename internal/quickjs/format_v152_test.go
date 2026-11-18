package quickjs

import (
	"context"
	"reflect"
	"testing"
)

func TestSourceV152NonFiniteOutputAndThrownValue(t *testing.T) {
	bg := context.Background()
	engine, err := NewEngine(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	got := engine.Execute(bg, `console.log("avg", 0 / 0, 1 / 0, -1 / 0, { a: 1 }); text(0 / 0);`, Options{})
	want := []OutputItem{{Type: "console", Text: `avg NaN Infinity -Infinity {"a":1}`}, {Type: "text", Text: "NaN"}}
	if !got.OK || got.Error != nil || !reflect.DeepEqual(got.Output, want) {
		t.Fatalf("output = %+v, %v; want %+v", got.Output, got.Error, want)
	}
	thrown := engine.Execute(bg, "throw NaN", Options{})
	if thrown.OK || thrown.Error == nil || thrown.Error.Kind != "script" || thrown.Error.Message != "NaN" {
		t.Fatalf("throw NaN: %+v, %v", thrown, thrown.Error)
	}
}
