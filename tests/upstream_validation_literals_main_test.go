package goai_test

import "testing"

func TestUpstreamMainValidationLiterals(t *testing.T) {
	mainRunFormatFixture(t, "validation-literals-main.json", 0, 185)
}
