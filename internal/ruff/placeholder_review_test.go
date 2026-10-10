package ruff

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInspectionAndArgumentFilesRejected(t *testing.T) {
	argfile := filepath.Join(t.TempDir(), "ruff-args.txt")
	if err := os.WriteFile(argfile, []byte("--silent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--show-files", "--show-settings", "@" + argfile, "@missing.txt"} {
		t.Run(flag, func(t *testing.T) {
			called := false
			checker := New(func([]string, string, func([]byte, error) error) { called = true }, func() error { return nil }, nil, flag)
			python := "python"
			err := checker.Check("print(missing)", &python, "workflow:12", Config{}, func(Diagnostic) {})
			if called || err == nil || !strings.Contains(err.Error(), "workflow:12") {
				t.Fatalf("unsupported argument reached Ruff: called=%v, error=%v", called, err)
			}
		})
	}
}

func TestIndependentTemplateValues(t *testing.T) {
	binary, err := exec.LookPath("ruff")
	if err != nil {
		t.Skip("Ruff is not installed")
	}
	for _, tc := range []struct {
		script string
		extra  []string
	}{
		{"d = {${{ inputs.first }}: 1, ${{ inputs.second }}: 2}", nil},
		{"d = {${{ inputs.first }}: 1, 0: 2}", nil},
		{"d = {'${{ inputs.first }}': 1, '${{ inputs.second }}': 2}", nil},
		{"d = {'${{ inputs.first }}': 1, '${{ inputs.other }}': 2}", nil},
		{"d = {${{\n inputs.first\n}}: 1, ${{\n inputs.second\n}}: 2}", nil},
		{"_0 = 0\nd = {${{ inputs.first }}: 1, _0: 2}", nil},
		{"_０ = 0\nd = {${{ inputs.first }}: 1, _０: 2}", nil},
		{"é = 'ok'; d = {${{ inputs.first }}: 1, ${{ inputs.other }}: 2}", nil},
		{"d = {${{0}}: 1, ${{1}}: 2}", nil},
		{"def f(value: '${{ inputs.type }}'): pass", nil},
		{"def f(value: ${{ inputs.type }}): pass", nil},
		{"d = {0: 1, 0: 2}; print(${{ inputs.first }})", []string{"F601"}},
		{"print(_0); print(${{ inputs.first }})", []string{"F821"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			script := strings.ReplaceAll(tc.script+"\nprint(missing)", "\n", ending)
			t.Run(script, func(t *testing.T) {
				var result error
				var diagnostics []Diagnostic
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
				if err := checker.Check(script, &python, "workflow:12", Config{}, func(d Diagnostic) { diagnostics = append(diagnostics, d) }); err != nil {
					t.Fatal(err)
				}
				if err := checker.Wait(); err != nil {
					t.Fatal(err)
				}
				var codes []string
				for _, diagnostic := range diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				if !slices.Equal(codes, append(slices.Clone(tc.extra), "F821")) {
					t.Fatalf("template masking changed findings: %+v", diagnostics)
				}
				last := diagnostics[len(diagnostics)-1]
				if !strings.Contains(last.Message, "missing") || last.Location.Row != strings.Count(script, "\n")+1 || last.Location.Column != 7 {
					t.Fatalf("template masking changed independent diagnostics: %+v", diagnostics)
				}
			})
		}
	}
}

func TestOpaqueTemplatePatternSkips(t *testing.T) {
	for _, pattern := range []string{"${{ inputs.value }}", "[${{ inputs.value }}]", "{${{ inputs.key }}: value}"} {
		checker := New(func([]string, string, func([]byte, error) error) {
			t.Fatal("opaque placeholder introduced a pattern binding")
		}, func() error { return nil }, func(source string) (int, bool) {
			end := strings.Index(source, "}}")
			return end + 2, end >= 0
		})
		python := "python"
		if err := checker.Check("match value:\n    case "+pattern+": pass", &python, "test", Config{}, func(Diagnostic) {}); err != nil {
			t.Fatal(err)
		}
	}
}
