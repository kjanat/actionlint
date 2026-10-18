package component

import (
	"context"
	"strings"
	"testing"
)

func TestHostLogSharesOutputBudget(t *testing.T) {
	var lines []string
	w := &lineWriter{log: func(line string) { lines = append(lines, line) }}
	h := hostImplementation{output: w}
	if _, err := w.Write([]byte(strings.Repeat("x", maxPluginOutput-4))); err != nil {
		t.Fatal(err)
	}
	if err := h.Log(context.Background(), "last"); err != nil {
		t.Fatal(err)
	}
	if err := h.Log(context.Background(), "overflow"); err == nil {
		t.Fatal("host log bypassed shared budget")
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("stdio ignored host log overflow")
	}
	for _, line := range lines {
		if len(line) > maxPluginLine {
			t.Fatal("unbounded host log line")
		}
	}
	w.reset()
	lines = nil
	if err := h.Log(context.Background(), "fresh"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "fresh" {
		t.Fatalf("cross-call log: %q", lines)
	}
}

func TestOutputBoundsAndCallBoundary(t *testing.T) {
	var lines []string
	w := &lineWriter{log: func(line string) { lines = append(lines, line) }}
	chunk := strings.Repeat("x", maxPluginLine)
	for range maxPluginOutput / len(chunk) {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write=%d, %v", n, err)
		}
		if cap(w.pending) > maxPluginLine {
			t.Fatalf("retained %d bytes", cap(w.pending))
		}
	}
	if _, err := w.Write([]byte("overflow")); err == nil {
		t.Fatal("output budget ignored")
	}
	if _, err := w.Write([]byte("\n")); err == nil {
		t.Fatal("output error was not sticky")
	}
	for _, line := range lines {
		if len(line) > maxPluginLine {
			t.Fatal("unbounded log entry")
		}
	}
	w.reset()
	lines = nil
	_, _ = w.Write([]byte("first call"))
	w.flush()
	w.reset()
	_, _ = w.Write([]byte("second call\n"))
	if strings.Join(lines, "|") != "first call|second call" {
		t.Fatalf("partial output crossed calls: %q", lines)
	}
}
