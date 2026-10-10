package actionlint

import (
	"io"
	"path/filepath"
	"testing"
)

func TestRequiredToolsFileSelection(t *testing.T) {
	root := t.TempDir()
	config := writeShellcheckFixture(t, root, "actionlint.yml", "files: {excludes: ['skip.yml']}\noverrides:\n  - includes: [disabled.yml]\n    lint: {rules: {external: {shellcheck: off}}}\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, ConfigFile: config, Shellcheck: "shellcheck"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", false}, {"disabled.yml", false}} {
		got, err := session.RequiredTools([]string{tc.path})
		if err != nil || got.Shellcheck != tc.want {
			t.Fatalf("%s: %+v, %v", tc.path, got, err)
		}
	}
	// Exclusions must apply before trying to read this nonexistent input.
	result, err := session.Files([]string{filepath.Join(root, "skip.yml")}, nil)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("excluded file: %+v, %v", result, err)
	}
}

func TestDeprecatedPyflakesOption(t *testing.T) {
	linter, err := NewLinter(io.Discard, &LinterOptions{Pyflakes: "does-not-exist --invalid"})
	if err != nil {
		t.Fatal(err)
	}
	workflow := []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: python\n        run: print(undefined_name)\n")
	findings, err := linter.Lint("ci.yml", workflow, nil)
	if err != nil || len(findings) != 0 {
		t.Fatalf("deprecated option invoked Python linting: %v, %v", findings, err)
	}
}
