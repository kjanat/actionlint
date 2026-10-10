package ruff

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestOpaqueStatementTemplatesSkipBindingAnalysis(t *testing.T) {
	for _, script := range []string{
		"${{ inputs.setup }}\nprint(value)",
		"  ${{ inputs.setup }}\nprint(value)",
		"if True:\n    ${{ inputs.setup }}\n    print(value)",
		"print(1); ${{ inputs.setup }}; print(value)",
		"if True: ${{ inputs.setup }}; print(value)",
		"if(True): ${{ inputs.setup }}; print(value)",
		"if ';' == ';': ${{ inputs.setup }}; print(value)",
		"if ${{ inputs.condition }}: ${{ inputs.setup }}; print(value)",
		"# setup declaration\n${{ inputs.setup }} # dynamic assignment\nprint(value)",
		"\\\n${{ inputs.setup }}\nprint(value)",
		"print(1); \\\n${{ inputs.setup }}; print(value)",
		"print(${{ inputs.value }})\n${{ inputs.setup }}\nprint(value)",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(script+ending, func(t *testing.T) {
				checker := New(func([]string, string, func([]byte, error) error) {
					t.Fatal("opaque statement template reached Ruff without its bindings")
				}, func() error { return nil }, reviewExpressionEnd)
				python := "python"
				if err := checker.Check(strings.ReplaceAll(script, "\n", ending), &python, "workflow:12", Config{}, func(Diagnostic) {}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestOpaqueStatementTemplatesRealRuff(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		name, script, expansion string
		wantCalls, wantIssues   int
	}{
		{"standalone binding", "${{ inputs.setup }}\nprint(value)", "value = 1\nprint(value)", 0, 0},
		{"indented binding", "if True:\n    ${{ inputs.setup }}\n    print(value)", "if True:\n    value = 1\n    print(value)", 0, 0},
		{"semicolon binding", "print(1); ${{ inputs.setup }}; print(value)", "print(1); value = 1; print(value)", 0, 0},
		{"inline suite binding", "if True: ${{ inputs.setup }}; print(value)", "if True: value = 1; print(value)", 0, 0},
		{"parenthesized inline suite", "if(True): ${{ inputs.setup }}; print(value)", "if(True): value = 1; print(value)", 0, 0},
		{"quoted punctuation inline suite", "if ';' == ';': ${{ inputs.setup }}; print(value)", "if ';' == ';': value = 1; print(value)", 0, 0},
		{"templated inline condition", "if ${{ inputs.condition }}: ${{ inputs.setup }}; print(value)", "if True: value = 1; print(value)", 0, 0},
		{"continued binding", "\\\n${{ inputs.setup }}\nprint(value)", "\\\nvalue = 1\nprint(value)", 0, 0},
		{"continued semicolon binding", "print(1); \\\n${{ inputs.setup }}; print(value)", "print(1); \\\nvalue = 1; print(value)", 0, 0},
		{"value expression", "print(${{ inputs.value }})\nprint(missing)", "", 1, 1},
		{"assignment value", "value = ${{ inputs.value }}\nprint(value)\nprint(missing)", "", 1, 1},
		{"conditional value", "if ${{ inputs.condition }}:\n    print(missing)", "", 1, 1},
		{"dictionary value", "value = {'a': ${{ inputs.value }}}\nprint(missing)", "", 1, 1},
		{"annotation value", "value: ${{ inputs.type }}\nprint(missing)", "", 1, 1},
		{"lambda value", "function = lambda value: ${{ inputs.value }}\nprint(missing)", "", 1, 1},
		{"callable value", "${{ inputs.callable }}()\nprint(missing)", "", 1, 1},
		{"assert value", "assert ${{ inputs.value }}\nprint(missing)", "", 1, 1},
		{"return value", "def f():\n    return ${{ inputs.value }}\nprint(missing)", "", 1, 1},
		{"yield value", "def f():\n    yield ${{ inputs.value }}\nprint(missing)", "", 1, 1},
		{"quoted template", "print('${{ inputs.setup }}')\nprint(missing)", "", 1, 1},
		{"formatted expression", `print(f"{${{ inputs.value }}}")` + "\nprint(missing)", "", 1, 1},
		{"quoted statement template", "'${{ inputs.setup }}'\nprint(missing)", "", 1, 1},
		{"comment template", "# ${{ inputs.setup }}\nprint(missing)", "", 1, 1},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				var result error
				calls := 0
				var diagnostics []Diagnostic
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
						t.Fatalf("Ruff failed: %v: %s", err, stderr.String())
					}
					result = callback(output, nil)
				}, func() error { return result }, reviewExpressionEnd)
				python := "python"
				script := strings.ReplaceAll(tc.script, "\n", ending)
				if err := checker.Check(script, &python, "workflow:12", Config{}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
					t.Fatal(err)
				}
				if err := checker.Wait(); err != nil {
					t.Fatal(err)
				}
				if calls != tc.wantCalls || len(diagnostics) != tc.wantIssues {
					t.Fatalf("calls=%d, diagnostics=%+v", calls, diagnostics)
				}
				if tc.wantIssues == 1 {
					finding := diagnostics[0]
					if finding.Code != "F821" || !strings.Contains(finding.Message, "missing") || finding.Location.Row != strings.Count(script, "\n")+1 {
						t.Fatalf("unrelated undefined name changed: %+v", finding)
					}
				}
				if tc.expansion != "" {
					expansion := strings.ReplaceAll(tc.expansion, "\n", ending)
					if err := checker.Check(expansion, &python, "workflow:12", Config{}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
						t.Fatal(err)
					}
					if err := checker.Wait(); err != nil || calls != 1 || len(diagnostics) != 0 {
						t.Fatalf("valid expanded binding: calls=%d, findings=%+v, error=%v", calls, diagnostics, err)
					}
				}
			})
		}
	}
}
