package bedrock

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func usageModelMain() *goai.Model {
	return &goai.Model{ID: "model", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Cost: goai.ModelCost{Input: 3, Output: 15, CacheRead: .3, CacheWrite: 3.75}}
}
func metadataMessageMain(t *testing.T, model *goai.Model, usages ...*types.TokenUsage) *goai.Message {
	t.Helper()
	reader := &lifecycleReader{events: make(chan types.ConverseStreamOutput, len(usages)+2)}
	reader.events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	for _, usage := range usages {
		reader.events <- &types.ConverseStreamOutputMemberMetadata{Value: types.ConverseStreamMetadataEvent{Usage: usage}}
	}
	reader.events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonEndTurn}}
	close(reader.events)
	var final *goai.Message
	processConverseStream(lifecycleResponse(reader), model, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Error(e.Err)
		}
	})
	if final == nil {
		t.Fatal("missing metadata completion")
	}
	return final
}
func assertUsageMain(t *testing.T, u *goai.Usage, hour int, present bool, total int) {
	t.Helper()
	if u.CacheWrite1h != hour || u.CacheWrite1hSet != present || u.TotalTokens != total {
		t.Fatal("usage counter/presence", u, hour, present, total)
	}
	raw, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	json.Unmarshal(raw, &object)
	_, exists := object["cacheWrite1h"]
	if exists != present {
		t.Fatal("optional cache counter JSON", string(raw))
	}
	long := min(max(hour, 0), u.CacheWrite)
	wantWrite := (float64(u.CacheWrite-long)*3.75 + float64(long)*6) / 1e6
	wantTotal := float64(u.Input)*3/1e6 + float64(u.Output)*15/1e6 + float64(u.CacheRead)*.3/1e6 + wantWrite
	if math.Abs(u.Cost.CacheWrite-wantWrite) > 1e-12 || math.Abs(u.Cost.Total-wantTotal) > 1e-12 {
		t.Fatal("cache write pricing", u.Cost, wantWrite, wantTotal)
	}
	if u.Cost.TotalFromProvider == nil || *u.Cost.TotalFromProvider {
		t.Fatal("estimated cost provenance", u.Cost)
	}
}

func TestBedrockCacheMetadataMain(t *testing.T) {
	for _, tc := range []struct {
		name      string
		details   []types.CacheDetail
		hour      int
		present   bool
		total     *int32
		wantTotal int
	}{
		{name: "missing", wantTotal: 125},
		{name: "empty", details: []types.CacheDetail{}, present: true, wantTotal: 125},
		{name: "mixed", details: []types.CacheDetail{{Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(150000)}, {Ttl: types.CacheTTLFiveMinutes, InputTokens: aws.Int32(600000)}, {Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(250000)}}, hour: 400000, present: true, total: aws.Int32(1000000), wantTotal: 1000000},
		{name: "null-count", details: []types.CacheDetail{{Ttl: types.CacheTTLOneHour}}, present: true, wantTotal: 125},
		{name: "unknown-ttl", details: []types.CacheDetail{{Ttl: types.CacheTTL("future"), InputTokens: aws.Int32(999)}}, present: true, wantTotal: 125},
		{name: "zero-total", total: aws.Int32(0), wantTotal: 125},
		{name: "hour-over-total", details: []types.CacheDetail{{Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(2000000)}}, hour: 2000000, present: true, wantTotal: 125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := &types.TokenUsage{InputTokens: aws.Int32(100), OutputTokens: aws.Int32(25), TotalTokens: tc.total, CacheReadInputTokens: aws.Int32(20), CacheWriteInputTokens: aws.Int32(1000000), CacheDetails: tc.details}
			message := metadataMessageMain(t, usageModelMain(), u)
			if message.Usage.CacheWrite != 1000000 || message.Usage.CacheRead != 20 {
				t.Fatal("aggregate cache counters changed", message.Usage)
			}
			assertUsageMain(t, message.Usage, tc.hour, tc.present, tc.wantTotal)
		})
	}
}

func TestBedrockMetadataReplacementMain(t *testing.T) {
	first := &types.TokenUsage{InputTokens: aws.Int32(10), OutputTokens: aws.Int32(5), CacheWriteInputTokens: aws.Int32(100), CacheDetails: []types.CacheDetail{{Ttl: types.CacheTTLOneHour, InputTokens: aws.Int32(42)}}}
	for _, mode := range []string{"no-usage", "omitted-details", "empty-details"} {
		t.Run(mode, func(t *testing.T) {
			var second *types.TokenUsage
			hour, present, total := 42, true, 15
			if mode != "no-usage" {
				second = &types.TokenUsage{InputTokens: aws.Int32(7), OutputTokens: aws.Int32(3), CacheWriteInputTokens: aws.Int32(100)}
				hour, present, total = 0, false, 10
				if mode == "empty-details" {
					second.CacheDetails = []types.CacheDetail{}
					present = true
				}
			}
			message := metadataMessageMain(t, usageModelMain(), first, second)
			assertUsageMain(t, message.Usage, hour, present, total)
		})
	}
}

func TestBedrockCacheDetailsWirePresenceMain(t *testing.T) {
	for _, tc := range []struct {
		name, details string
		hour          int
		present       bool
	}{
		{name: "missing"}, {name: "null", details: `,"cacheDetails":null`}, {name: "empty", details: `,"cacheDetails":[]`, present: true}, {name: "one-hour", details: `,"cacheDetails":[{"ttl":"1h","inputTokens":400000},{"ttl":"5m","inputTokens":600000}]`, hour: 400000, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
				encoder := eventstream.NewEncoder()
				for _, f := range []struct{ name, payload string }{{"messageStart", `{"role":"assistant"}`}, {"metadata", `{"usage":{"inputTokens":100,"outputTokens":25,"totalTokens":0,"cacheReadInputTokens":20,"cacheWriteInputTokens":1000000` + tc.details + `}}`}, {"messageStop", `{"stopReason":"end_turn"}`}} {
					var headers eventstream.Headers
					headers.Set(":message-type", eventstream.StringValue("event"))
					headers.Set(":event-type", eventstream.StringValue(f.name))
					if err := encoder.Encode(w, eventstream.Message{Headers: headers, Payload: []byte(f.payload)}); err != nil {
						t.Error(err)
					}
				}
			}))
			defer server.Close()
			model := usageModelMain()
			model.BaseURL = server.URL
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var final *goai.Message
			for event := range streamBedrock(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Region: "us-east-1", Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1", "AWS_EC2_METADATA_DISABLED": "true"}}) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}
			if final == nil {
				t.Fatal("missing wire completion")
			}
			assertUsageMain(t, final.Usage, tc.hour, tc.present, 125)
		})
	}
}
