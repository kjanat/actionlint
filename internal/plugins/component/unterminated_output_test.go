package component

import (
	"strings"
	"testing"
)

func TestUnterminatedOutputRemainsBounded(t *testing.T) {
	const lineLimit = 64 << 10
	const total = 256 << 10
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "logging", true: "disabled"}[disabled], func(t *testing.T) {
			var captured strings.Builder
			writer := &lineWriter{log: func(line string) {
				if len(line) > lineLimit {
					t.Fatalf("log chunk grew to %d bytes", len(line))
				}
				if !disabled {
					captured.WriteString(line)
				}
			}}
			chunk := []byte(strings.Repeat("x", 1024))
			for range total / len(chunk) {
				if n, err := writer.Write(chunk); err != nil || n != len(chunk) {
					t.Fatalf("write=%d err=%v", n, err)
				}
				if len(writer.pending) > lineLimit || cap(writer.pending) > 2*lineLimit {
					t.Fatalf("unterminated line retained len=%d cap=%d", len(writer.pending), cap(writer.pending))
				}
			}
			if _, err := writer.Write([]byte("\n")); err != nil {
				t.Fatal(err)
			}
			if !disabled && captured.String() != strings.Repeat("x", total) {
				t.Fatalf("streamed %d of %d bytes", captured.Len(), total)
			}
		})
	}
}
