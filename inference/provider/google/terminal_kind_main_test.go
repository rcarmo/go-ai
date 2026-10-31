package google

import (
	"fmt"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived from both adapters' post-stream error throw/catch at 81f3cd41.
// The upstream raw-stop file checks result messages but not terminal event kind.
func TestMainSourceGoogleTerminalKind(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			reason       string
			tool, failed bool
		}{{"MALFORMED_FUNCTION_CALL", false, true}, {"MALFORMED_FUNCTION_CALL", true, true}, {"SAFETY", false, true}, {"STOP", true, false}, {"MAX_TOKENS", true, false}} {
			t.Run(fmt.Sprintf("%s/%s/tool=%t", api, tc.reason, tc.tool), func(t *testing.T) {
				message, _, events := captureMainGoogleStreamEvents(t, api, []string{mainGoogleStopChunk(tc.reason, tc.tool)}, nil)
				done, failures := 0, 0
				for _, event := range events {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
						if e.Err == nil || e.Err.Error() != "Provider stopped with: "+tc.reason {
							t.Errorf("diagnostic %v", e.Err)
						}
					}
				}
				if tc.failed {
					if done != 0 || failures != 1 || message.StopReason != goai.StopReasonError {
						t.Fatalf("done=%d errors=%d reason=%s", done, failures, message.StopReason)
					}
				} else if done != 1 || failures != 0 {
					t.Fatalf("done=%d errors=%d", done, failures)
				}
			})
		}
	}
}
