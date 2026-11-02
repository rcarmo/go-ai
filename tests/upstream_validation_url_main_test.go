package goai_test

import "testing"

// Original URL snapshot 589 and 103 source-derived controls promoted unchanged.
func TestUpstreamMainValidationURL(t *testing.T) {
	mainRunFormatFixture(t, "validation-url.json", 1, 103)
}

func TestUpstreamMainValidationURLHosts(t *testing.T) {
	mainRunFormatFixture(t, "validation-url-idna.json", 0, 24)
}
