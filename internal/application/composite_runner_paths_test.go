package application_test

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestCompositeContextDirectorySeparators(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	for _, directory := range []string{"scripts", "local/scripts"} {
		writeFixture(t, root, directory+"/lib.sh", "VALUE=42\n")
	}
	if runtime.GOOS != "windows" {
		for _, directory := range []string{`literal\scripts`, `local/literal\scripts`} {
			writeFixture(t, root, directory+"/lib.sh", "VALUE=42\n")
		}
	}
	for _, context := range []string{"github.action_path", "github.workspace"} {
		for _, tc := range []struct {
			runner, suffix string
			known          bool
		}{
			{"windows-latest", `\scripts`, true},
			{"ubuntu-latest", `\scripts`, false},
			{"self-hosted", `\scripts`, false},
			{"ubuntu-latest", `/literal\scripts`, runtime.GOOS != "windows"},
		} {
			t.Run(context+"/"+tc.runner+"/"+tc.suffix, func(t *testing.T) {
				metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '${{ `+context+" }}"+tc.suffix+`'
      run: |
        . ./lib.sh
        echo $VALUE
`)
				workflow := writeFixture(t, root, ".github/workflows/context.yml", `on: push
jobs:
  test:
    runs-on: `+tc.runner+`
    steps:
      - uses: actions/checkout@v6
      - uses: $/local
`)
				session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
				if err != nil {
					t.Fatal(err)
				}
				warned := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
					return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "SC2086")
				})
				if warned == tc.known {
					t.Fatalf("context directory resolution known=%v: %+v", tc.known, result.Diagnostics)
				}
			})
		}
	}
}
