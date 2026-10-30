package ruff

import (
	"slices"
	"strings"
	"testing"
)

func TestExtraFileOperandsRejected(t *testing.T) {
	for _, flags := range [][]string{{"other.py"}, {"."}, {""}, {"-"}, {"--", "other.py"}, {"--select", "F", "other.py"}, {"--exclude=vendor", "other.py"}} {
		checker := New(func([]string, string, func([]byte, error) error) { t.Error("file operand reached Ruff") }, func() error { return nil }, nil, flags...)
		python := "python"
		if err := checker.Check("print(missing)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "file operands") {
			t.Errorf("flags %q: %v", flags, err)
		}
	}
}

func TestExtraOptionValuesPreserved(t *testing.T) {
	for _, flags := range [][]string{
		{"--select", "F", "--ignore=F401", "--exclude", "vendor/**"},
		{"--config", `lint.dummy-variable-rgx = "--silent"`, "--color", "never"},
		{"--exclude", "generated", "--extend-exclude", "other.py"},
		{"--per-file-ignores", "*.py:F401", "--extend-select", "B", "--preview"},
	} {
		calls := 0
		checker := New(func(args []string, _ string, _ func([]byte, error) error) {
			calls++
			if !slices.Equal(args[1:len(flags)+1], flags) {
				t.Errorf("changed flags: %q", args)
			}
		}, func() error { return nil }, nil, flags...)
		python := "python"
		if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) {}); err != nil || calls != 1 {
			t.Errorf("flags %q: calls=%d, error=%v", flags, calls, err)
		}
	}
}
