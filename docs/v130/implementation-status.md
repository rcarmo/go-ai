# 1.3.0 port status

Target official71023aec1546d6d677a65669c5665c0927ce0dd0, baseline official1.2.0 06a466935536f8c9bcacb2d5efc0fc021be1bdff. Native1.2.0 tag remains unchanged. All3npm archives match registry SHA512; cryptographic signatures not yet refreshed. Relevant changed-path inventory78paths from226-file compare (below API300limit).

First implementation slice: completed-tool JSON object parser with control/invalid-Unicode-escape repair; explicit ArgumentsError in ToolCall/ContentBlock and early argument validation, OpenAI Chat completed-call propagation and ordinary harness extraction. Streaming partial parsing remains separate. Focused parser/OpenAI/tool-validation race tests pass. Remaining providers, durable receipt/error propagation and end-to-end malformed-call fixtures are not yet ported. No full1.3 acceptance or catalogue regeneration claim.

Next: finish malformed-call production propagation, cross-model thinking/model-effort/provider changes, catalogue delta, durable SQLite prefix/baseline changes and codemode worker lifetime/capacity acquisition. Stay within official1.3 changed contracts; no architecture/lint/release side quests.
