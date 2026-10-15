# 1.3.0 port status

Target official71023aec1546d6d677a65669c5665c0927ce0dd0, baseline official1.2.0 06a466935536f8c9bcacb2d5efc0fc021be1bdff. Native1.2.0 tag remains unchanged. All3npm archives match registry SHA512; cryptographic signatures not yet refreshed. Relevant changed-path inventory78paths from226-file compare (below API300limit).

First implementation slice: completed-tool JSON object parser with control/invalid-Unicode-escape repair; explicit ArgumentsError in ToolCall/ContentBlock and early argument validation, OpenAI Chat completed-call propagation and ordinary harness extraction. Streaming partial parsing remains separate. Focused parser/OpenAI/tool-validation race tests pass. Remaining providers, durable receipt/error propagation and end-to-end malformed-call fixtures are not yet ported. No full1.3 acceptance or catalogue regeneration claim.

Next: finish malformed-call production propagation, cross-model thinking/model-effort/provider changes, catalogue delta, durable SQLite prefix/baseline changes and codemode worker lifetime/capacity acquisition. Stay within official1.3 changed contracts; no architecture/lint/release side quests.

## Completed argument propagation checkpoint

Anthropic, Bedrock, Mistral, Responses and both Codex completion loops now use completed-object parsing and carry ArgumentsError into partial terminal content and end events. Durable assistant receipts/checkpoints preserve the marker; execution rejects it before hooks/host effects. TestTool130MalformedArgumentsNeverExecute covers memory/journal end-to-end error-result continuation, zero effects, race count3 passed6.052s. Full affected provider race suites pass (Anthropic5.736s/Bedrock4.285s/Mistral1.256s/Responses2.152s/Codex2.261s), plus vet/diff checks. Pi-messages wire propagation, nonstream/provider-specific raw cases and complete1.3 end-to-end malformed fixtures remain under audit. No full1.3 acceptance claim.

## Cross-model thinking

TransformMessages now drops all thinking when provider/API/model identity differs, instead of exposing readable reasoning as text. Same-model signed/redacted blocks survive; unsigned whitespace-only thinking drops. New identity matrix checks caller isolation; model-selection expectation updated to1.3 contract. Root-wide Thinking/Transform race selection passes (consumer2.056s, affected providers pass); no full-suite acceptance claim. Anthropic error-result image extraction/order is the next observed gap: current native path flattens results to text and can lose image content.

## Anthropic tool-result images

Anthropic now retains image blocks inside successful ordinary results; errored results keep text/placeholder blocks only and move images with a call-ID caption after every tool_result in the adjacent result group. Deferred-reference sibling text/images remain intact after grouped results. Tests assert successful/error image preservation and ordering, one placeholder per consecutive image run, error flag, caller isolation and updated sibling expectations. Full Anthropic race count3 passes6.922s plus vet/diff. Other Anthropic1.3 thinking-off/effort and Bedrock image conversion deltas remain separate audit work.

## Anthropic explicit thinking off

Managed-effort requests now honour explicit native Reasoning pointer off/empty when the model supports off: disabled thinking without block_binding/output effort or historical/final effort markers; terminal providerThinkingLevel omitted. Omitted reasoning keeps managed adaptive defaults; explicit off=null model mapping remains unsupported and preserves adaptive mode. Request regressions and full Anthropic race count3 pass7.095s, vet/diff pass. No full1.3 acceptance.

## Responses cache-preserving effort

Added Responses SupportsMidConvoEffort DTO/generator emission. Gated explicit reasoning requests emit configuration_update before same-provider/API/model historical assistants with providerThinkingLevel, then current effort; request-level effort stays medium. Other model history, disabled capability, omitted reasoning and ChatGPT sign-in remain unmarked. Stream output receives the effective providerThinkingLevel through the request view. New payload gate/history regression passes; full Responses race count3 2.510s plus generator compile/vet/diff pass. Updated fallback-message-ID fixture to1.3 cross-model thinking removal. Catalogue regeneration and full HTTP terminal metadata acceptance still pending; no full1.3 claim.

## Pinned1.3 catalogue refresh

Regenerated from verified npm schema-v6 JSON:1614chat/60image/40classifier (41/1/6providers). Manifest structure f5b811296040611e9314534f2fb2464a007a47efe8b11b1e07dde80651a70bd4 pinned; archive/source1.3 comparator pins updated. Generated Kimi Coding now Chat Completions at /coding/v1; OpenCode endpoint/model/compat and Responses effort flags come from pinned data. Assertions updated for removed/replaced models and actual prices/counts, not generated file hand edits. Consumer full race29.823s, exact regeneration/negative drift, offline reproducibility/twelve atomic failure cases pass. Generator CPU/alloc_space/alloc_objects reviewed (short40ms CPU samples; catalogue JSON/reflection dominates, no optimisation claim); Python negative harness peak4,420,670bytes. Raw helper summaries/logs and failed temporary fixture output disposed. No full1.3 acceptance; runtime endpoint/auth protocol changes still need production-path audit.

## Durable system baseline

Native context derivation/cache extension now distinguish explicit pi.system/head baseline contributions from later system deltas: baseline can lead imported assistant/tool history, while an ordinary later delta does not leapfrog assistants. Effective edited contributions are inspected; cached extension uses the same decision. Memory/journal imported-history/head/incremental regression and existing context/tool-planning/entry-context race count3 pass; durable vet/diff checks pass. SQLite prefix/cloudflare coordination changes are not yet implemented or waved away.
