package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			path := writeShellcheckFixture(t, root, "app/lib/check.sh", script)
			workflow := writeShellcheckFixture(t, root, ".github/workflows/test.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        working-directory: app\n    steps:\n      - run: . ./lib/check.sh\n      - run: . ./lib/check.sh\n")
			options := AnalysisOptions{WorkingDir: root, Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"--check-sourced"}}}
			if tc.rc {
				rc := writeShellcheckFixture(t, root, ".shellcheckrc", "shell=bash\n")
				options.ShellcheckSettings = &ShellcheckSettings{Config: ShellcheckRCFile(rc)}
			}
			session, err := NewAnalysisSession(options)
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
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
			if finding.Start != (DiagnosticPosition{tc.line, 6}) || finding.End != (DiagnosticPosition{tc.line, 12}) {
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
			legacy := result.legacyErrors()[0].GetTemplateFields([]byte("workflow text"))
			if !strings.Contains(legacy.Snippet, "echo $VALUE") {
				t.Fatalf("legacy snippet uses workflow text: %+v", legacy)
			}
		})
	}
}

func TestCompositeShellcheckSourcedDiagnostics(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	path := writeShellcheckFixture(t, root, "app/lib/check.sh", "echo $VALUE\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: app\n      run: . ./lib/check.sh\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{
		Shellcheck: command, ShellcheckOptions: &ExternalCommandOptions{Arguments: []string{"-a"}},
	})
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want one sourced finding: %+v", result.Diagnostics)
	}
	finding := result.Diagnostics[0]
	if want := filepath.Join("app", "lib", "check.sh"); finding.Path != want {
		t.Fatalf("want relative diagnostic path %q, got %q", want, finding.Path)
	}
	assertShellcheckSourceFile(t, filepath.Join(root, finding.Path), path)
	if finding.Start != (DiagnosticPosition{1, 6}) || finding.Snippet != "echo $VALUE" {
		t.Fatalf("sourced finding attributed to composite: %+v", finding)
	}
}

func TestShellcheckUnavailableSourcedDiagnostic(t *testing.T) {
	rule := newRuleShellcheck(&externalCommand{})
	finding := rule.sourcedDiagnostic(shellcheckError{File: "missing.sh", Line: 1, Column: 1, Code: 2086, Level: "info", Message: "finding"}, t.TempDir(), make(map[string][]byte))
	if got := finding.GetTemplateFields([]byte("workflow text")); got.Snippet != "" || !strings.HasSuffix(got.Filepath, "missing.sh") {
		t.Fatalf("missing source borrowed workflow content: %+v", got)
	}
}

func TestCompositeShellcheckSiblingWorkingDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, filepath.Dir(root), "shared/value.sh", "VALUE=42\n")
	writeShellcheckFixture(t, root, "local/action.yml", "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ../shared\n      run: |\n        . ./value.sh\n        echo $VALUE\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
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
