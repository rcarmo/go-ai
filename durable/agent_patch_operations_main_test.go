package durable

import "testing"

// Source-derived supplementary boundary controls, not a translated test body.
// applyChange in pinned harness/agent.ts assigns objects as whole top-level
// fields and deletes cleared fields; ConfigurePatch retains native validation.
func TestMainAgentPatchFieldOperationBoundaries(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h, c := upstreamCompletedChat(t, b, "unused")
		cwd := "/before"
		if err := c.ConfigurePatch(bg, AgentPatch{Cwd: &cwd}); err != nil {
			t.Fatal(err)
		}
		initial, ch := recordUpstreamWindow(t, c)
		model := ModelRef{Provider: "openai", ID: "replacement"}
		if err := c.ConfigurePatch(bg, AgentPatch{Model: &model, Clear: []string{"cwd"}}); err != nil {
			t.Fatal(err)
		}
		f := watchReceive(t, ch)
		assertConversationOperations(t, f.ops, []Operation{{"d", []any{"docs", "pi.agent", "cwd"}}, {"s", []any{"docs", "pi.agent", "model"}, JSON{"provider": "openai", "id": "replacement"}}}, h.session.limits)
		replayUpstreamWindow(t, h.session, initial, []upstreamWindowFrame{f})
		// No changed top-level fields must be silent, including an explicitly
		// supplied value already present. The following note is a delivery witness.
		if err := c.ConfigurePatch(bg, AgentPatch{}); err != nil {
			t.Fatal(err)
		}
		if err := c.ConfigurePatch(bg, AgentPatch{Model: &model}); err != nil {
			t.Fatal(err)
		}
		note := appendUpstreamHistory(t, h, c, "note", 0)
		frames := awaitUpstreamWindow(t, ch, note)
		if len(frames) != 1 {
			t.Fatal("unchanged patch published document frame", len(frames))
		}
		if len(frames[0].ops) != 1 || frames[0].ops[0][0] != "p" {
			t.Fatal("unchanged patch leaked operations", frames[0].ops)
		}
	})
}
