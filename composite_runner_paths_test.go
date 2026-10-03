package actionlint

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCompositeContextDirectorySeparators(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	for _, directory := range []string{"scripts", "local/scripts"} {
		writeShellcheckFixture(t, root, directory+"/lib.sh", "VALUE=42\n")
	}
	if runtime.GOOS != "windows" {
		for _, directory := range []string{`literal\scripts`, `local/literal\scripts`} {
			writeShellcheckFixture(t, root, directory+"/lib.sh", "VALUE=42\n")
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
				metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: '${{ "+context+" }}"+tc.suffix+"'\n      run: |\n        . ./lib.sh\n        echo $VALUE\n")
				workflow := writeShellcheckFixture(t, root, ".github/workflows/context.yml", "on: push\njobs:\n  test:\n    runs-on: "+tc.runner+"\n    steps:\n      - uses: actions/checkout@v6\n      - uses: $/local\n")
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
				if err != nil {
					t.Fatal(err)
				}
				result, err := session.Files([]string{workflow}, nil)
				if err != nil {
					t.Fatal(err)
				}
				warned := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
					return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "SC2086")
				})
				if warned == tc.known {
					t.Fatalf("context directory resolution known=%v: %+v", tc.known, result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeExecutableUnixPaths(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, tc := range []struct {
		name, checkout, directory, script string
	}{
		{"colon checkout", "a:debug", "${{ github.action_path }}", "./bad.sh"},
		{"colon directory", "a:debug", "a:debug/local", "./bad.sh"},
		{"colon cd", "a:debug", ".", "cd a:debug/local && ./bad.sh"},
		{"colon direct operand", "a:debug", ".", "./a:debug/local/bad.sh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: '"+tc.directory+"'\n      run: "+tc.script+"\n")
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {path: '"+tc.checkout+"'}\n- uses: ./"+tc.checkout+"/local", AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata })
			if found != (runtime.GOOS != "windows") {
				t.Fatalf("wrong Unix checkout script result: %+v", result.Diagnostics)
			}
		})
	}
}
