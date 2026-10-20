package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	toolconfig "actionlint.kjanat.dev/v2/internal/config/tools"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestShellcheckSourcedDiagnostics(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name  string
		line  int
		rc    bool
		alias bool
	}{
		{"configured first line", 1, true, false},
		{"argument beyond run block", 20, false, false},
		{"symlinked checkout", 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.alias {
				alias := filepath.Join(t.TempDir(), "checkout")
				if err := os.Symlink(root, alias); err != nil {
					t.Skipf("directory symlinks unavailable: %v", err)
				}
				root = alias
			}
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			script := strings.Repeat("# library\n", tc.line-1) + "echo $VALUE\n"
			path := writeFixture(t, root, "app/lib/check.sh", script)
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: app
    steps:
      - run: . ./lib/check.sh
      - run: . ./lib/check.sh
`)
			options := application.Options{WorkingDir: root}
			optionsTools := adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}
			if tc.rc {
				rc := writeFixture(t, root, ".shellcheckrc", "shell=bash\n")
				optionsTools.ShellcheckSettings = &toolconfig.ShellcheckSettings{Config: toolconfig.ShellcheckRCFile(rc)}
			}
			session := options
			session.Tools = optionsTools.Registrations()
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("want one sourced finding, got %+v", result.Diagnostics)
			}
			finding := result.Diagnostics[0]
			if want := filepath.Join("app", "lib", "check.sh"); finding.Path != want {
				t.Fatalf("want relative diagnostic path %q, got %q", want, finding.Path)
			}
			assertShellcheckSourceFile(t, filepath.Join(root, finding.Path), path)
			if finding.Start != (diagmodel.Position{Line: tc.line, Column: 6}) || finding.End != (diagmodel.Position{Line: tc.line, Column: 12}) {
				t.Fatalf("wrong sourced location: %+v", finding)
			}
			if finding.Code != "SC2086" || finding.Snippet != "echo $VALUE" || len(finding.Fixes) != 0 {
				t.Fatalf("wrong sourced metadata: %+v", finding)
			}
			want, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			tracked := false
			for _, input := range result.Inputs {
				info, err := os.Stat(input)
				if err == nil && filepath.IsAbs(input) && os.SameFile(info, want) {
					tracked = true
					break
				}
			}
			if !tracked {
				t.Fatalf("sourced diagnostic file missing from inputs: %v", result.Inputs)
			}
			legacy := render.Prepare(*result).Findings[0].TemplateFields()
			if !strings.Contains(legacy.Snippet, "echo $VALUE") {
				t.Fatalf("legacy snippet uses workflow text: %+v", legacy)
			}
		})
	}
}

func TestShellcheckSourcedDiagnosticPathFilters(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, pattern string
		wantPath      string
		alias         bool
	}{
		{"workflow filter keeps library finding", ".github/workflows/**", "scripts/lib.sh", false},
		{"library filter keeps workflow finding", "scripts/**", ".github/workflows/test.yml", false},
		{"combined filter removes both", "**", "", false},
		{"symlinked workflow filter keeps library finding", ".github/workflows/**", "scripts/lib.sh", true},
		{"symlinked library filter keeps workflow finding", "scripts/**", ".github/workflows/test.yml", true},
		{"symlinked combined filter removes both", "**", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.alias {
				alias := filepath.Join(t.TempDir(), "checkout")
				if err := os.Symlink(root, alias); err != nil {
					t.Skipf("directory symlinks unavailable: %v", err)
				}
				root = alias
			}
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, "scripts/lib.sh", "echo $LIBRARY\n")
			writeFixture(t, root, ".github/actionlint.yaml", "paths:\n  '"+tc.pattern+`':
    ignore: ['SC2086']
`)
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo $INLINE
          . ./scripts/lib.sh
`)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantPath == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("want no findings, got %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || filepath.ToSlash(result.Diagnostics[0].Path) != tc.wantPath {
				t.Fatalf("want finding in %s, got %+v", tc.wantPath, result.Diagnostics)
			}
		})
	}
}
