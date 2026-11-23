package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type completionObservationV160 struct {
	message     *goai.Message
	counts      map[string]int
	toolIndexes []int
	toolEvents  map[int][]string
}

func observeCompletionsStreamV160(t *testing.T, model *goai.Model, ctx *goai.Context, firstLine int) completionObservationV160 {
	t.Helper()
	frames := pinnedCompletionsChunksV160[firstLine]
	if len(frames) == 0 {
		t.Fatalf("missing pinned SSE fixture at L%d", firstLine)
	}
	var body strings.Builder
	for _, frame := range frames {
		fmt.Fprintf(&body, "data: %s\n\n", frame)
	}
	requests := 0
	transport := promptCacheTransportV160(func(r *http.Request) (*http.Response, error) {
		requests++
		defer r.Body.Close()
		want := strings.TrimRight(model.BaseURL, "/") + "/chat/completions"
		if r.Method != http.MethodPost || r.URL.String() != want {
			return nil, fmt.Errorf("expected POST %s, got %s %s", want, r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String())), Request: r}, nil
	})
	level := goai.ThinkingLevel("") // Select the simple adapter without requesting effort.
	opts := &goai.StreamOptions{APIKey: "test", Reasoning: &level, HTTPTransport: transport}
	got := completionObservationV160{counts: map[string]int{}, toolEvents: map[int][]string{}}
	recordTool := func(index int, event string) {
		got.toolIndexes = append(got.toolIndexes, index)
		got.toolEvents[index] = append(got.toolEvents[index], event)
		got.counts[event]++
	}
	terminal := 0
	for event := range goai.Stream(t.Context(), model, ctx, opts) {
		switch e := event.(type) {
		case *goai.TextStartEvent:
			got.counts["text_start"]++
		case *goai.TextDeltaEvent:
			got.counts["text_delta"]++
		case *goai.TextEndEvent:
			got.counts["text_end"]++
		case *goai.ThinkingStartEvent:
			got.counts["thinking_start"]++
		case *goai.ThinkingDeltaEvent:
			got.counts["thinking_delta"]++
		case *goai.ThinkingEndEvent:
			got.counts["thinking_end"]++
		case *goai.ToolCallStartEvent:
			recordTool(e.ContentIndex, "toolcall_start")
		case *goai.ToolCallDeltaEvent:
			recordTool(e.ContentIndex, "toolcall_delta")
		case *goai.ToolCallEndEvent:
			recordTool(e.ContentIndex, "toolcall_end")
		case *goai.DoneEvent:
			terminal++
			got.message = e.Message
		case *goai.ErrorEvent:
			terminal++
			got.message = e.Error
			if got.message == nil {
				t.Fatalf("stream did not produce an assistant result: %v", e.Err)
			}
		}
	}
	if requests != 1 || terminal != 1 || got.message == nil {
		t.Fatalf("want one request and one terminal result, got requests=%d terminals=%d result=%v", requests, terminal, got.message)
	}
	return got
}

func assertCompletionsContentV160(t *testing.T, got *goai.Message, wantJSON string) {
	t.Helper()
	wire, err := json.Marshal(got.Content)
	if err != nil {
		t.Fatal(err)
	}
	var actual, want []map[string]any
	if err := json.Unmarshal(wire, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Errorf("content = %s; want %s", wire, wantJSON)
	}
}

func assertCompletionsNoErrorMessageV160(t *testing.T, got *goai.Message) {
	t.Helper()
	if got.ErrorMessage != "" || got.ErrorMessagePresent {
		t.Errorf("expected errorMessage to be absent; got %q (present=%v)", got.ErrorMessage, got.ErrorMessagePresent)
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:646-1172.
// Native terminal events are projected to their assistant result, as the
// source's .result() does. This does not equate SDK and native event classes.
func TestSourceV160CompletionsStreamAssembly(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheModelV160(t, "", "", "")
	t.Run("non-standard finish reason", func(t *testing.T) {
		zai := promptCacheCatalogueV160(t, goai.ProviderZAI, "glm-5.2")
		got := observeCompletionsStreamV160(t, zai, completionsChoiceContextV160("Hi"), 646).message
		if got.StopReason != goai.StopReasonError || got.ErrorMessage != "Provider finish_reason: network_error" {
			t.Errorf("terminal reason = %q, errorMessage = %q", got.StopReason, got.ErrorMessage)
		}
	})
	t.Run("null chunk", func(t *testing.T) {
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Reply with exactly OK"), 681).message
		if got.StopReason != goai.StopReasonStop || got.ResponseID != "chatcmpl-test" || got.Usage == nil || got.Usage.TotalTokens != 4 {
			t.Errorf("terminal reason=%q id=%q usage=%+v", got.StopReason, got.ResponseID, got.Usage)
		}
		assertCompletionsNoErrorMessageV160(t, got)
		assertCompletionsContentV160(t, got, `[{"type":"text","text":"OK"}]`)
	})
	t.Run("null finish reasons then EOF", func(t *testing.T) {
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Reply with a longer sentence"), 723).message
		if got.StopReason != goai.StopReasonError || got.ErrorMessage != "Stream ended without finish_reason" {
			t.Errorf("terminal reason = %q, errorMessage = %q", got.StopReason, got.ErrorMessage)
		}
	})
	t.Run("missing finish reason allowed", func(t *testing.T) {
		model := promptCacheModelV160(t, "", "", `{"supportsFinishReason":false}`)
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Reply with a complete answer"), 755).message
		if got.StopReason != goai.StopReasonStop {
			t.Errorf("terminal reason = %q, want stop", got.StopReason)
		}
		assertCompletionsNoErrorMessageV160(t, got)
		assertCompletionsContentV160(t, got, `[{"type":"text","text":"complete answer"}]`)
	})
	read := goai.Tool{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
	t.Run("empty custom object on function delta", func(t *testing.T) {
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Read README.md", read), 782).message
		assertCompletionsContentV160(t, got, `[{"type":"toolCall","id":"call_1","name":"read","arguments":{"path":"README.md"}}]`)
	})
	t.Run("stable index with changing ids", func(t *testing.T) {
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Read README.md", read), 831)
		if got.message.StopReason != goai.StopReasonToolUse {
			t.Errorf("terminal reason = %q, want toolUse", got.message.StopReason)
		}
		if !reflect.DeepEqual(got.toolIndexes, []int{0, 0, 0, 0, 0}) {
			t.Errorf("tool content indexes = %v, want [0 0 0 0 0]", got.toolIndexes)
		}
		assertCompletionsContentV160(t, got.message, `[{"type":"toolCall","id":"functions.read:0","name":"read","arguments":{"path":"README.md"}}]`)
	})
	t.Run("mixed parallel deltas", func(t *testing.T) {
		tools := []goai.Tool{
			read,
			{Name: "grep", Description: "Search a file", Parameters: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern","path"]}`)},
			{Name: "list", Description: "List a directory", Parameters: read.Parameters},
			{Name: "write", Description: "Write a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)},
		}
		got := observeCompletionsStreamV160(t, model, completionsChoiceContextV160("Think, answer, and use tools.", tools...), 942)
		if got.message.StopReason != goai.StopReasonToolUse {
			t.Errorf("terminal reason = %q, want toolUse", got.message.StopReason)
		}
		counts := map[string]int{"text_start": 1, "text_delta": 3, "text_end": 1, "thinking_start": 1, "thinking_delta": 2, "thinking_end": 1, "toolcall_start": 4, "toolcall_delta": 9, "toolcall_end": 4}
		if !reflect.DeepEqual(got.counts, counts) {
			t.Errorf("event counts = %v; want %v", got.counts, counts)
		}
		twoDeltas := []string{"toolcall_start", "toolcall_delta", "toolcall_delta", "toolcall_end"}
		want := map[int][]string{2: twoDeltas, 3: {"toolcall_start", "toolcall_delta", "toolcall_delta", "toolcall_delta", "toolcall_end"}, 4: twoDeltas, 5: twoDeltas}
		if !reflect.DeepEqual(got.toolEvents, want) {
			t.Errorf("events by content index = %v; want %v", got.toolEvents, want)
		}
		assertCompletionsContentV160(t, got.message, `[
		  {"type":"thinking","thinking":"think 1 think 2","thinkingSignature":"reasoning_content"},
		  {"type":"text","text":"answer 1 answer 2\n"},
		  {"type":"toolCall","id":"tc_read_initial","name":"read","arguments":{"path":"README.md"}},
		  {"type":"toolCall","id":"tc_grep_initial","name":"grep","arguments":{"pattern":"TODO","path":"src"}},
		  {"type":"toolCall","id":"tc_list_no_index","name":"list","arguments":{"path":"packages/ai"}},
		  {"type":"toolCall","id":"tc_write_no_index","name":"write","arguments":{"path":"out.txt","content":"ok"}}
		]`)
	})
}

// JSON.stringify of the pinned mockState.chunks arrays, keyed by declaration line.
var pinnedCompletionsChunksV160 = map[int][]string{
	646: {
		`{"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"network_error"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	},
	681: {
		`null`,
		`{"id":"chatcmpl-test","choices":[{"delta":{"content":"OK"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-test","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	},
	723: {
		`{"id":"chatcmpl-truncated","choices":[{"delta":{"content":"partial answer"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-truncated","choices":[{"delta":{"content":"partial answer"},"finish_reason":null}]}`,
	},
	755: {
		`{"id":"chatcmpl-no-finish-reason","choices":[{"delta":{"content":"complete answer"},"finish_reason":null}]}`,
	},
	782: {
		`{"id":"chatcmpl-empty-custom","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"},"custom":{}}]},"finish_reason":"tool_calls"}]}`,
	},
	831: {
		`{"id":"chatcmpl-kimi-bad-stream","choices":[{"delta":{"tool_calls":[{"index":0,"id":"functions.read:0","type":"function","function":{"name":"read","arguments":""}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-kimi-bad-stream","choices":[{"delta":{"tool_calls":[{"index":0,"id":"chatcmpl-tool-a","type":"function","function":{"name":null,"arguments":"{\"path\":\"README"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-kimi-bad-stream","choices":[{"delta":{"tool_calls":[{"index":0,"id":"chatcmpl-tool-b","type":"function","function":{"name":null,"arguments":".md\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	},
	942: {
		`{"id":"chatcmpl-mixed-deltas","choices":[{"delta":{"content":"answer 1","reasoning_content":"think 1","tool_calls":[{"index":0,"id":"tc_read_initial","type":"function","function":{"name":"read","arguments":"{\"path\":\"README"}},{"index":1,"id":"tc_grep_initial","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"TODO"}},{"id":"tc_list_no_index","type":"function","function":{"name":"list","arguments":"{\"path\":\"packages"}},{"id":"tc_write_no_index","type":"function","function":{"name":"write","arguments":"{\"path\":\"out"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-mixed-deltas","choices":[{"delta":{"content":" answer 2","tool_calls":[{"index":1,"id":"tc_grep_changed","type":"function","function":{"arguments":"\",\"path\":\"src"}},{"id":"tc_write_no_index","type":"function","function":{"arguments":".txt\",\"content\":\"ok\"}"}},{"id":"tc_list_no_index","type":"function","function":{"arguments":"/ai\"}"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-mixed-deltas","choices":[{"delta":{"content":"\n","reasoning_content":" think 2","tool_calls":[{"index":0,"id":"tc_read_changed","type":"function","function":{"arguments":".md\"}"}},{"index":1,"type":"function","function":{"arguments":"\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":8,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":2}}}`,
	},
}
