package goai

import (
	"os"
	"testing"
)

func requireAnyEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if os.Getenv(name) != "" {
			return
		}
	}
	t.Skipf("skipping upstream live/E2E parity test; set one of %v", names)
}

func TestUpstreamCacheRetentionLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/cache-retention.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamContextOverflowLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/context-overflow.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamCrossProviderHandoffLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/cross-provider-handoff.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamEmptyLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/empty.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamImageToolResultLiveE2E(t *testing.T) {
	requireAnyEnv(t, "OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/image-tool-result.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamImagesLiveE2E(t *testing.T) {
	requireAnyEnv(t, "OPENROUTER_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/images.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamInterleavedThinkingLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/interleaved-thinking.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamResponseIDLiveE2E(t *testing.T) {
	requireAnyEnv(t, "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/responseid.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamStreamLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/stream.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamTokensLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/tokens.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamToolCallWithoutResultLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/tool-call-without-result.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamTotalTokensLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/total-tokens.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamUnicodeSurrogateLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY", "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/unicode-surrogate.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamZenLiveE2E(t *testing.T) {
	requireAnyEnv(t, "ANTHROPIC_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/zen.test.ts; live request assertions pending opt-in implementation")
}

func TestUpstreamXHighLiveE2E(t *testing.T) {
	requireAnyEnv(t, "OPENAI_API_KEY")
	t.Skip("LIVE-GATED parity wrapper for upstream test/xhigh.test.ts; live request assertions pending opt-in implementation")
}
