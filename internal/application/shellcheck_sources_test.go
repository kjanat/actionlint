package application_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	shellprofile "actionlint.kjanat.dev/v2/internal/tools/profile/shell"
)

func TestNativeSourcedRereadRecordsReportedSymlinkAlias(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	physical := writeFixture(t, root, "physical/lib.sh", "echo $VALUE\n")
	alias := filepath.Join(root, "reported.sh")
	if err := os.Symlink(physical, alias); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, errors.ErrUnsupported) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	encodedPath, err := json.Marshal(alias)
	if err != nil {
		t.Fatal(err)
	}
	workflow := writeFixture(t, root, "ci.yml", "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: '. ./reported.sh'}]}}\n")
	options := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellcheckRunner: func([]string, string) ([]byte, error) {
		return fmt.Appendf(nil, `{"comments":[{"file":%s,"line":1,"column":6,"level":"info","code":2086,"message":"reported source"}]}`, encodedPath), nil
	}}).Registrations()}
	result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Inputs, alias) || slices.Contains(result.Inputs, physical) {
		t.Fatalf("native display reread replaced reported alias with physical path: %v", result.Inputs)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Reference != nil || !render.Prepare(*result).Findings[0].Unavailable {
		t.Fatalf("native unavailable report did not reach rendering: %+v", result)
	}
}

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
				optionsTools.ShellcheckSettings = &shellprofile.Settings{Config: shellprofile.RCFile(rc)}
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
			encoded, err := json.Marshal(finding)
			if err != nil {
				t.Fatal(err)
			}
			if finding.Reference != nil || strings.Contains(string(encoded), `"reference"`) || strings.Contains(string(encoded), `"revision"`) || strings.Contains(string(encoded), `"fixes"`) {
				t.Fatalf("native sourced diagnostic fabricated known evidence: %s", encoded)
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

func TestUnavailableSourcedDiagnosticKeepsYAMLSuppressionAnchor(t *testing.T) {
	command := shellcheckForTest(t)
	for _, suppressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "reported", true: "suppressed"}[suppressed], func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeFixture(t, root, "scripts/lib.sh", "echo $VALUE\n")
			comment := ""
			if suppressed {
				comment = " # actionlint:ignore shellcheck -- reviewed source"
			}
			workflow := writeFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: . ./scripts/lib.sh"+comment+"\n")
			options := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command, ShellcheckOptions: &processconfig.Options{Arguments: []string{"--check-sourced"}}}).Registrations()}
			result, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if suppressed {
				want = 0
			}
			if len(result.Diagnostics) != want {
				t.Fatalf("YAML anchor suppression changed: %+v", result.Diagnostics)
			}
			for _, finding := range result.Diagnostics {
				if finding.Reference != nil || len(finding.Fixes) != 0 || filepath.ToSlash(finding.Path) != "scripts/lib.sh" {
					t.Fatalf("unavailable source gained known evidence: %+v", finding)
				}
			}
		})
	}
}
