package ruff

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestSanitizeNameRequiredTemplates(t *testing.T) {
	end := func(source string) (int, bool) {
		index := strings.Index(source, "}}")
		return index + 2, index >= 0
	}
	for _, script := range []string{
		"import ${{ inputs.module }}",
		"from ${{ inputs.module }} import name",
		"from json import ${{ inputs.name }}",
		"from json import (\n  ${{ inputs.name }}\n)",
		"import json as ${{ inputs.alias }}",
		"import json, ${{ inputs.module }}",
		"import \\\n  ${{ inputs.module }}",
		"def ${{ inputs.function }}(): pass",
		"def func(${{ inputs.parameter }}): pass",
		"def func(other=0, ${{ inputs.parameter }}=0): pass",
		"class ${{ inputs.class }}: pass",
		"class Example[${{ inputs.type }}]: pass",
		"def func[${{ inputs.type }}](): pass",
		"for ${{ inputs.item }} in []: pass",
		"for first, ${{ inputs.item }} in []: pass",
		"with open('file') as ${{ inputs.file }}: pass",
		"try: pass\nexcept Exception as ${{ inputs.error }}: pass",
		"global ${{ inputs.name }}",
		"nonlocal existing, ${{ inputs.name }}",
		"del ${{ inputs.name }}",
		"lambda ${{ inputs.name }}: 0",
		"${{ inputs.name }} = 0",
		"${{ inputs.name }} += 1",
		"${{ inputs.name }}: int = 1",
		"${{ inputs.name }}, other = (1, 2)",
		"(${{ inputs.name }}, other) = (1, 2)",
		"(\n${{ inputs.name }}, other\n) = (1, 2)",
		"[${{ inputs.name }}, other] = [1, 2]",
		"[${{ inputs.name }}, *other] = [1, 2]",
		"[(${{ inputs.name }}, other), last] = [(1, 2), 3]",
		"def func(item: int, ${{ inputs.parameter }}): pass",
		"def func(item: ${{ inputs.type }}, ${{ inputs.parameter }}): pass",
		"values = [item for ${{ inputs.name }} in []]",
		"(${{ inputs.name }} := 1)",
		"func(${{ inputs.keyword }}=1)",
		"match value:\n    case {\"x\": x, **${{ inputs.rest }}}: pass",
		"match value:\n    case [*${{ inputs.rest }}]: pass",
		"match value:\n    case [first, *${{ inputs.rest }}] if first: pass",
		"match value:\n    case [\n        *${{ inputs.rest }}\n    ]: pass",
		"match value:\n    case ${{ inputs.type }}(): pass",
		"match value:\n    case ${{ inputs.type }} (item): pass",
		"match value:\n    case [${{ inputs.type }}()]: pass",
		"match value:\n    case {\"key\": ${{ inputs.type }}()}: pass",
		"match value:\n    case Outer(${{ inputs.type }}()) if enabled: pass",
		"match value:\n    case case(${{ inputs.type }}()): pass",
		"match value:\n    case (\n        ${{ inputs.type }}\n        ()\n    ): pass",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			script := strings.ReplaceAll(script, "\n", ending)
			t.Run(script, func(t *testing.T) {
				if source, valid, err := Sanitize(script, end); err != nil || valid || source != "" {
					t.Fatalf("name template became Python: %q, valid=%v, err=%v", source, valid, err)
				}
				checker := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("sent name template to Ruff")
				}, func() error { return nil }, end)
				python := "python"
				if err := checker.Check(script, &python, "test", Config{}, func(Diagnostic) {}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, script := range []string{
		"value = ${{ inputs.value }}",
		"print(${{ inputs.value }})",
		"import json\nprint(${{ inputs.value }})",
		"import json # a comment\nprint(${{ inputs.value }})",
		"import json; print(${{ inputs.value }})",
		"print('import'); print(${{ inputs.value }})",
		"print('import ${{ inputs.value }}')",
		"# import ${{ inputs.value }}\nprint(1)",
		"for item in ${{ inputs.items }}: pass",
		"def func(item=${{ inputs.value }}): pass",
		"def func(item: ${{ inputs.type }}): pass",
		"values = [${{ inputs.value }} for item in []]",
		"values = [item for item in ${{ inputs.items }}]",
		"func(${{ inputs.value }}, keyword=1)",
		"func(${{ inputs.value }}, *other)",
		"value = (${{ inputs.value }}, other)",
		"mapping[${{ inputs.key }}] = 1",
		"mapping[${{ inputs.key }}, other] = 1",
		"del mapping[${{ inputs.key }}]\nprint(missing)",
		"del mapping [${{ inputs.key }}]\nprint(missing)",
		"def func(item=call(1, ${{ inputs.value }})): pass",
		"def func(item: tuple[${{ inputs.type }}, ${{ inputs.other }}]): pass",
		"class Example(${{ inputs.base }}): pass",
		"mapping = {${{ inputs.key }}: 1}",
		"lambda item: ${{ inputs.value }}",
		"print(${{ inputs.value }} == 1)",
		"func(*${{ inputs.values }})",
		"mapping = {**${{ inputs.values }}}",
		"values = [*${{ inputs.values }}]",
		"case = [*${{ inputs.values }}]",
		"case(*${{ inputs.values }})",
		"match value:\n    case [*rest] if ${{ inputs.enabled }}: pass",
		"match value:\n    case {\"${{ inputs.key }}\": value}: pass",
		"${{ inputs.callable }}()",
		"case(${{ inputs.callable }}())",
		"case = ${{ inputs.callable }}()",
		"match value:\n    case Widget() if ${{ inputs.callable }}(): pass",
		"match value:\n    case {\"${{ inputs.type }}()\": value}: pass",
	} {
		t.Run(script, func(t *testing.T) {
			if _, valid, err := Sanitize(script, end); err != nil || !valid {
				t.Fatalf("ordinary value template skipped: valid=%v, err=%v", valid, err)
			}
		})
	}
}

func TestRuffImportTemplateReplacement(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		source  string
		invalid bool
	}{
		{"import json\nprint(json)\n", false},
		{"import (0)\n", true},
	} {
		cmd := exec.CommandContext(t.Context(), binary, arguments(Config{})...)
		cmd.Stdin = strings.NewReader(tc.source)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		var exitErr *exec.ExitError
		if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
			t.Fatalf("Ruff failed: %v: %s", err, stderr.String())
		}
		diagnostics, err := Decode(output)
		if err != nil {
			t.Fatal(err)
		}
		invalid := false
		for _, diagnostic := range diagnostics {
			invalid = invalid || diagnostic.Code == "invalid-syntax"
		}
		if invalid != tc.invalid {
			t.Fatalf("source %q: invalid=%v, want %v; %+v", tc.source, invalid, tc.invalid, diagnostics)
		}
	}
}
