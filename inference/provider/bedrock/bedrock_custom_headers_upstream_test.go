package bedrock

import "testing"

func TestBedrockCustomHeadersSkipsReservedHeadersCaseInsensitively(t *testing.T) {
	for _, key := range []string{"authorization", "Authorization", "AUTHORIZATION", "host", "Host", "HOST", "x-amz-date", "X-Amz-Date", "x-amz-security-token"} {
		if !isReservedBedrockHeader(key) {
			t.Fatalf("%q should be reserved", key)
		}
	}
}

func TestBedrockCustomHeadersAllowsCallerHeaders(t *testing.T) {
	for _, key := range []string{"x-custom", "x-allowed", "anthropic-beta", "x-trace-id"} {
		if isReservedBedrockHeader(key) {
			t.Fatalf("%q should be allowed", key)
		}
	}
}
