# Shared local test corpus for sibling ports

Generated: 2026-06-23

Scope: tests authored in `go-ai` rather than ported 1:1 from upstream `@earendil-works/pi-ai`. The published upstream `v0.80.2` npm tarball does not include `*.test.ts`; the canonical GitHub source at `ec6311b` does. This registry is the local Go regression corpus for tests that are not known 1:1 upstream ports; `rs-ai` and `swift-ai` should adapt these where applicable. Keep this file updated whenever adding local regression/provider-quirk tests and cross-check against `docs/upstream-tests-parity.md`.

## Summary

- Local Go test functions inventoried: **188**
- Amazon Bedrock provider: 11
- Anthropic Messages provider: 6
- Core API / harness / transforms / utilities: 98
- Faux test provider: 10
- Gemini CLI provider: 1
- Google / Vertex provider: 4
- Image generation / OpenRouter images: 7
- Mistral provider: 1
- Model registry/metadata: 3
- OAuth providers: 8
- OpenAI Chat Completions provider: 8
- OpenAI Codex transport/provider: 9
- OpenAI/Azure Responses provider: 10
- Retry helper: 5
- SSE transport: 4
- Streaming/partial JSON parser: 3

## Tests

| Test | File | Covers | Upstream gap / bug guarded |
|---|---|---|---|
| `TestCompleteNilModelDoesNotPanic` | `audit_hardening_test.go` | model registry/generated metadata parity: Complete Nil Model Does Not Panic | Guards generated registry drift and provider metadata changes. |
| `TestNilRegistrationNoops` | `audit_hardening_test.go` | Nil Registration Noops | Guards locally discovered edge cases and Go API compatibility. |
| `TestCloneContextDeepCopiesNestedFields` | `audit_hardening_test.go` | Clone Context Deep Copies Nested Fields | Guards locally discovered edge cases and Go API compatibility. |
| `TestGetToolCallsReturnsArgumentCopies` | `audit_hardening_test.go` | tool-call/schema conversion behavior: Get Tool Calls Returns Argument Copies | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestMapThinkingAndCostNilSafe` | `audit_hardening_test.go` | reasoning/thinking wire-format behavior: Map Thinking And Cost Nil Safe | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestAdjustMaxTokensForThinkingReservesOutput` | `audit_hardening_test.go` | reasoning/thinking wire-format behavior: Adjust Max Tokens For Thinking Reserves Output | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestIsContextOverflowUsesDiagnosticsAndNilSafe` | `audit_hardening_test.go` | Is Context Overflow Uses Diagnostics And Nil Safe | Guards locally discovered edge cases and Go API compatibility. |
| `TestAdaptReasoningItem` | `coverage_boost_test.go` | reasoning/thinking wire-format behavior: Adapt Reasoning Item | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestAdaptCommentaryDone` | `coverage_boost_test.go` | Adapt Commentary Done | Guards locally discovered edge cases and Go API compatibility. |
| `TestNormalizeReasoningTextDone` | `coverage_boost_test.go` | reasoning/thinking wire-format behavior: Normalize Reasoning Text Done | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestShortHash` | `coverage_boost_test.go` | Short Hash | Guards locally discovered edge cases and Go API compatibility. |
| `TestCopilotHeaders` | `coverage_boost_test.go` | auth/header/env edge case: Copilot Headers | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestCopilotHeadersWithIntent` | `coverage_boost_test.go` | auth/header/env edge case: Copilot Headers With Intent | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestNewStderrLogger` | `coverage_boost_test.go` | New Stderr Logger | Guards locally discovered edge cases and Go API compatibility. |
| `TestClearModels` | `coverage_boost_test.go` | model registry/generated metadata parity: Clear Models | Guards generated registry drift and provider metadata changes. |
| `TestDefaultRetryConfig` | `coverage_boost_test.go` | retry/cancellation robustness: Default Retry Config | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestNoRetryConfig` | `coverage_boost_test.go` | retry/cancellation robustness: No Retry Config | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestNewHTTPClient` | `coverage_boost_test.go` | New HTTPClient | Guards locally discovered edge cases and Go API compatibility. |
| `TestDoWithRetrySuccess` | `coverage_boost_test.go` | retry/cancellation robustness: Do With Retry Success | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestDoWithRetry429` | `coverage_boost_test.go` | retry/cancellation robustness: Do With Retry429 | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestDoWithRetryExhausted` | `coverage_boost_test.go` | retry/cancellation robustness: Do With Retry Exhausted | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestDoWithRetryOnRetryCallback` | `coverage_boost_test.go` | retry/cancellation robustness: Do With Retry On Retry Callback | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestAppendAssistantMessage` | `coverage_boost_test.go` | Append Assistant Message | Guards locally discovered edge cases and Go API compatibility. |
| `TestGetTextContent` | `coverage_boost_test.go` | Get Text Content | Guards locally discovered edge cases and Go API compatibility. |
| `TestInvokeOnResponse` | `coverage_boost_test.go` | Invoke On Response | Guards locally discovered edge cases and Go API compatibility. |
| `TestCompleteViaFaux` | `coverage_boost_test.go` | Complete Via Faux | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamMissingFunction` | `coverage_boost_test.go` | streaming/event transport behavior: Stream Missing Function | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestCompleteErrorEventWithoutMessage` | `coverage_boost_test.go` | Complete Error Event Without Message | Guards locally discovered edge cases and Go API compatibility. |
| `TestApplyToolCallLimitNoOp` | `coverage_test.go` | tool-call/schema conversion behavior: Apply Tool Call Limit No Op | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestApplyToolCallLimitTrims` | `coverage_test.go` | tool-call/schema conversion behavior: Apply Tool Call Limit Trims | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestAzureSessionHeaders` | `coverage_test.go` | auth/header/env edge case: Azure Session Headers | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestNormalizeAzureReasoningEventPassthrough` | `coverage_test.go` | reasoning/thinking wire-format behavior: Normalize Azure Reasoning Event Passthrough | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestNormalizeAzureReasoningEventCommentary` | `coverage_test.go` | reasoning/thinking wire-format behavior: Normalize Azure Reasoning Event Commentary | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestNormalizeAzureReasoningTextDelta` | `coverage_test.go` | reasoning/thinking wire-format behavior: Normalize Azure Reasoning Text Delta | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestDetectCompatProviders` | `coverage_test.go` | Detect Compat Providers | Guards locally discovered edge cases and Go API compatibility. |
| `TestResolveAPIKey` | `coverage_test.go` | auth/header/env edge case: Resolve APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestTransformMessagesPreservesImages` | `coverage_test.go` | image generation behavior: Transform Messages Preserves Images | Guards OpenRouter/image API behavior beyond text providers. |
| `TestTransformInsertsSyntheticToolResults` | `coverage_test.go` | tool-call/schema conversion behavior: Transform Inserts Synthetic Tool Results | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestClampReasoning` | `coverage_test.go` | reasoning/thinking wire-format behavior: Clamp Reasoning | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestSupportsXhigh` | `coverage_test.go` | Supports Xhigh | Guards locally discovered edge cases and Go API compatibility. |
| `TestValidateTypeChecks` | `coverage_test.go` | Validate Type Checks | Guards locally discovered edge cases and Go API compatibility. |
| `TestUnregisterAndClear` | `coverage_test.go` | Unregister And Clear | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamNilModel` | `defensive_test.go` | streaming/event transport behavior: Stream Nil Model | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestAppendAssistantMessageNilSafe` | `defensive_test.go` | Append Assistant Message Nil Safe | Guards locally discovered edge cases and Go API compatibility. |
| `TestDoWithRetryRequiresReplayableBody` | `defensive_test.go` | provider request/payload parity: Do With Retry Requires Replayable Body | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestDoWithRetryNegativeMaxRetriesClampsToSingleAttempt` | `defensive_test.go` | retry/cancellation robustness: Do With Retry Negative Max Retries Clamps To Single Attempt | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestDoWithRetryReplaysBodyAcrossRetries` | `defensive_test.go` | provider request/payload parity: Do With Retry Replays Body Across Retries | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestExamplesBuild` | `examples_smoke_test.go` | Examples Build | Guards locally discovered edge cases and Go API compatibility. |
| `TestExamplesMissingCredentialMessages` | `examples_smoke_test.go` | Examples Missing Credential Messages | Guards locally discovered edge cases and Go API compatibility. |
| `TestUserMessage` | `goai_test.go` | User Message | Guards locally discovered edge cases and Go API compatibility. |
| `TestContextJSON` | `goai_test.go` | Context JSON | Guards locally discovered edge cases and Go API compatibility. |
| `TestModelRegistry` | `goai_test.go` | model registry/generated metadata parity: Model Registry | Guards generated registry drift and provider metadata changes. |
| `TestStreamNoProvider` | `goai_test.go` | streaming/event transport behavior: Stream No Provider | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestIsContextOverflow` | `goai_test.go` | Is Context Overflow | Guards locally discovered edge cases and Go API compatibility. |
| `TestValidateToolCall` | `goai_test.go` | tool-call/schema conversion behavior: Validate Tool Call | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestGetEnvAPIKey` | `goai_test.go` | auth/header/env edge case: Get Env APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestGetEnvAPIKeyAnthropic` | `goai_test.go` | auth/header/env edge case: Get Env APIKey Anthropic | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestGetEnvAPIKeyWithEnvBedrockAuthenticated` | `goai_test.go` | auth/header/env edge case: Get Env APIKey With Env Bedrock Authenticated | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestGetEnvAPIKeyWithEnvGoogleVertexADC` | `goai_test.go` | auth/header/env edge case: Get Env APIKey With Env Google Vertex ADC | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestCalculateCost` | `goai_test.go` | Calculate Cost | Guards locally discovered edge cases and Go API compatibility. |
| `TestCalculateCostAnthropicLongCacheWrite` | `goai_test.go` | prompt/cache usage or retention behavior: Calculate Cost Anthropic Long Cache Write | Guards cache-retention/usage accounting/cost parity. |
| `TestModelsAreEqual` | `goai_test.go` | model registry/generated metadata parity: Models Are Equal | Guards generated registry drift and provider metadata changes. |
| `TestAdjustMaxTokensForThinking` | `goai_test.go` | reasoning/thinking wire-format behavior: Adjust Max Tokens For Thinking | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestTransformSkipsErroredMessages` | `goai_test.go` | Transform Skips Errored Messages | Guards locally discovered edge cases and Go API compatibility. |
| `TestTransformDowngradesImages` | `goai_test.go` | image generation behavior: Transform Downgrades Images | Guards OpenRouter/image API behavior beyond text providers. |
| `TestSanitizeSurrogates` | `goai_test.go` | Sanitize Surrogates | Guards locally discovered edge cases and Go API compatibility. |
| `TestDetectCompat` | `goai_test.go` | Detect Compat | Guards locally discovered edge cases and Go API compatibility. |
| `TestClampThinkingLevelPrefersUpgrade` | `goai_test.go` | reasoning/thinking wire-format behavior: Clamp Thinking Level Prefers Upgrade | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestHasOpenAIAuthHeader` | `goai_test.go` | auth/header/env edge case: Has Open AIAuth Header | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestMergeProviderHeadersAppliesOverridesAndSuppressions` | `goai_test.go` | auth/header/env edge case: Merge Provider Headers Applies Overrides And Suppressions | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestApplyDefaultHeadersPreservesExplicitEmptyOverride` | `goai_test.go` | auth/header/env edge case: Apply Default Headers Preserves Explicit Empty Override | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestHasAnthropicAuthHeader` | `goai_test.go` | auth/header/env edge case: Has Anthropic Auth Header | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestBuildCopilotDynamicHeaders` | `goai_test.go` | auth/header/env edge case: Build Copilot Dynamic Headers | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestAgentLoopHarness` | `harness_integration_test.go` | Agent Loop Harness | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamingHarness` | `harness_integration_test.go` | streaming/event transport behavior: Streaming Harness | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestErrorHandlingHarness` | `harness_integration_test.go` | Error Handling Harness | Guards locally discovered edge cases and Go API compatibility. |
| `TestContextCompactionHarness` | `harness_integration_test.go` | Context Compaction Harness | Guards locally discovered edge cases and Go API compatibility. |
| `TestHooksHarness` | `harness_integration_test.go` | Hooks Harness | Guards locally discovered edge cases and Go API compatibility. |
| `TestCrossProviderHandoff` | `harness_integration_test.go` | Cross Provider Handoff | Guards locally discovered edge cases and Go API compatibility. |
| `TestCloneContext` | `harness_test.go` | Clone Context | Guards locally discovered edge cases and Go API compatibility. |
| `TestCloneContextNil` | `harness_test.go` | Clone Context Nil | Guards locally discovered edge cases and Go API compatibility. |
| `TestSaveLoadContext` | `harness_test.go` | Save Load Context | Guards locally discovered edge cases and Go API compatibility. |
| `TestEstimateTokens` | `harness_test.go` | Estimate Tokens | Guards locally discovered edge cases and Go API compatibility. |
| `TestFitsInContextWindow` | `harness_test.go` | Fits In Context Window | Guards locally discovered edge cases and Go API compatibility. |
| `TestCompactContext` | `harness_test.go` | Compact Context | Guards locally discovered edge cases and Go API compatibility. |
| `TestGetToolCalls` | `harness_test.go` | tool-call/schema conversion behavior: Get Tool Calls | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestNeedsToolExecution` | `harness_test.go` | tool-call/schema conversion behavior: Needs Tool Execution | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestAppendHelpers` | `harness_test.go` | Append Helpers | Guards locally discovered edge cases and Go API compatibility. |
| `TestHooksOnStreamOptions` | `harness_test.go` | streaming/event transport behavior: Hooks On Stream Options | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestInvokeOnPayloadNil` | `harness_test.go` | provider request/payload parity: Invoke On Payload Nil | Guards locally discovered edge cases and Go API compatibility. |
| `TestImageAPIProviderRegistered` | `images_test.go` | image generation behavior: Image APIProvider Registered | Guards OpenRouter/image API behavior beyond text providers. |
| `TestBuiltinImageModels` | `images_test.go` | model registry/generated metadata parity: Builtin Image Models | Guards generated registry drift and provider metadata changes. |
| `TestGenerateImagesErrorPaths` | `images_test.go` | image generation behavior: Generate Images Error Paths | Guards OpenRouter/image API behavior beyond text providers. |
| `TestGenerateImagesOpenRouterHooksAndResponse` | `images_test.go` | image generation behavior: Generate Images Open Router Hooks And Response | Guards OpenRouter/image API behavior beyond text providers. |
| `TestGenerateImagesOpenRouterUsesProviderEnvAPIKey` | `images_test.go` | auth/header/env edge case: Generate Images Open Router Uses Provider Env APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestGenerateImagesOpenRouterPayloadParityAndAbort` | `images_test.go` | provider request/payload parity: Generate Images Open Router Payload Parity And Abort | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestGenerateImagesOpenRouterRetriesAndHookError` | `images_test.go` | image generation behavior: Generate Images Open Router Retries And Hook Error | Guards OpenRouter/image API behavior beyond text providers. |
| `TestNormalizeAnthropicBaseURLAddsV1` | `inference/provider/anthropic/anthropic_copilot_test.go` | provider OAuth/provider-specific behavior: Normalize Anthropic Base URLAdds V1 | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamAnthropicUsesBearerForCopilot` | `inference/provider/anthropic/anthropic_copilot_test.go` | streaming/event transport behavior: Stream Anthropic Uses Bearer For Copilot | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestBuildRequestJSONRoundTrip` | `inference/provider/anthropic/anthropic_copilot_test.go` | provider request/payload parity: Build Request JSONRound Trip | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamAnthropicParsesOneHourCacheWriteUsage` | `inference/provider/anthropic/anthropic_retry_test.go` | streaming/event transport behavior: Stream Anthropic Parses One Hour Cache Write Usage | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamAnthropicUsesExplicitAuthHeaderWithoutAPIKey` | `inference/provider/anthropic/anthropic_retry_test.go` | streaming/event transport behavior: Stream Anthropic Uses Explicit Auth Header Without APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestStreamAnthropicRetries429AndSucceeds` | `inference/provider/anthropic/anthropic_retry_test.go` | streaming/event transport behavior: Stream Anthropic Retries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestProcessConverseStreamSurfacesStreamErr` | `inference/provider/bedrock/bedrock_stream_test.go` | streaming/event transport behavior: Process Converse Stream Surfaces Stream Err | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestMapStopReason` | `inference/provider/bedrock/bedrock_stream_test.go` | Map Stop Reason | Guards locally discovered edge cases and Go API compatibility. |
| `TestExtractRegionFromURL` | `inference/provider/bedrock/bedrock_test.go` | Extract Region From URL | Guards locally discovered edge cases and Go API compatibility. |
| `TestShouldUseExplicitBedrockEndpoint` | `inference/provider/bedrock/bedrock_test.go` | Should Use Explicit Bedrock Endpoint | Guards locally discovered edge cases and Go API compatibility. |
| `TestBedrockCustomHeaderReservation` | `inference/provider/bedrock/bedrock_test.go` | auth/header/env edge case: Bedrock Custom Header Reservation | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestBedrockOptionPrecedenceAndRequestMetadata` | `inference/provider/bedrock/bedrock_test.go` | provider request/payload parity: Bedrock Option Precedence And Request Metadata | Guards generated registry drift and provider metadata changes. |
| `TestBuildConverseInputIncludesSystemToolsAndThinking` | `inference/provider/bedrock/bedrock_test.go` | tool-call/schema conversion behavior: Build Converse Input Includes System Tools And Thinking | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestBuildConverseInputUsesNativeXhighForClaudeOpus47` | `inference/provider/bedrock/bedrock_test.go` | Build Converse Input Uses Native Xhigh For Claude Opus47 | Guards locally discovered edge cases and Go API compatibility. |
| `TestConvertMessagesCoalescesConsecutiveToolResults` | `inference/provider/bedrock/bedrock_test.go` | tool-call/schema conversion behavior: Convert Messages Coalesces Consecutive Tool Results | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestCreateImageBlockDecodesBase64` | `inference/provider/bedrock/bedrock_test.go` | image generation behavior: Create Image Block Decodes Base64 | Guards OpenRouter/image API behavior beyond text providers. |
| `TestBedrockPayloadHookCanReplaceInput` | `inference/provider/bedrock/bedrock_test.go` | provider request/payload parity: Bedrock Payload Hook Can Replace Input | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxContentAndAssistantHelpers` | `inference/provider/faux/faux_test.go` | Faux Content And Assistant Helpers | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxTextStream` | `inference/provider/faux/faux_test.go` | streaming/event transport behavior: Faux Text Stream | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestFauxComplete` | `inference/provider/faux/faux_test.go` | Faux Complete | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxToolCall` | `inference/provider/faux/faux_test.go` | tool-call/schema conversion behavior: Faux Tool Call | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestFauxThinking` | `inference/provider/faux/faux_test.go` | reasoning/thinking wire-format behavior: Faux Thinking | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestFauxResponseFactory` | `inference/provider/faux/faux_test.go` | Faux Response Factory | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxMultipleResponses` | `inference/provider/faux/faux_test.go` | Faux Multiple Responses | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxError` | `inference/provider/faux/faux_test.go` | Faux Error | Guards locally discovered edge cases and Go API compatibility. |
| `TestFauxAbort` | `inference/provider/faux/faux_test.go` | retry/cancellation robustness: Faux Abort | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestFauxCallCount` | `inference/provider/faux/faux_test.go` | Faux Call Count | Guards locally discovered edge cases and Go API compatibility. |
| `TestStreamGeminiCLIRetries429AndSucceeds` | `inference/provider/geminicli/geminicli_retry_test.go` | streaming/event transport behavior: Stream Gemini CLIRetries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestBuildStreamURLEscapesPathAndQuery` | `inference/provider/google/google_audit_test.go` | streaming/event transport behavior: Build Stream URLEscapes Path And Query | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestBuildVertexStreamURLUsesProjectAndLocationOptions` | `inference/provider/google/google_audit_test.go` | streaming/event transport behavior: Build Vertex Stream URLUses Project And Location Options | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestProcessStreamHandlesMultilineSSE` | `inference/provider/google/google_audit_test.go` | streaming/event transport behavior: Process Stream Handles Multiline SSE | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamGoogleRetries429AndSucceeds` | `inference/provider/google/google_retry_test.go` | streaming/event transport behavior: Stream Google Retries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamMistralRetries429AndSucceeds` | `inference/provider/mistral/mistral_retry_test.go` | streaming/event transport behavior: Stream Mistral Retries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamOpenAIInvokesOnPayload` | `inference/provider/openai/openai_payload_test.go` | provider request/payload parity: Stream Open AIInvokes On Payload | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamOpenAIUsesExplicitAuthHeaderWithoutAPIKey` | `inference/provider/openai/openai_payload_test.go` | streaming/event transport behavior: Stream Open AIUses Explicit Auth Header Without APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestStreamOpenAICloudflareAIGatewayHeadersAndURL` | `inference/provider/openai/openai_payload_test.go` | streaming/event transport behavior: Stream Open AICloudflare AIGateway Headers And URL | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestBuildRequestBodyClampsPromptCacheKey` | `inference/provider/openai/openai_payload_test.go` | provider request/payload parity: Build Request Body Clamps Prompt Cache Key | Guards cache-retention/usage accounting/cost parity. |
| `TestBuildRequestBodyUsesCompatThinkingFormats` | `inference/provider/openai/openai_payload_test.go` | provider request/payload parity: Build Request Body Uses Compat Thinking Formats | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestProcessSSEStreamCapturesResponseModelAndCacheUsage` | `inference/provider/openai/openai_payload_test.go` | streaming/event transport behavior: Process SSEStream Captures Response Model And Cache Usage | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestProcessSSEStreamAttachesPendingEncryptedReasoningDetails` | `inference/provider/openai/openai_payload_test.go` | streaming/event transport behavior: Process SSEStream Attaches Pending Encrypted Reasoning Details | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamOpenAIRetries429AndSucceeds` | `inference/provider/openai/openai_retry_test.go` | streaming/event transport behavior: Stream Open AIRetries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestBuildCodexRequestClampsPromptCacheKey` | `inference/provider/openaicodex/codex_request_test.go` | provider request/payload parity: Build Codex Request Clamps Prompt Cache Key | Guards cache-retention/usage accounting/cost parity. |
| `TestBuildCodexRequestMatchesPiaiShape` | `inference/provider/openaicodex/codex_request_test.go` | provider request/payload parity: Build Codex Request Matches Piai Shape | Guards locally discovered edge cases and Go API compatibility. |
| `TestExtractCodexEventErrorUsesNestedPayload` | `inference/provider/openaicodex/codex_request_test.go` | provider request/payload parity: Extract Codex Event Error Uses Nested Payload | Guards locally discovered edge cases and Go API compatibility. |
| `TestBuildCodexHeadersAddsAccountAndExperimentalHeaders` | `inference/provider/openaicodex/codex_request_test.go` | auth/header/env edge case: Build Codex Headers Adds Account And Experimental Headers | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestStreamViaSSERetries429AndSucceeds` | `inference/provider/openaicodex/codex_retry_test.go` | streaming/event transport behavior: Stream Via SSERetries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamViaWebSocketAutoUsesCachedDeltaAndDebugStats` | `inference/provider/openaicodex/codex_ws_test.go` | streaming/event transport behavior: Stream Via Web Socket Auto Uses Cached Delta And Debug Stats | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestRemoveCodexWebSocketSessionClosesConnection` | `inference/provider/openaicodex/codex_ws_test.go` | streaming/event transport behavior: Remove Codex Web Socket Session Closes Connection | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamCodexWebSocketSetupFailureFallsBackToSSEWithDiagnostic` | `inference/provider/openaicodex/codex_ws_test.go` | streaming/event transport behavior: Stream Codex Web Socket Setup Failure Falls Back To SSEWith Diagnostic | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestStreamCodexRetriesWebSocketConnectionLimitOnceBeforeSSE` | `inference/provider/openaicodex/codex_ws_test.go` | streaming/event transport behavior: rs-ai-origin Codex connection-limit retry: real WebSocket connection receives nested `websocket_connection_limit_reached`, retries one fresh WS handshake, then falls back to SSE | Guards upstream `isWebSocketConnectionLimitReachedError` / `retriedWebSocketConnectionLimit` behavior and catches cached-connection reuse bugs after pre-start WS API errors. |
| `TestStreamViaWebSocketProtocolFlow` | `inference/provider/openaicodex/codex_ws_test.go` | streaming/event transport behavior: Stream Via Web Socket Protocol Flow | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestResolveAzureResponsesConfigUsesEnvAndDeploymentMap` | `inference/provider/openairesponses/responses_azure_test.go` | Resolve Azure Responses Config Uses Env And Deployment Map | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestResolveAzureResponsesConfigNormalizesAzureHost` | `inference/provider/openairesponses/responses_azure_test.go` | Resolve Azure Responses Config Normalizes Azure Host | Guards locally discovered edge cases and Go API compatibility. |
| `TestResponsesUsesExplicitAuthHeaderWithoutAPIKey` | `inference/provider/openairesponses/responses_azure_test.go` | auth/header/env edge case: Responses Uses Explicit Auth Header Without APIKey | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestAzureResponsesRequestAppliesCleanupAndSessionHeaders` | `inference/provider/openairesponses/responses_azure_test.go` | provider request/payload parity: Azure Responses Request Applies Cleanup And Session Headers | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestAzureResponsesNormalizesCommentaryIntoThinkingEvents` | `inference/provider/openairesponses/responses_azure_test.go` | reasoning/thinking wire-format behavior: Azure Responses Normalizes Commentary Into Thinking Events | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestBuildRequestOmitsDefaultReasoningForGitHubCopilot` | `inference/provider/openairesponses/responses_request_test.go` | provider request/payload parity: Build Request Omits Default Reasoning For Git Hub Copilot | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestBuildRequestClampsPromptCacheKey` | `inference/provider/openairesponses/responses_request_test.go` | provider request/payload parity: Build Request Clamps Prompt Cache Key | Guards cache-retention/usage accounting/cost parity. |
| `TestBuildRequestDefaultsReasoningForNonCopilotReasoningModels` | `inference/provider/openairesponses/responses_request_test.go` | provider request/payload parity: Build Request Defaults Reasoning For Non Copilot Reasoning Models | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestBuildAssistantItemsAllowsEmptyThinkingSignature` | `inference/provider/openairesponses/responses_request_test.go` | reasoning/thinking wire-format behavior: Build Assistant Items Allows Empty Thinking Signature | Guards provider-specific reasoning/thinking payload and replay semantics. |
| `TestStreamResponsesRetries429AndSucceeds` | `inference/provider/openairesponses/responses_retry_test.go` | streaming/event transport behavior: Stream Responses Retries429 And Succeeds | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestParseCompleteJSON` | `internal/jsonparse/partial_test.go` | Parse Complete JSON | Guards locally discovered edge cases and Go API compatibility. |
| `TestParsePartialJSON` | `internal/jsonparse/partial_test.go` | Parse Partial JSON | Guards locally discovered edge cases and Go API compatibility. |
| `TestParseEmpty` | `internal/jsonparse/partial_test.go` | Parse Empty | Guards locally discovered edge cases and Go API compatibility. |
| `TestComputeBackoff` | `internal/retry/backoff_test.go` | Compute Backoff | Guards locally discovered edge cases and Go API compatibility. |
| `TestComputeBackoffConstant` | `internal/retry/backoff_test.go` | Compute Backoff Constant | Guards locally discovered edge cases and Go API compatibility. |
| `TestIsRetryableStatus` | `internal/retry/backoff_test.go` | retry/cancellation robustness: Is Retryable Status | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestParseRetryAfter` | `internal/retry/backoff_test.go` | retry/cancellation robustness: Parse Retry After | Guards transport retry/cancellation edge cases and resource cleanup. |
| `TestParseDurationString` | `internal/retry/backoff_test.go` | Parse Duration String | Guards locally discovered edge cases and Go API compatibility. |
| `TestDiscardLoggerDefault` | `logger_test.go` | Discard Logger Default | Guards locally discovered edge cases and Go API compatibility. |
| `TestSimpleLogger` | `logger_test.go` | Simple Logger | Guards locally discovered edge cases and Go API compatibility. |
| `TestLogLevelFiltering` | `logger_test.go` | Log Level Filtering | Guards locally discovered edge cases and Go API compatibility. |
| `TestSetLogger` | `logger_test.go` | Set Logger | Guards locally discovered edge cases and Go API compatibility. |
| `TestSetLoggerNil` | `logger_test.go` | Set Logger Nil | Guards locally discovered edge cases and Go API compatibility. |
| `TestTransformMessagesAddsSyntheticResultForTrailingOrphan` | `logic_audit_test.go` | Transform Messages Adds Synthetic Result For Trailing Orphan | Guards locally discovered edge cases and Go API compatibility. |
| `TestTransformMessagesNilModelReturnsInput` | `logic_audit_test.go` | model registry/generated metadata parity: Transform Messages Nil Model Returns Input | Guards generated registry drift and provider metadata changes. |
| `TestApplyToolCallLimitUsesBudgetTrim` | `logic_audit_test.go` | tool-call/schema conversion behavior: Apply Tool Call Limit Uses Budget Trim | Guards provider-specific tool-call schema/replay/validation parity. |
| `TestRegisterBuiltinModels` | `models_test.go` | model registry/generated metadata parity: Register Builtin Models | Guards generated registry drift and provider metadata changes. |
| `TestGeneratedModelMetadataParity` | `models_test.go` | model registry/generated metadata parity: Generated Model Metadata Parity | Guards generated registry drift and provider metadata changes. |
| `TestListModelsFilter` | `models_test.go` | model registry/generated metadata parity: List Models Filter | Guards generated registry drift and provider metadata changes. |
| `TestPKCE` | `oauth/oauth_test.go` | PKCE | Guards locally discovered edge cases and Go API compatibility. |
| `TestNormalizeDomain` | `oauth/oauth_test.go` | Normalize Domain | Guards locally discovered edge cases and Go API compatibility. |
| `TestGetGitHubCopilotBaseURL` | `oauth/oauth_test.go` | provider OAuth/provider-specific behavior: Get Git Hub Copilot Base URL | Guards OAuth refresh/login/model filtering drift. |
| `TestGitHubCopilotModelFiltering` | `oauth/oauth_test.go` | model registry/generated metadata parity: Git Hub Copilot Model Filtering | Guards generated registry drift and provider metadata changes. |
| `TestIsSelectableCopilotModel` | `oauth/oauth_test.go` | streaming/event transport behavior: Is Selectable Copilot Model | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestGetAPIKeyRefreshesExpiredCredential` | `oauth/oauth_test.go` | auth/header/env edge case: Get APIKey Refreshes Expired Credential | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestGetAPIKeyKeepsValidCredential` | `oauth/oauth_test.go` | auth/header/env edge case: Get APIKey Keeps Valid Credential | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestOAuthRegistryRoundTrip` | `oauth/oauth_test.go` | auth/header/env edge case: OAuth Registry Round Trip | Guards provider auth/header/env precedence bugs not fully covered by upstream tests. |
| `TestParseSSESurfacesReaderErrors` | `transports/sse/sse_error_test.go` | streaming/event transport behavior: Parse SSESurfaces Reader Errors | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestParseSSE` | `transports/sse/sse_test.go` | streaming/event transport behavior: Parse SSE | Guards event stream protocol compatibility and partial-failure behavior. |
| `TestParseMultilineData` | `transports/sse/sse_test.go` | Parse Multiline Data | Guards locally discovered edge cases and Go API compatibility. |
| `TestParseStickyIDAndRetry` | `transports/sse/sse_test.go` | retry/cancellation robustness: Parse Sticky IDAnd Retry | Guards transport retry/cancellation edge cases and resource cleanup. |
