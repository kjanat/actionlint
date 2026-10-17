package engine

import (
	"os"
	"path/filepath"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestCompositeShellcheckSourcedDiagnostics(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	path := writeShellcheckFixture(t, root, "app/lib/check.sh", "echo $VALUE\n")
	writeShellcheckFixture(t, root, "local/action.yml", `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: app
      run: . ./lib/check.sh
`)
	result := compositeAnalysis(t, root, "- uses: ./local", Request{Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"-a"}}}).Registrations()})
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want one sourced finding: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if want := filepath.Join("app", "lib", "check.sh"); finding.Path != want {
		t.Fatalf("want relative diagnostic path %q, got %q", want, finding.Path)
	}
	assertShellcheckSourceFile(t, filepath.Join(root, finding.Path), path)
	if finding.Start != (diagmodel.Position{Line: 1, Column: 6}) || finding.Snippet != "echo $VALUE" {
		t.Fatalf("sourced finding attributed to composite: %+v", finding)
	}
}

func TestCompositeShellcheckSiblingWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, filepath.Dir(root), "shared/value.sh", "VALUE=42\n")
	writeShellcheckFixture(t, root, "local/action.yml", `name: test
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ../shared
      run: |
        . ./value.sh
        echo $VALUE
`)
	result := compositeAnalysis(t, root, "- uses: ./local", Request{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("composite did not analyze sibling source: %+v", result.Diagnostics)
	}
}

func assertShellcheckSourceFile(t *testing.T, got, want string) {
	t.Helper()
	actual, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(actual, expected) {
		t.Fatalf("wrong sourced file: got %q, want %q", got, want)
	}
}
