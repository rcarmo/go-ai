# 1.3.0 port status

Target official71023aec1546d6d677a65669c5665c0927ce0dd0, baseline official1.2.0 06a466935536f8c9bcacb2d5efc0fc021be1bdff. Native1.2.0 tag remains unchanged. All3npm archives match registry SHA512; cryptographic signatures not yet refreshed. Relevant changed-path inventory78paths from226-file compare (below API300limit).

First implementation slice: completed-tool JSON object parser with control/invalid-Unicode-escape repair; explicit ArgumentsError in ToolCall/ContentBlock and early argument validation, OpenAI Chat completed-call propagation and ordinary harness extraction. Streaming partial parsing remains separate. Focused parser/OpenAI/tool-validation race tests pass. Remaining providers, durable receipt/error propagation and end-to-end malformed-call fixtures are not yet ported. No full1.3 acceptance or catalogue regeneration claim.

Next: finish malformed-call production propagation, cross-model thinking/model-effort/provider changes, catalogue delta, durable SQLite prefix/baseline changes and codemode worker lifetime/capacity acquisition. Stay within official1.3 changed contracts; no architecture/lint/release side quests.

## Completed argument propagation checkpoint

Anthropic, Bedrock, Mistral, Responses and both Codex completion loops now use completed-object parsing and carry ArgumentsError into partial terminal content and end events. Durable assistant receipts/checkpoints preserve the marker; execution rejects it before hooks/host effects. TestTool130MalformedArgumentsNeverExecute covers memory/journal end-to-end error-result continuation, zero effects, race count3 passed6.052s. Full affected provider race suites pass (Anthropic5.736s/Bedrock4.285s/Mistral1.256s/Responses2.152s/Codex2.261s), plus vet/diff checks. Pi-messages wire propagation, nonstream/provider-specific raw cases and complete1.3 end-to-end malformed fixtures remain under audit. No full1.3 acceptance claim.

## Cross-model thinking

TransformMessages now drops all thinking when provider/API/model identity differs, instead of exposing readable reasoning as text. Same-model signed/redacted blocks survive; unsigned whitespace-only thinking drops. New identity matrix checks caller isolation; model-selection expectation updated to1.3 contract. Root-wide Thinking/Transform race selection passes (consumer2.056s, affected providers pass); no full-suite acceptance claim. Anthropic error-result image extraction/order is the next observed gap: current native path flattens results to text and can lose image content.
