package actions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

func TestScriptRuntimeStandaloneAndCalledComposite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, shell, script string
		want                int
	}{
		{"errexit-status", "bash", `CHANGED="$(git diff --exit-code)"; EXIT_CODE=$?`, 1},
		{"errexit-status", "bash", `CHANGED="$(git diff --exit-code)" || EXIT_CODE=$?`, 0},
		{"errexit-status", "bash +e {0}", `false; status=$?`, 0},
		{"workflow-command-syntax", "bash", `echo "::notice:Up to date"`, 1},
		{"workflow-command-syntax", "python", `print("::error:msg")`, 1},
		{"workflow-command-syntax", "pwsh", `Write-Host '::warning single-colon'`, 1},
		{"workflow-command-syntax", "pwsh", `[System.IO.Path]::GetTempPath()`, 0},
	} {
		for _, called := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/%d/called=%v", tc.name, tc.shell, tc.want, called), func(t *testing.T) {
				t.Parallel()
				root, _ := testutil.ExecutableFixture(t, "shellcheck", "ruff")
				configPath := fixturefiles.Write(t, root, ".github/actionlint.yaml", "lint: {rules: {preset: none, suspicious: {"+tc.name+": on}}}\n")
				action := fmt.Sprintf("name: Script regression\ndescription: Check runner script behavior\nruns:\n  using: composite\n  steps:\n    - shell: %s\n      run: |\n        %s\n", tc.shell, tc.script)
				var result *checking.Result
				if called {
					fixturefiles.Write(t, root, "inner/action.yml", action)
					result = testutil.Composite(t, root, "- uses: ./inner", checking.Options{ConfigFile: configPath})
				} else {
					var err error
					config, parseErr := checking.ConfigurationRules().Parse([]byte("lint: {rules: {preset: none, suspicious: {" + tc.name + ": on}}}"))
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					result, err = checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: "action.yml", Content: []byte(action), Config: config}})
					if err != nil {
						t.Fatal(err)
					}
				}
				var found []results.Diagnostic
				for _, d := range result.Diagnostics {
					if d.Rule == tc.name {
						found = append(found, d)
					}
				}
				if len(found) != tc.want {
					t.Fatalf("got %+v, want %d (all %+v)", found, tc.want, result.Diagnostics)
				}
				for _, d := range found {
					if d.Start.Line != 8 || !strings.HasSuffix(filepath.ToSlash(d.Path), "action.yml") || d.Severity != "warning" {
						t.Fatalf("wrong declaring source or severity: %+v", d)
					}
				}
			})
		}
	}
}

func TestScriptRuntimePublicGPGAction(t *testing.T) {
	t.Parallel()
	// These exact public files were fetched without executing their commands.
	// The first revision contains the failures described in PR343 comment
	// 6713776814. The second is the fixed revision cited in that comment.
	for _, tc := range []struct {
		file string
		want int
	}{
		{"gpg-881bd5cd.action.yml", 2},
		{"gpg-ac6e230b.action.yml", 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Join("internal", "development", "tests", "integration", "actions", "testdata", "script-runtime", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			config, err := checking.ConfigurationRules().Parse([]byte("lint: {rules: {preset: none, suspicious: {errexit-status: on, workflow-command-syntax: on}}}"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: t.TempDir()}, []workspace.Source{{Path: "action.yml", Content: content, Config: config}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != tc.want {
				t.Fatalf("got %+v, want %d", result.Diagnostics, tc.want)
			}
			if tc.want != 0 {
				seen := map[string]int{}
				for _, d := range result.Diagnostics {
					seen[d.Rule] = d.Start.Line
				}
				if seen["errexit-status"] != 16 || seen["workflow-command-syntax"] != 26 {
					t.Fatalf("wrong public source ranges: %+v", seen)
				}
			}
		})
	}
}
