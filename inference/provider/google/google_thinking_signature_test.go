package google

import "testing"

func isThinkingPartForTest(part geminiPart) bool {
	return part.Thought != nil && *part.Thought
}

func retainThoughtSignatureForTest(existing, next string) string {
	if next != "" {
		return next
	}
	return existing
}

func TestGoogleThinkingSignatureTreatsThoughtTrueAsThinking(t *testing.T) {
	truth := true
	if !isThinkingPartForTest(geminiPart{Thought: &truth}) {
		t.Fatal("thought true without signature should be thinking")
	}
	if !isThinkingPartForTest(geminiPart{Thought: &truth, ThoughtSignature: "opaque-signature"}) {
		t.Fatal("thought true with signature should be thinking")
	}
}

func TestGoogleThinkingSignatureDoesNotTreatThoughtSignatureAloneAsThinking(t *testing.T) {
	f := false
	if isThinkingPartForTest(geminiPart{ThoughtSignature: "opaque-signature"}) {
		t.Fatal("signature alone should not be thinking")
	}
	if isThinkingPartForTest(geminiPart{Thought: &f, ThoughtSignature: "opaque-signature"}) {
		t.Fatal("thought false should not be thinking")
	}
}

func TestGoogleThinkingSignatureDoesNotTreatEmptyOrMissingSignaturesAsThinkingWithoutThought(t *testing.T) {
	f := false
	if isThinkingPartForTest(geminiPart{}) {
		t.Fatal("missing thought/signature should not be thinking")
	}
	if isThinkingPartForTest(geminiPart{Thought: &f, ThoughtSignature: ""}) {
		t.Fatal("thought false empty signature should not be thinking")
	}
}

func TestGoogleThinkingSignaturePreservesExistingSignatureWhenSubsequentDeltasOmitSignature(t *testing.T) {
	first := retainThoughtSignatureForTest("", "sig-1")
	if first != "sig-1" {
		t.Fatalf("first=%q", first)
	}
	second := retainThoughtSignatureForTest(first, "")
	if second != "sig-1" {
		t.Fatalf("second=%q", second)
	}
	third := retainThoughtSignatureForTest(second, "")
	if third != "sig-1" {
		t.Fatalf("third=%q", third)
	}
}

func TestGoogleThinkingSignatureUpdatesWhenNewNonEmptySignatureArrives(t *testing.T) {
	if got := retainThoughtSignatureForTest("sig-1", "sig-2"); got != "sig-2" {
		t.Fatalf("updated=%q", got)
	}
}
