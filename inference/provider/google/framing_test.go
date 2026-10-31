package google

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/rcarmo/go-ai/transports/sse"
)

func TestGoogleSDKFramingReadPartitions(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Wire, Failure string
			Chunks              []any
		}
	}
	raw, err := os.ReadFile("testdata/sdk_framing_main.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		for _, split := range []bool{false, true} {
			name := tc.Name + "/whole"
			if split {
				name = tc.Name + "/byte"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var reader io.Reader = strings.NewReader(tc.Wire)
				if split {
					reader = iotest.OneByteReader(reader)
				}
				var chunks []any
				failure := ""
				for event := range parseGoogleFrames(ctx, reader) {
					if event.Event == sse.EventError {
						t.Fatal(event.Data)
					}
					if event.Event == googleIncompleteEOF {
						failure = "incomplete"
						break
					}
					var value any
					if err := json.Unmarshal([]byte(event.Data), &value); err != nil {
						failure = "json"
						break
					}
					chunks = append(chunks, value)
				}
				if failure != tc.Failure || !reflect.DeepEqual(chunks, tc.Chunks) {
					t.Errorf("failure=%q want %q; decoded frames equal=%v", failure, tc.Failure, reflect.DeepEqual(chunks, tc.Chunks))
				}
			})
		}
	}
}

func TestGoogleFramingLineLimit(t *testing.T) {
	piece := strings.Repeat("x", 2*1024*1024)
	for _, tc := range []struct {
		name, wire, want string
		overflow         bool
	}{
		{name: "under bound", wire: "data: " + piece + "\n\n", want: piece},
		{name: "over bound", wire: "data: " + piece + piece + "\n\n", overflow: true},
		{name: "bounded lines in larger event", wire: "data: " + piece + "\n" + piece + "\n\n", want: piece + "\n" + piece},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			for event := range parseGoogleFrames(context.Background(), strings.NewReader(tc.wire)) {
				count++
				if tc.overflow {
					if event.Event != sse.EventError || !strings.Contains(event.Data, "token too long") {
						t.Errorf("missing scanner guard: %s", event.Event)
					}
				} else if event.Event != "message" || event.Data != tc.want {
					t.Errorf("frame changed: event=%s len=%d want=%d", event.Event, len(event.Data), len(tc.want))
				}
			}
			if count != 1 {
				t.Errorf("events=%d want 1", count)
			}
		})
	}
}
