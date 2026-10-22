package tools_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	repoconfig "actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/testutil"
	"actionlint.kjanat.dev/internal/tools/adapters"
	"actionlint.kjanat.dev/internal/workspace"
)

func TestRuffTemplateParenthesisFindings(t *testing.T) {
	command := testutil.RequireRuff(t)
	config, err := repoconfig.Parse([]byte("tools: {ruff: {select: [F, UP034]}}"))
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
				source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          ` + strings.ReplaceAll(tc.script+"\nprint(missing)", "\n", "\n          ") + "\n"
				result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{Ruff: command}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(strings.ReplaceAll(source, "\n", ending)), Config: config}})
				if err != nil {
					t.Fatal(err)
				}
				var codes []string
				for _, diagnostic := range result.Diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				if !slices.Equal(codes, tc.codes) {
					t.Fatalf("template parentheses changed diagnostics: %+v", result.Diagnostics)
				}
				last := result.Diagnostics[len(result.Diagnostics)-1]
				if last.Start.Line != 9+strings.Count(tc.script, "\n") || last.Start.Column != 17 {
					t.Fatalf("independent finding moved: %+v", last)
				}
			})
		}
	}
}

func TestRuffStdinUsesCommandDirectory(t *testing.T) {
	command := testutil.RequireRuff(t)
	config, err := repoconfig.Parse([]byte("tools: {ruff: {select: [F, INP001]}}"))
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{t.TempDir(), ".", "symlink", "symlink-parent", "logical-pwd-parent"} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			working := directory
			if directory == "." {
				t.Chdir(root)
				working = "python"
				if err := os.Mkdir(filepath.Join(root, working), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(directory, "symlink") || directory == "logical-pwd-parent" {
				working = filepath.Join(t.TempDir(), "alias")
				target := root
				if directory != "symlink" {
					target = filepath.Join(root, "nested")
					if err := os.Mkdir(target, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, working); err != nil {
					t.Skipf("directory symlink is unavailable: %v", err)
				}
				if directory == "symlink-parent" {
					working += string(filepath.Separator) + ".."
				}
				if directory == "logical-pwd-parent" {
					t.Chdir(target)
					t.Setenv("PWD", working)
					working = ".."
				}
			}
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &command, WorkingDir: working}}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" {
				t.Fatalf("synthetic filename acquired package findings: %+v", result.Diagnostics)
			}
		})
	}
}

func TestRuffAPIIntegrationFlags(t *testing.T) {
	command := testutil.RequireRuff(t)
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: print(missing)
`
	for _, flags := range [][]string{{"--no-cache"}, {"--isolated"}, {"--ignore-noqa"}, {"--no-fix"}, {"--target-version", "py314"}, {"--target-version=py314"}, {"--stdin-filename", "actionlint.py"}, {"--stdin-filename=actionlint.py"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			_, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &command, Arguments: flags}}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source)}})
			if err == nil || !strings.Contains(err.Error(), "integration-owned option") {
				t.Fatalf("integration flag not rejected: %v", err)
			}
		})
	}
}

func TestRuffRepeatableSelectionFlags(t *testing.T) {
	command := testutil.RequireRuff(t)
	config, err := repoconfig.Parse([]byte("tools: {ruff: {select: [F], ignore: [F401]}}"))
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
`
	for _, flags := range [][]string{{"--select", "F"}, {"--ignore", "F401"}, {"--select=F", "--ignore=F401"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &command, Arguments: flags}}).Registrations()}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F821" {
				t.Fatalf("repeatable selection flags changed findings: %+v", result.Diagnostics)
			}
		})
	}
}
