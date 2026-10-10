package actionlint

import (
	"path/filepath"
	"testing"
)

func TestCleanCompositeSuppressionDirectives(t *testing.T) {
	for _, tc := range []struct {
		name, directive, config, want string
	}{
		{"malformed", "# actionlint:ignore shellcheck", "", "inline-suppression"},
		{"prohibited", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\n", "disallow-suppressions"},
		{"allowed", "# actionlint:ignore shellcheck -- reviewed", "", ""},
		{"override", "# actionlint:ignore shellcheck -- reviewed", "policy: {disallow-suppressions: true}\noverrides:\n  - includes: ['inner/action.yml']\n    lint: {rules: {policy: {disallow-suppressions: off}}}\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", tc.config)
			writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: ./inner\n")
			metadata := writeShellcheckFixture(t, root, "inner/action.yml", "name: inner "+tc.directive+"\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n")
			result := compositeAnalysis(t, root, "- uses: ./outer\n- uses: ./outer", AnalysisOptions{})
			if tc.want == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatal(result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != tc.want || result.Diagnostics[0].Start.Line != 1 || filepath.Join(root, result.Diagnostics[0].Path) != metadata {
				t.Fatalf("want one %s in inner/action.yml:1: %+v", tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeBlockSuppression(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: | # actionlint:ignore shellcheck -- reviewed expansion\n        echo $VALUE\n")
	result := compositeAnalysis(t, root, "- uses: ./local", AnalysisOptions{Shellcheck: command})
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
}
