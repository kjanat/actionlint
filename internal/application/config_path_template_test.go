package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestShellcheckWorkflowPathValidation(t *testing.T) {
	command := shellcheckForTest(t)
	for _, selection := range []struct{ config, message string }{
		{`"${{ github.action_path }}/.shellcheckrc"`, "requires an analyzed composite action"},
		{`{source-path: ["${{ github.action_path }}/lib"]}`, "requires an analyzed composite action"},
		{`{source-path: ["${{ env.HOME }}"]}`, "unknown path interpolation"},
	} {
		t.Run(selection.config, func(t *testing.T) {
			for _, tc := range []struct{ name, step string }{
				{"uses only", "uses: actions/checkout@v6"},
				{"python", "shell: python\n        run: print('hello')"},
				{"powershell", "shell: pwsh\n        run: Write-Output hello"},
				{"bash", "shell: bash\n        run: echo hello"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					config := writeFixture(t, root, "actionlint.yaml", `tools:
  shellcheck:
    config: `+selection.config+"\n")
					workflow := writeFixture(t, root, "workflow.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - `+tc.step+"\n")
					session := application.Options{WorkingDir: root, ConfigFile: config, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
					var err error

					_, err = application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
					if err == nil || !strings.Contains(err.Error(), selection.message) {
						t.Fatalf("ordinary workflow accepted invalid configuration: %v", err)
					}
				})
			}
		})
	}
}

func TestShellcheckInterpolatedPaths(t *testing.T) {
	command := shellcheckForTest(t)
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "checkout")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_WORKSPACE", workspace)
	writeFixture(t, workspace, "shared/env.sh", "VALUE=42\n")
	rc := writeFixture(t, workspace, "rc/.shellcheckrc", "disable=SC2086\n")
	workflow := writeFixture(t, repository, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          . env.sh
          echo $VALUE
`)
	for _, selection := range []string{
		`"${{ github.workspace }}/rc/.shellcheckrc"`,
		`{source-path: ["${{ github.workspace }}/shared"]}`,
	} {
		config := writeFixture(t, repository, ".github/actionlint.yaml", "tools: {shellcheck: {config: "+selection+"}}\n")
		session := application.Options{WorkingDir: repository, ConfigFile: config, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
		var err error

		result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("interpolated config did not apply: %+v, %v", result, err)
		}
		if strings.Contains(selection, ".shellcheckrc") {
			found := false
			for _, input := range result.Inputs {
				found = found || input == rc
			}
			if !found {
				t.Fatalf("interpolated rc missing from inputs: %v", result.Inputs)
			}
		}
	}
}
