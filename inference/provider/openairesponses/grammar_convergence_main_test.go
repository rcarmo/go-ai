package openairesponses

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func responsesGrammarFixtureMain(size int, initial string) (string, string) {
	chunk := `chunk "quoted" \ ` + "\n" + strings.Repeat("x", 14)
	deltas := make([]string, size/len(chunk))
	for i := range deltas {
		deltas[i] = chunk
	}
	input := initial + strings.Join(deltas, "")
	return responsesGrammarEventsMain(initial, deltas, input), input
}

func responsesGrammarEventsMain(initial string, deltas []string, input string) string {
	var body strings.Builder
	emit := func(value any) {
		encoded, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&body, "data: %s\n\n", encoded)
	}
	item := func(text string) map[string]any {
		return map[string]any{"type": "custom_tool_call", "call_id": "call_1", "id": "ctc_1", "name": "sample_tool", "input": text}
	}
	emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item(initial)})
	for _, delta := range deltas {
		emit(map[string]any{"type": "response.custom_tool_call_input.delta", "output_index": 0, "item_id": "ctc_1", "delta": delta})
	}
	emit(map[string]any{"type": "response.custom_tool_call_input.done", "output_index": 0, "item_id": "ctc_1", "input": input})
	emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item(input)})
	emit(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	return body.String()
}

type responsesGrammarObservation struct {
	json   string
	starts []map[string]any
	done   *goai.Message
	err    error
}

func observeResponsesGrammarMain(body string) responsesGrammarObservation {
	events := make(chan goai.Event, 32)
	completed := make(chan responsesGrammarObservation, 1)
	go func() {
		var delta strings.Builder
		var r responsesGrammarObservation
		for event := range events {
			switch e := event.(type) {
			case *goai.ToolCallStartEvent:
				r.starts = append(r.starts, e.Partial.Content[e.ContentIndex].Arguments)
			case *goai.ToolCallDeltaEvent:
				delta.WriteString(e.Delta)
			case *goai.DoneEvent:
				r.done = e.Message
			case *goai.ErrorEvent:
				r.err = e.Err
			}
		}
		r.json = delta.String()
		completed <- r
	}()
	processStreamWithOptions(strings.NewReader(body), &goai.Model{ID: "gpt-test", Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI}, nil, goai.NewAssistantEventSender(events), responsesToolView{grammarToolInputProperties: map[string]string{"sample_tool": "payload"}})
	close(events)
	return <-completed
}

func runResponsesGrammarMain(tb testing.TB, body, expected, initial string) {
	tb.Helper()
	assertResponsesGrammarMain(tb, observeResponsesGrammarMain(body), expected, initial)
}

func assertResponsesGrammarMain(tb testing.TB, r responsesGrammarObservation, expected, initial string) {
	tb.Helper()
	if r.err != nil || r.done == nil {
		tb.Fatal("grammar stream failed", r.err)
	}
	if !reflect.DeepEqual(r.starts, []map[string]any{{"payload": initial}}) {
		tb.Fatalf("want one initial input snapshot, got %#v", r.starts)
	}
	if len(r.done.Content) != 1 || r.done.StopReason != goai.StopReasonToolUse {
		tb.Fatalf("want one completed tool call, got reason %s and %d blocks", r.done.StopReason, len(r.done.Content))
	}
	want := map[string]any{"payload": expected}
	block := r.done.Content[0]
	if block.Type != "toolCall" || block.ID != "call_1|ctc_1" || block.Name != "sample_tool" || !reflect.DeepEqual(block.Arguments, want) {
		tb.Fatalf("final tool call has wrong shape or arguments: type=%s id=%s name=%s", block.Type, block.ID, block.Name)
	}
	var generated map[string]any
	if err := json.Unmarshal([]byte(r.json), &generated); err != nil || !reflect.DeepEqual(generated, want) {
		tb.Fatal("append-only deltas do not reconstruct exactly one payload property", err)
	}
}
func TestResponsesGrammarInitialAndLargeInputMain(t *testing.T) {
	for _, size := range []int{0, 25600, 256000} {
		body, expected := responsesGrammarFixtureMain(size, "a")
		runResponsesGrammarMain(t, body, expected, "a")
	}
}
func BenchmarkResponsesGrammarGrowthMain(b *testing.B) {
	for _, size := range []int{25600, 256000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			body, expected := responsesGrammarFixtureMain(size, "")
			b.ReportAllocs()
			b.SetBytes(int64(len(expected)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runResponsesGrammarMain(b, body, expected, "")
			}
		})
	}
}

// Source: constrained-sampling.test.ts:345–443. These check the exact event
// fixtures; the separate allocation check below measures the same two sizes.
func TestSourceV160ResponsesGrammarInitialAndLargeInput(t *testing.T) {
	t.Run("initial input", func(t *testing.T) {
		body := responsesGrammarEventsMain("a", []string{"b"}, "abc")
		runResponsesGrammarMain(t, body, "abc", "a")
	})
	for _, size := range []int{25_600, 256_000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			body, input := pinnedResponsesGrammarFixtureV160(size)
			runResponsesGrammarMain(t, body, input, "")
		})
	}
}

func pinnedResponsesGrammarFixtureV160(size int) (string, string) {
	deltas := make([]string, size/32)
	for i := range deltas {
		chunk := fmt.Sprintf("chunk %d \"quoted\" \\ \n", i)
		deltas[i] = chunk + strings.Repeat("x", 32-len(chunk))
	}
	input := strings.Join(deltas, "")
	return responsesGrammarEventsMain("", deltas, input), input
}

// Source: constrained-sampling.test.ts:397. TotalAlloc, like the source's
// allocated-byte counter, includes garbage collected during the operation.
// Fixture construction and output assertions are outside the measured region.
func TestSourceV160ResponsesGrammarAllocationGrowth(t *testing.T) {
	allocated := func(size int) uint64 {
		body, input := pinnedResponsesGrammarFixtureV160(size)
		assertResponsesGrammarMain(t, observeResponsesGrammarMain(body), input, "")
		var samples [3]uint64
		for i := range samples {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			result := observeResponsesGrammarMain(body)
			runtime.ReadMemStats(&after)
			samples[i] = after.TotalAlloc - before.TotalAlloc
			assertResponsesGrammarMain(t, result, input, "")
		}
		slices.Sort(samples[:])
		return samples[1]
	}
	small, large := allocated(25_600), allocated(256_000)
	if small == 0 {
		t.Fatal("small stream allocated no bytes")
	}
	ratio := float64(large) / float64(small)
	if !(ratio < 25) {
		t.Fatalf("allocated bytes grew by %.2fx (%d -> %d); want less than 25x", ratio, small, large)
	}
	t.Logf("allocated bytes grew by %.2fx (%d -> %d)", ratio, small, large)
}
