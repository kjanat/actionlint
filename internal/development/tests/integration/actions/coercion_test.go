package actions_test

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestActionCoercionSelection(t *testing.T) {
	t.Parallel()
	const manifest = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - if: ${{ startsWith(true, 'TR') }}%s\n      run: echo ok\n      shell: bash\n"
	for _, called := range []bool{false, true} {
		for _, tc := range []struct {
			name, config, comment, severity string
			strict                          bool
		}{
			{"default", "", "", "", false},
			{"on", "lint: {rules: {suspicious: {coercion: on}}}", "", "warning", false},
			{"warn", "lint: {rules: {suspicious: {coercion: warn}}}", "", "warning", false},
			{"error", "lint: {rules: {suspicious: {coercion: error}}}", "", "error", false},
			{"off", "lint: {rules: {suspicious: {coercion: off}}}", "", "", false},
			{"strict", "lint: {rules: {suspicious: {coercion: on}}}", "", "warning", true},
			{"strict default", "", "", "error", true},
			{"strict off", "lint: {rules: {suspicious: {coercion: off}}}", "", "", true},
			{"inline", "lint: {rules: {suspicious: {coercion: on}}}", " # ignore[coercion]", "", false},
			{"legacy inline", "lint: {rules: {suspicious: {coercion: on}}}", " # ignore[suspicious-coercion]", "", false},
			{"override", "lint: {rules: {suspicious: {coercion: on}}}\noverrides: [{includes: ['**/action.yml', 'action.yml'], lint: {rules: {suspicious: {coercion: off}}}}]", "", "", false},
		} {
			t.Run(tc.name+"/called="+map[bool]string{false: "false", true: "true"}[called], func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				if called {
					root, _ = testutil.ExecutableFixture(t, "shellcheck", "ruff")
				}
				source := strings.Replace(manifest, "%s", tc.comment, 1)
				cfg, err := checking.ConfigurationRules().Parse([]byte(tc.config))
				if err != nil {
					t.Fatal(err)
				}
				var result *checking.Result
				options := checking.Options{WorkingDir: root, Presets: repoconfig.Presets{Strict: tc.strict}}
				if called {
					fixturefiles.Write(t, root, ".github/actionlint.yaml", tc.config+"\ntools: {shellcheck: false, ruff: false}\n")
					fixturefiles.Write(t, root, "inner/action.yml", source)
					result = testutil.Composite(t, root, "- uses: ./inner", options)
				} else {
					result, err = checking.Analyze(t.Context(), options, []workspace.Source{{Path: "action.yml", Content: []byte(source), Config: cfg}})
					if err != nil {
						t.Fatal(err)
					}
				}
				count := 0
				for _, finding := range result.Diagnostics {
					if finding.Rule != "coercion" {
						continue
					}
					count++
					if finding.Severity != tc.severity || finding.Start.Line != 6 || !strings.HasSuffix(filepath.ToSlash(finding.Path), "action.yml") || finding.End.Column <= finding.Start.Column {
						t.Fatalf("unexpected finding: %+v", finding)
					}
					line := strings.Split(source, "\n")[finding.Start.Line-1]
					if line[finding.Start.Column-1:finding.End.Column-1] != "true" {
						t.Fatalf("wrong source span: %+v", finding)
					}
				}
				want := 0
				if tc.severity != "" {
					want = 1
				}
				if count != want {
					t.Fatalf("got %d findings, want %d: %+v", count, want, result.Diagnostics)
				}
			})
		}
	}
}
