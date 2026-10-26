package text

import "testing"

func TestLFLine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		line   int
		want   string
		ok     bool
	}{
		{name: "first line", source: "first\nsecond", line: 1, want: "first", ok: true},
		{name: "second line", source: "first\nsecond", line: 2, want: "second", ok: true},
		{name: "CRLF", source: "first\r\nsecond", line: 1, want: "first", ok: true},
		{name: "BOM is data", source: "\ufefffirst\nsecond", line: 1, want: "\ufefffirst", ok: true},
		{name: "CR is not a separator", source: "first\rsecond", line: 2},
		{name: "NEL is not a separator", source: "first\u0085second", line: 2},
		{name: "LS is not a separator", source: "first\u2028second", line: 2},
		{name: "PS is not a separator", source: "first\u2029second", line: 2},
		{name: "zero line", source: "first", line: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LFLine([]byte(tc.source), tc.line)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("LFLine(%q, %d) = (%q, %t), want (%q, %t)", tc.source, tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}
