package ruff

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func reviewRuffDiagnostics(t *testing.T, scripts ...string) ([]Diagnostic, int) {
	t.Helper()
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	var result error
	var diagnostics []Diagnostic
	calls := 0
	checker := New(func(args []string, source string, callback func([]byte, error) error) {
		calls++
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Stdin = strings.NewReader(source)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			err = nil
		}
		if err != nil {
			t.Fatalf("Ruff: %v: %s", err, &stderr)
		}
		result = callback(output, nil)
	}, func() error { return result }, func(source string) (int, bool) { end := strings.Index(source, "}}"); return end + 2, end >= 0 })
	python := "python"
	for _, script := range scripts {
		if err := checker.Check(script, &python, "test", Config{Select: []string{"E501", "W605", "F821"}}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := checker.Wait(); err != nil {
		t.Fatal(err)
	}
	return diagnostics, calls
}

func TestTemplateLineWidths(t *testing.T) {
	long := strings.Repeat("long_", 24)
	for _, source := range []string{
		"print(${{ inputs." + long + " }})",
		"print('é ${{ inputs." + long + " }}')",
		"print(${{\n inputs." + long + "\n}})",
		"print(${{ inputs.value\n}}) # " + strings.Repeat("long text ", 15),
		"# ${{ inputs." + long + " }}",
		"# ${{\n inputs." + long + "\n}}",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			script := strings.ReplaceAll(source+"\nprint('"+strings.Repeat("long text ", 15)+"')\nprint(missing)", "\n", ending)
			diagnostics, calls := reviewRuffDiagnostics(t, script)
			line := strings.Count(source, "\n") + 2
			if calls != 1 || len(diagnostics) != 2 || diagnostics[0].Code != "E501" || diagnostics[0].Location.Row != line || diagnostics[1].Code != "F821" || diagnostics[1].Location.Row != line+1 {
				t.Errorf("source %q: calls=%d, findings=%+v", script, calls, diagnostics)
			}
		}
	}
}

func TestTemplateStringEscapes(t *testing.T) {
	for _, tc := range []struct {
		source string
		skip   bool
	}{
		{`print("\${{ inputs.escape }}")`, true},
		{`print('\\\${{ inputs.escape }}')`, true},
		{`print("""\${{ inputs.escape }}""")`, true},
		{"print(\"\"\"prefix\n\\${{ inputs.escape }}\"\"\")", true},
		{"print(\"\\${{\n inputs.escape\n}}\")", true},
		{`print(f"\${{ inputs.escape }}")`, true},
		{`print(t"\${{ inputs.escape }}")`, true},
		{`print(b"\${{ inputs.escape }}")`, true},
		{`print("\x${{ inputs.hex }}")`, true},
		{`print("\u12${{ inputs.hex }}")`, true},
		{`print("\U0000${{ inputs.hex }}")`, true},
		{`print("\N{${{ inputs.name }}}")`, true},
		{`print(f"{'\${{ inputs.escape }}'}")`, true},
		{`print("\\${{ inputs.value }}")`, false},
		{`print("\\\\${{ inputs.value }}")`, false},
		{`print("\n${{ inputs.value }}")`, false},
		{`print("\x41${{ inputs.value }}")`, false},
		{`print("\u0041${{ inputs.value }}")`, false},
		{`print("\U00000041${{ inputs.value }}")`, false},
		{`print("\N{LATIN CAPITAL LETTER A}${{ inputs.value }}")`, false},
		{`print(r"\${{ inputs.value }}")`, false},
		{`print(rf"\${{ inputs.value }}")`, false},
		{`print(RF"""\${{ inputs.value }}""")`, false},
		{`print(rt"\${{ inputs.value }}")`, false},
		{`print(br"\${{ inputs.value }}")`, false},
		{`print(b"\u${{ inputs.hex }}")`, false},
		{`print(b"\N{${{ inputs.name }}}")`, false},
		{`print(f"{r'\${{ inputs.value }}'}")`, false},
		{`# \${{ inputs.value }}`, false},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(tc.source, "\n", ending)
			diagnostics, calls := reviewRuffDiagnostics(t, source+ending+"print(missing)")
			if tc.skip {
				if calls != 0 || len(diagnostics) != 0 {
					t.Errorf("escape %q was checked: %+v", tc.source, diagnostics)
				}
				continue
			}
			if calls != 1 || len(diagnostics) != 1 || diagnostics[0].Code != "F821" || diagnostics[0].Location.Row != 2 {
				t.Errorf("safe escape %q lost independent finding: %+v", tc.source, diagnostics)
			}
		}
	}
}

func TestTemplateEscapesPreserveOtherScripts(t *testing.T) {
	diagnostics, calls := reviewRuffDiagnostics(t, `print("\${{ inputs.escape }}")`, "print(missing)")
	if calls != 1 || len(diagnostics) != 1 || diagnostics[0].Code != "F821" || diagnostics[0].Location.Row != 1 {
		t.Fatalf("skipped script affected next check: %+v; calls=%d", diagnostics, calls)
	}
	for _, source := range []string{`print("\q")`, `print(b"\q")`, `print("\q${{ inputs.value }}")`} {
		diagnostics, calls := reviewRuffDiagnostics(t, source+"\nprint(missing)")
		if calls != 1 || len(diagnostics) != 2 || diagnostics[0].Code != "W605" || diagnostics[1].Code != "F821" {
			t.Errorf("genuine escape finding lost in %q: %+v", source, diagnostics)
		}
	}
}
