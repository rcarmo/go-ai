package goai_test

import "testing"

// Source-derived null-check/normalisation controls, not new upstream test bodies.
func TestUpstreamMainValidationNullMembership(t *testing.T) {
	mainRunFormatFixture(t, "validation-null-membership.json", 0, 61)
}
