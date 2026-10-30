package responsestream

import "testing"

func TestSlotsExplicitAndLegacyRouting(t *testing.T) {
	var s Slots[string]
	a, b := "a", "b"
	zero, one, unknown := 0, 1, 99
	s.Add(&zero, &a)
	s.Add(&one, &b)
	if s.Get(&zero) != &a || s.Get(&one) != &b || s.Get(nil) != &b || s.Get(&unknown) != nil {
		t.Fatal("explicit/legacy routing")
	}
	s.Delete(&zero)
	if s.Get(nil) != &b || s.Get(&zero) != nil {
		t.Fatal("deleting another slot changed legacy item")
	}
	s.Delete(&unknown)
	if s.Get(nil) != &b {
		t.Fatal("unknown completion changed current item")
	}
	s.Delete(nil)
	if s.Get(&one) != nil || s.Get(nil) != nil {
		t.Fatal("legacy completion left indexed state")
	}
	s.Add(nil, &a)
	if s.Get(&zero) != nil || s.Get(nil) != &a {
		t.Fatal("legacy item adopted an explicit index")
	}
	s.Delete(nil)
	if s.Get(nil) != nil {
		t.Fatal("legacy completion")
	}
}
