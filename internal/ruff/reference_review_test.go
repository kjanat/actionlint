package ruff

import (
	"bytes"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestTemplateReferences(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		name, script string
		want         []string
	}{
		{"import", "import os; print(${{ inputs.expression }})", nil},
		{"local", "def f():\n    value = 1\n    return ${{ inputs.expression }}", nil},
		{"annotation", "def f():\n    value: int\n    return ${{ inputs.expression }}", nil},
		{"redefinition", "import os\nprint(${{ inputs.expression }})\nimport os\nprint(os.name)", nil},
		{"export", `import os; __all__ = ["${{ inputs.export }}"]`, nil},
		{"forward annotation", "from typing import Any\ndef f(value: '${{ inputs.annotation }}'): pass", nil},
		{"formatted expression", `import os; print(f"{${{ inputs.expression }}}")`, nil},
		{"multiline", "import os\nprint(${{\n inputs.expression\n}})", nil},
		{"ordinary unused import", "import os; print(1)", []string{"F401"}},
		{"ordinary unused local", "def f():\n    value = 1\n    return 0", []string{"F841"}},
		{"ordinary unused annotation", "def f():\n    value: int\n    return 0", []string{"F842"}},
		{"ordinary redefinition", "import os\nprint(1)\nimport os\nprint(os.name)", []string{"F811"}},
		{"comment", "import os\n# ${{ inputs.expression }}\nprint(1)", []string{"F401"}},
		{"independent duplicate key", "print(${{ inputs.expression }})\nvalue = {1: 2, 1: 3}", []string{"F601"}},
		{"argument", "def f(value):\n    return ${{ inputs.expression }}", nil},
		{"method", "class C:\n    def f(self, value):\n        return ${{ inputs.expression }}", nil},
		{"class method", "class C:\n    @classmethod\n    def f(cls, value):\n        return ${{ inputs.expression }}", nil},
		{"static method", "class C:\n    @staticmethod\n    def f(value):\n        return ${{ inputs.expression }}", nil},
		{"lambda", "function = lambda value: ${{ inputs.expression }}", nil},
		{"loop", "for value in [1]:\n    print(${{ inputs.expression }})", nil},
		{"ordinary argument", "def f(value):\n    return 0", []string{"ARG001"}},
		{"ordinary method", "class C:\n    def f(self, value):\n        return 0", []string{"ARG002"}},
		{"ordinary class method", "class C:\n    @classmethod\n    def f(cls, value):\n        return 0", []string{"ARG003"}},
		{"ordinary static method", "class C:\n    @staticmethod\n    def f(value):\n        return 0", []string{"ARG004"}},
		{"ordinary lambda", "function = lambda value: 0", []string{"ARG005"}},
		{"ordinary loop", "for value in [1]:\n    print(0)", []string{"B007"}},
		{"syntax", "print(${{ inputs.expression }})\nif True print(1)", []string{"invalid-syntax"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				script := strings.ReplaceAll(tc.script+"\nprint(missing)", "\n", ending)
				var diagnostics []Diagnostic
				var result error
				checker := New(func(args []string, source string, callback func([]byte, error) error) {
					cmd := exec.CommandContext(t.Context(), binary, args...)
					cmd.Stdin = strings.NewReader(source)
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					output, runErr := cmd.Output()
					var exitErr *exec.ExitError
					if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
						runErr = nil
					}
					if runErr != nil {
						t.Fatalf("Ruff failed: %v: %s", runErr, stderr.String())
					}
					result = callback(output, runErr)
				}, func() error { return result }, func(source string) (int, bool) {
					end := strings.Index(source, "}}")
					return end + 2, end >= 0
				})
				python := "python"
				if err := checker.Check(script, &python, "workflow:12", Config{Select: []string{"F", "ARG", "B007"}}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
					t.Fatal(err)
				}
				if err := checker.Wait(); err != nil {
					t.Fatal(err)
				}
				var codes []string
				for _, diagnostic := range diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				if slices.Contains(tc.want, "invalid-syntax") {
					if !slices.Equal(codes, tc.want) || diagnostics[0].Location != (Position{Row: 2, Column: 9}) {
						t.Fatalf("independent syntax finding changed: %+v", diagnostics)
					}
					return
				}
				if !slices.Equal(codes, append(slices.Clone(tc.want), "F821")) {
					t.Fatalf("incorrect template-reference findings: %+v", diagnostics)
				}
				last := diagnostics[len(diagnostics)-1]
				if !strings.Contains(last.Message, "missing") || last.Location.Row != strings.Count(script, "\n")+1 || last.Location.Column != 7 {
					t.Fatalf("independent finding changed: %+v", last)
				}
			})
		}
	}
}
