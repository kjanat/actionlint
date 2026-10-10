package ruff

import (
	"slices"
	"strings"
	"testing"
)

func TestSilentFlagsRejected(t *testing.T) {
	for _, flag := range []string{"--silent", "-s", "-qs"} {
		t.Run(flag, func(t *testing.T) {
			checker := New(func([]string, string, func([]byte, error) error) {
				t.Fatal("silent mode reached Ruff")
			}, func() error { return nil }, nil, flag)
			python := "python"
			if err := checker.Check("print(missing)", &python, "test", Config{}, func(Diagnostic) {}); err == nil || !strings.Contains(err.Error(), "silent output is not supported") {
				t.Fatalf("silent mode not rejected: %v", err)
			}
		})
	}
}

func TestVersionedShellTarget(t *testing.T) {
	for _, tc := range []struct{ shell, explicit, want string }{
		{"python3.9 {0}", "", "py39"},
		{"/usr/bin/python3.12 -u {0}", "", "py312"},
		{`C:\Python\PYTHON3.10.EXE {0}`, "", "py310"},
		{"python3.9 {0}", "py314", "py314"},
		{"python3.15 {0}", "", "py315"},
		{"python3.6 {0}", "", "py314"},
		{"python3.99 {0}", "", "py314"},
		{"python3 {0}", "", "py314"},
		{"actions-shell python {0}", "", "py314"},
	} {
		t.Run(tc.shell+tc.explicit, func(t *testing.T) {
			calls := 0
			checker := New(func(args []string, _ string, _ func([]byte, error) error) {
				calls++
				if i := slices.Index(args, "--target-version"); i < 0 || args[i+1] != tc.want {
					t.Fatalf("target: %v; want %s", args, tc.want)
				}
			}, func() error { return nil }, nil)
			checker.WorkflowShell(&tc.shell)
			if err := checker.Check("print(1)", nil, "test", Config{TargetVersion: tc.explicit}, func(Diagnostic) {}); err != nil || calls != 1 {
				t.Fatalf("calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestSanitizeOperatorTemplates(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"if lhs ${{ inputs.operator }} rhs: pass",
		"value = lhs ${{ inputs.operator }} (rhs)",
		"value = left() ${{ inputs.operator }} -right",
		"value = 'left' ${{ inputs.operator }} 'right'",
		"value = values[0] ${{ inputs.operator }} other",
		"value = ${{ inputs.left }} ${{ inputs.operator }} other",
		"value = (left\n ${{ inputs.operator }}\n right)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("operator template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
	for _, source := range []string{
		"if ${{ inputs.value }}: pass", "value = left + ${{ inputs.value }}",
		"if False: pass\nelif ${{ inputs.value }}: pass",
		"try: pass\nexcept ${{ inputs.error }}: pass", "raise ${{ inputs.error }}",
		"return ${{ inputs.value }}", "print(${{ inputs.value }})",
		"value = ${{ inputs.value }} + other", "value = ${{ inputs.value }} if test else other",
		"value = [${{ inputs.value }} for item in items]",
		"if left == ${{ inputs.value }} and right: pass",
		"value = 'lhs ${{ inputs.operator }} rhs'", "# lhs ${{ inputs.operator }} rhs",
	} {
		t.Run(source, func(t *testing.T) {
			if _, valid, err := Sanitize(source, end); err != nil || !valid {
				t.Fatalf("value template skipped: %v, %v", valid, err)
			}
		})
	}
}

func TestImportFromTemplatesRemainUnsupported(t *testing.T) {
	end := func(source string) (int, bool) { index := strings.Index(source, "}}"); return index + 2, index >= 0 }
	for _, source := range []string{
		"from ${{ inputs.module }} import name",
		"from module import ${{ inputs.name }}",
		"raise ValueError()\nfrom ${{ inputs.module }} import name",
		"raise ValueError(); from ${{ inputs.module }} import name",
		"def generate():\n    yield 1\n    from ${{ inputs.module }} import name",
		"def generate():\n    yield 1; from ${{ inputs.module }} import name",
		"if True: from ${{ inputs.module }} import name",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(source, "\n", ending)
			t.Run(source, func(t *testing.T) {
				if got, valid, err := Sanitize(source, end); err != nil || valid || got != "" {
					t.Fatalf("import template accepted: %q, %v, %v", got, valid, err)
				}
			})
		}
	}
}
