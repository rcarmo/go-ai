package responsestream

import "testing"

func TestToolLoadIDPinnedVectorsMain(t *testing.T) {
	for _, tc := range []struct {
		marker string
		names  []string
		want   string
	}{{"system:1", []string{"b"}, "pi_tool_load_ngzlf2gp9pex"}, {"system:3", []string{"a", "b"}, "pi_tool_load_wqgd6dxf5ssr"}, {"system:2", []string{"名字😀"}, "pi_tool_load_mydtqf1m3ahb0"}, {"system:0", []string{"\xed\xa0\x80"}, "pi_tool_load_1yoq8pr1ib0t2u"}} {
		if got := ToolLoadID(tc.marker, tc.names); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
