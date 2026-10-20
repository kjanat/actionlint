package application_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestCompositeCheckoutEnvironmentPlacement(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	writeFixture(t, root, "refresh/action.yml", `name: refresh
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`)
	for _, scope := range []string{"workflow", "job", "step", "composite"} {
		for _, tc := range []struct {
			name, environment string
			read              bool
		}{
			{"redirected worktree", "GIT_WORK_TREE: /tmp/tree", false},
			{"Git wrapper", "PATH: tools", false},
			{"ordinary env", "MY_VALUE: enabled", true},
			{"empty loader", "NODE_OPTIONS: ''", true},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				workflowEnv, jobEnv, stepEnv := "", "", ""
				call := "actions/checkout@v6"
				switch scope {
				case "workflow":
					workflowEnv = "env: {" + tc.environment + "}\n"
				case "job":
					jobEnv = "    env: {" + tc.environment + "}\n"
				case "step", "composite":
					stepEnv = "        env: {" + tc.environment + "}\n"
					if scope == "composite" {
						call = "$/refresh"
					}
				}
				workflow := writeFixture(t, root, ".github/workflows/environment.yml", "on: push\n"+workflowEnv+`jobs:
  test:
    runs-on: ubuntu-latest
`+jobEnv+"    steps:\n      - uses: "+call+"\n"+stepEnv+"      - uses: ./local\n")
				session := application.Options{WorkingDir: root}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
				if err != nil {
					t.Fatal(err)
				}
				if slices.Contains(result.Inputs, metadata) != tc.read {
					t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
				}
			})
		}
	}
}

func TestCompositeWorkingDirectorySuffixSources(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	writeFixture(t, root, "local/scripts/Nested/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest"} {
		for _, tc := range []struct{ name, spec, directory string }{
			{"workspace", "./source/local", "source/SCRIPTS/nested"},
			{"independent", "$/local", "${{ github.action_path }}/SCRIPTS/nested"},
		} {
			t.Run(runner+"/"+tc.name, func(t *testing.T) {
				metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: `+tc.directory+`
      run: |
        . ./lib.sh
        echo $VALUE
        echo $OTHER
`)
				workflow := writeFixture(t, root, ".github/workflows/suffix.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: source}
      - uses: `+tc.spec+"\n")
				session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
				if err != nil {
					t.Fatal(err)
				}
				var findings []diagmodel.Diagnostic
				for _, d := range result.Diagnostics {
					if d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata {
						findings = append(findings, d)
					}
				}
				if len(findings) != 1 || findings[0].Start.Line != 11 || !strings.Contains(findings[0].Message, "SC2086") {
					t.Fatalf("only unsourced variable should warn: %+v", result.Diagnostics)
				}
			})
		}
	}
}
