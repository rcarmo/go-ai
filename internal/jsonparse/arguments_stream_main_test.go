package jsonparse

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestArgumentsStreamMainPinnedReference(t *testing.T) {
	data, err := os.ReadFile("testdata/arguments_stream_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Operations []struct{ Append, Replace *[]uint16 }
			Frames     []struct {
				Text, Arguments, Delta any
				Same, Continuation     bool
			}
			Finish struct {
				Arguments any
				Error     bool
			}
		}
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for index, c := range corpus.Cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			s := &ToolCallArgumentsStream{}
			var prior map[string]any
			for i, op := range c.Operations {
				want := c.Frames[i]
				var args map[string]any
				if op.Append != nil {
					args = s.AppendUTF16(*op.Append)
				} else {
					r := s.ReplaceUTF16(*op.Replace)
					args = r.Arguments
					if r.Continuation != want.Continuation || r.Continuation && !reflect.DeepEqual(previewTag(r.Delta), want.Delta) {
						t.Fatalf("replace delta=%#v continuation=%v want=%#v/%v", previewTag(r.Delta), r.Continuation, want.Delta, want.Continuation)
					}
				}
				if !reflect.DeepEqual(previewTag(s.Text()), want.Text) || !reflect.DeepEqual(previewTag(args), want.Arguments) {
					t.Fatalf("frame %d text=%#v args=%#v want=%#v/%#v", i, previewTag(s.Text()), previewTag(args), want.Text, want.Arguments)
				}
				pointer := reflect.ValueOf(args).Pointer()
				if (pointer == reflect.ValueOf(prior).Pointer()) != want.Same {
					t.Fatalf("frame %d argument identity changed incorrectly", i)
				}
				prior = args
			}
			args, message := s.Finish()
			if (message != "") != c.Finish.Error || !reflect.DeepEqual(previewTag(args), c.Finish.Arguments) {
				t.Fatalf("finish args=%#v error=%q", previewTag(args), message)
			}
			if message != "" && !strings.HasSuffix(message, "\n"+previewText(s.Text())) {
				t.Fatal("diagnostic lost original text")
			}
			// The raw stream is private implementation state, not a serialised block.
			wire, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var exposed map[string]any
			if err = json.Unmarshal(wire, &exposed); err != nil || len(exposed) != 0 {
				t.Fatal("raw stream escaped into serialised state")
			}
		})
	}
}

func TestArgumentsStreamMainSnapshotIsolation(t *testing.T) {
	s := &ToolCallArgumentsStream{}
	first := s.Append(`{"a":"one`)
	second := s.Append(` two","items":[1`)
	s.Append(`,2]}`)
	if first["a"] != "one" || second["a"] != "one two" || len(second["items"].([]any)) != 1 {
		t.Fatal("prior argument object mutated")
	}
	done, err := s.Finish()
	if err != "" || len(done["items"].([]any)) != 2 {
		t.Fatal("final parse failed", err)
	}
}
