# Test Matrix

Current automated coverage by area.

## Core package

| Area | Coverage |
|---|---|
| Context helpers / harness | ✅ unit + integration tests |
| Transform logic | ✅ unit tests |
| Retry utility | ✅ unit tests |
| Logger | ✅ unit tests |
| Registry / complete path | ✅ unit tests |
| Example build + preflight | ✅ smoke tests |

## Provider-level fake-server coverage

| Provider | Request hooks | Retry | Protocol-specific behavior |
|---|---:|---:|---|
| OpenAI Completions | ✅ | ✅ | basic stream path |
| OpenAI Responses | ✅ | ✅ | ✅ Azure request cleanup + reasoning normalization |
| Anthropic | ✅ | ✅ | basic SSE path |
| Google Generative AI | ✅ | ✅ | basic Gemini SSE path |
| Google Gemini CLI | ✅ | ✅ | basic CCA SSE path |
| Mistral | ✅ | ✅ | basic SSE path |
| OpenAI Codex SSE fallback | ✅ | ✅ | SSE fallback only |
| OpenAI Codex WebSocket | ✅ request path | ✅ dial retry | ✅ fake WS protocol flow |
| Bedrock | ✅ payload hook path | ❌ unified retry | ✅ request + stream unit coverage |
| Faux | n/a | n/a | ✅ extensive harness/testing support |

## Not yet covered

| Area | Status |
|---|---|
| Real live-provider end-to-end calls in CI | ❌ requires secrets/network |
| Bedrock retry via unified `RetryConfig` | ❌ AWS SDK path still separate |
| Deeper protocol parsing coverage for every provider event variant | partial |
| Smart context compaction / summarizing compactor | ❌ not implemented |

## Notes

- HTTP provider retries are opt-in via `StreamOptions.RetryConfig`.
- Example smoke tests verify buildability and clean missing-credential behavior.
- Azure OpenAI Responses has dedicated fake-server tests for outbound cleanup and inbound reasoning normalization.
