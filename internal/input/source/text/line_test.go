package text

import (
	"strings"
	"testing"
)

var lfLineResult string

func TestLFLine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source []byte
		line   int
		want   string
		ok     bool
	}{
		{name: "first line", source: []byte("first\nsecond"), line: 1, want: "first", ok: true},
		{name: "second line", source: []byte("first\nsecond"), line: 2, want: "second", ok: true},
		{name: "CRLF", source: []byte("first\r\nsecond"), line: 1, want: "first", ok: true},
		{name: "blank line after CRLF", source: []byte("a\r\n\nb"), line: 2, want: "", ok: true},
		{name: "initial LF", source: []byte("\nb"), line: 1, want: "", ok: true},
		{name: "initial CRLF", source: []byte("\r\nb"), line: 1, want: "", ok: true},
		{name: "CRLF second line", source: []byte("first\r\nsecond"), line: 2, want: "second", ok: true},
		{name: "CRLF second line preserves bare EOF CR", source: []byte("first\r\nsecond\r"), line: 2, want: "second\r", ok: true},
		{name: "lone EOF CR is data", source: []byte("first\r"), line: 1, want: "first\r", ok: true},
		{name: "mixed CR then CRLF", source: []byte("first\r\r\nsecond"), line: 1, want: "first\r", ok: true},
		{name: "empty source is one empty line", source: []byte{}, line: 1, want: "", ok: true},
		{name: "empty source has no second line", source: []byte{}, line: 2},
		{name: "trailing LF adds empty line", source: []byte("first\n"), line: 2, want: "", ok: true},
		{name: "trailing LF has no third line", source: []byte("first\n"), line: 3},
		{name: "BOM is data", source: []byte("\ufefffirst\nsecond"), line: 1, want: "\ufefffirst", ok: true},
		{name: "CR is not a separator", source: []byte("first\rsecond"), line: 1, want: "first\rsecond", ok: true},
		{name: "CR is not a separator so no second line", source: []byte("first\rsecond"), line: 2},
		{name: "NEL is not a separator", source: []byte("first\u0085second"), line: 1, want: "first\u0085second", ok: true},
		{name: "NEL is not a separator so no second line", source: []byte("first\u0085second"), line: 2},
		{name: "LS is not a separator", source: []byte("first\u2028second"), line: 1, want: "first\u2028second", ok: true},
		{name: "LS is not a separator so no second line", source: []byte("first\u2028second"), line: 2},
		{name: "PS is not a separator", source: []byte("first\u2029second"), line: 1, want: "first\u2029second", ok: true},
		{name: "PS is not a separator so no second line", source: []byte("first\u2029second"), line: 2},
		{name: "invalid UTF-8 and NUL are data", source: []byte{'a', 0xff, 0, '\n'}, line: 1, want: string([]byte{'a', 0xff, 0}), ok: true},
		{name: "negative line", source: []byte("first"), line: -1},
		{name: "zero line", source: []byte("first"), line: 0},
		{name: "line past EOF", source: []byte("first"), line: 2},
		{name: "huge line", source: []byte("first"), line: int(^uint(0) >> 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LFLine(tc.source, tc.line)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("LFLine(%q, %d) = (%q, %t), want (%q, %t)", tc.source, tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func BenchmarkLFLine(b *testing.B) {
	for _, size := range []struct {
		name string
		size int
	}{
		{name: "small", size: 64},
		{name: "1MiB", size: 1 << 20},
	} {
		source := []byte("short\n" + strings.Repeat("x", size.size))
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				lfLineResult, _ = LFLine(source, 1)
			}
		})
	}
}
