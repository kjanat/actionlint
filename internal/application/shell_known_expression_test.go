package application_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestKnownShellExternalLinters(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		runner        string
		workflowShell string
		defaults      string
		stepShell     string
		want          string
	}{
		{"Windows object skips ShellCheck", `${{ fromJSON('{"labels":"windows-latest"}') }}`, "", "", "", ""},
		{"Windows labels expression skips ShellCheck", `{labels: "${{ fromJSON('[\"windows-latest\"]') }}"}`, "", "", "", ""},
		{"Windows explicit override", `${{ fromJSON('{"labels":"windows-latest"}') }}`, "", "", "bash", "shellcheck"},
		{"known bash default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"bash"}') }}`, "", "shellcheck"},
		{"known Python default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python"}') }}`, "", ""},
		{"known custom Python default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python -u {0}"}') }}`, "", ""},
		{"known default casing", "ubuntu-latest", "", `${{ fromJSON('{"ShElL":"python"}') }}`, "", ""},
		{"known PowerShell default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"pwsh"}') }}`, "", ""},
		{"known default overrides workflow", "ubuntu-latest", "python", `${{ fromJSON('{"shell":"bash"}') }}`, "", "shellcheck"},
		{"step overrides known default", "ubuntu-latest", "", `${{ fromJSON('{"shell":"python"}') }}`, "bash", "shellcheck"},
		{"dynamic custom Bash retains dispatch", "ubuntu-latest", "", "", `bash ${{ vars.FLAGS }} {0}`, "shellcheck"},
		{"missing shell inherits workflow", "ubuntu-latest", "python", `${{ fromJSON('{"working-directory":"src"}') }}`, "", ""},
		{"empty mapping inherits workflow", "ubuntu-latest", "python", `${{ fromJSON('{}') }}`, "", ""},
		{"unknown map masks workflow Python", "ubuntu-latest", "python", `${{ fromJSON(vars.DEFAULTS) }}`, "", ""},
		{"unknown map masks workflow Bash", "ubuntu-latest", "bash", `${{ fromJSON(vars.DEFAULTS) }}`, "", ""},
		{"invalid map masks workflow", "ubuntu-latest", "python", `${{ fromJSON('{"shell":[]}') }}`, "", ""},
		{"evaluated delimiters stay data", "ubuntu-latest", "python", `${{ fromJSON('{"shell":"${{ secrets.SHELL }}"}') }}`, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\n"
			if tc.workflowShell != "" {
				source += `defaults:
  run:
    shell: ` + tc.workflowShell + "\n"
			}
			source += `jobs:
  test:
    runs-on: ` + tc.runner + "\n"
			if tc.defaults != "" {
				source += "    defaults:\n      run: " + tc.defaults + "\n"
			}
			source += `    steps:
      - run: echo test
`
			if tc.stepShell != "" {
				source += "        shell: " + tc.stepShell + "\n"
			}
			session := application.Options{Tools: (adapters.Options{ShellcheckOptions: &processconfig.Options{Executable: &exe, Arguments: []string{"-test.run=^TestShellcheckPlanningHelper$", "--", "shellcheck"}}}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Stdin, Reader: bytes.NewReader([]byte(source)), Name: "test.yaml", NormalizePath: false})
			if err != nil {
				t.Fatal(err)
			}
			errors := result.Diagnostics
			var actual []string
			for _, diagnostic := range errors {
				if diagnostic.Rule == "shellcheck" || diagnostic.Rule == "pyflakes" {
					actual = append(actual, diagnostic.Rule)
				}
			}
			if strings.Join(actual, ",") != tc.want {
				t.Fatalf("external linters %v, want %q: %v", actual, tc.want, errors)
			}
		})
	}
}
