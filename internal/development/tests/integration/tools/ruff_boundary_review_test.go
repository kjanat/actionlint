package tools_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
)

func TestRuffTemplateParenthesisFindings(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "ruff")
	config, err := checking.ConfigurationRules().Parse([]byte("tools: {ruff: {select: [F, UP034]}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		script string
		codes  []string
	}{
		{"print(${{ '1' }})", []string{"F821"}},
		{"print(${{\n '1'\n}})", []string{"F821"}},
		{"print(${{ '1' }}, ${{ '2' }},)", []string{"F821"}},
		{"print(${{ '1' }}); print(${{ '2' }})", []string{"F821"}},
		{"é = 1; print(${{ '1' }})", []string{"F821"}},
		{"print((${{ '1' }}))", []string{"UP034", "F821"}},
		{"print(((${{ '1' }})))", []string{"UP034", "F821"}},
		{"print(${{ '1' }}); print((1))", []string{"UP034", "F821"}},
		{"print('(${{ '1' }})')", []string{"F821"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.script+ending, func(t *testing.T) {
				t.Parallel()
				source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          ` + strings.ReplaceAll(tc.script+"\nprint(missing)", "\n", "\n          ") + "\n" + "\npermissions: {}\n"
				result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Command: command}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}})
				if err != nil {
					t.Fatal(err)
				}
				ruff := ruffFindingsWithInterpolation(t, result.Diagnostics, templateDiagnosticLines(tc.script, 8)...)
				var codes []string
				for _, finding := range ruff {
					codes = append(codes, finding.Code)
				}
				if !slices.Equal(codes, tc.codes) {
					t.Fatalf("template parentheses changed diagnostics: %+v", result.Diagnostics)
				}
				last := ruff[len(ruff)-1]
				if last.Start.Line != 9+strings.Count(tc.script, "\n") || last.Start.Column != 17 {
					t.Fatalf("independent finding moved: %+v", last)
				}
			})
		}
	}
}

func assertRuffStdinDirectory(t *testing.T, root, working string) {
	t.Helper()
	command := testutil.RequireTool(t, "ruff")
	config, err := checking.ConfigurationRules().Parse([]byte("tools: {ruff: {select: [F, INP001]}}"))
	if err != nil {
		t.Fatal(err)
	}
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
permissions: {}
`
	result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, WorkingDir: working}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" {
		t.Fatalf("synthetic filename acquired package findings: %+v", result.Diagnostics)
	}
}

func linkDirectory(t *testing.T, target string) string {
	t.Helper()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	return alias
}

func createDirectory(t *testing.T, path string) string {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuffStdinUsesCommandDirectory(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{"absolute", "symlink", "symlink-parent"} {
		t.Run(directory, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			working := t.TempDir()
			switch directory {
			case "symlink":
				working = linkDirectory(t, root)
			case "symlink-parent":
				working = linkDirectory(t, createDirectory(t, filepath.Join(root, "nested"))) + string(filepath.Separator) + ".."
			}
			assertRuffStdinDirectory(t, root, working)
		})
	}
}

func TestRuffStdinUsesRelativeCommandDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	createDirectory(t, filepath.Join(root, "python"))
	assertRuffStdinDirectory(t, root, "python")
}

func TestRuffStdinUsesLogicalCommandDirectory(t *testing.T) {
	root := t.TempDir()
	target := createDirectory(t, filepath.Join(root, "nested"))
	t.Chdir(target)
	t.Setenv("PWD", linkDirectory(t, target))
	assertRuffStdinDirectory(t, root, "..")
}

func TestRuffAPIIntegrationFlags(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "ruff")
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
permissions: {}
`
	for _, flags := range [][]string{{"--no-cache"}, {"--isolated"}, {"--ignore-noqa"}, {"--no-fix"}, {"--target-version", "py314"}, {"--target-version=py314"}, {"--stdin-filename", "actionlint.py"}, {"--stdin-filename=actionlint.py"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			t.Parallel()
			_, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: flags}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
			if err == nil || !strings.Contains(err.Error(), "integration-owned option") {
				t.Fatalf("integration flag not rejected: %v", err)
			}
		})
	}
}

func TestRuffRepeatableSelectionFlags(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "ruff")
	config, err := checking.ConfigurationRules().Parse([]byte("tools: {ruff: {select: [F], ignore: [F401]}}"))
	if err != nil {
		t.Fatal(err)
	}
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: import os; print(missing)
permissions: {}
`
	for _, flags := range [][]string{{"--select", "F"}, {"--ignore", "F401"}, {"--select=F", "--ignore=F401"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			t.Parallel()
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir(), Tools: registry.Registrations(registry.Selections{"ruff": {Process: &processconfig.Options{Executable: &command, Arguments: flags}}})}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" {
				t.Fatalf("repeatable selection flags changed findings: %+v", result.Diagnostics)
			}
		})
	}
}
