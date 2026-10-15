package goai

import "testing"

func TestContentText120PreservesEmptyBlockSeparators(t *testing.T) {
	for _, tc := range []struct {
		blocks []ContentBlock
		want   string
	}{{[]ContentBlock{{Type: "text", Text: ""}, {Type: "text", Text: "second"}}, "\nsecond"}, {[]ContentBlock{{Type: "text", Text: ""}, {Type: "image"}, {Type: "text", Text: ""}}, "\n"}, {[]ContentBlock{{Type: "image"}}, ""}, {[]ContentBlock{{Type: "text", Text: "a"}, {Type: "image"}, {Type: "text", Text: "b"}}, "a\nb"}} {
		if got := ContentText(tc.blocks); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	if got := ContentText([]ContentBlock{{Type: "text"}, {Type: "text", Text: "x"}}, "|"); got != "|x" {
		t.Fatal(got)
	}
}
