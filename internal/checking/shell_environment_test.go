package checking_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestShellcheckEnvironmentDeclarations(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("ShellCheck is required for the native environment comparison")
	}
	for _, tc := range []struct {
		name, runner, script string
		want                 []string
	}{
		{"Linux names", "ubuntu-latest", `printf '%s\n' "$lowercase" "$UPPERCASE" "$miXedCase" "$FOO"`, nil},
		{"Windows Bash names", "windows-latest", `printf '%s\n' "$lowercase" "$UPPERCASE" "$miXedCase" "$LOWERCASE"`, []string{"SC2153"}},
		{"missing name", "ubuntu-latest", `printf '%s\n' "$lowercase" "$missing_lowercase"`, []string{"SC2154"}},
		{"misspelled name", "ubuntu-latest", `printf '%s\n' "$MIXEDCASE"`, []string{"SC2153"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "on: push\nenv: {lowercase: lower, UPPERCASE: upper, miXedCase: mixed, foo: outer}\njobs:\n  test:\n    runs-on: " + tc.runner + "\n    env: {FOO: inner, LOWERCASE: override}\n    steps:\n      - shell: bash\n        run: |\n          " + tc.script + "\n"
			report, err := checking.Analyze(t.Context(), checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}, []workspace.Source{{Path: "environment.yml", Content: []byte(content)}})
			if err != nil {
				t.Fatal(err)
			}
			var codes []string
			for _, finding := range report.Diagnostics {
				if finding.Rule == "shellcheck" {
					codes = append(codes, finding.Code)
					if finding.Start.Line != 10 {
						t.Errorf("environment prelude changed the source line: %+v", finding)
					}
				}
			}
			if !slices.Equal(codes, tc.want) {
				t.Fatalf("ShellCheck codes = %v, want %v; findings=%+v", codes, tc.want, report.Diagnostics)
			}
			if tc.name == "Windows Bash names" && !slices.ContainsFunc(report.Diagnostics, func(finding results.Diagnostic) bool {
				return finding.Code == "SC2153" && strings.Contains(finding.Message, "lowercase")
			}) {
				t.Fatal("Windows Bash casing finding lost the exported spelling")
			}
		})
	}
}
