package actionlint

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCompositeCheckoutModeIsolation(t *testing.T) {
	root, _ := executableFixture(t)
	for _, tc := range []struct {
		name, steps string
		finding     bool
	}{
		{"fresh sibling", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: chmod +x source/bad.sh\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- run: ./mirror/bad.sh", true},
		{"refresh root", "- uses: actions/checkout@v6\n- run: chmod +x bad.sh\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- uses: actions/checkout@v6\n- run: ./bad.sh", true},
		{"refresh sibling", "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- run: chmod +x mirror/bad.sh\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- run: ./mirror/bad.sh", true},
		{"retain modified sibling", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: chmod +x source/bad.sh\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- run: ./source/bad.sh", false},
		{"same directory spelling", "- uses: actions/checkout@v6\n  with: {path: source}\n- run: chmod +x source/scripts/../bad.sh\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- run: ./source/bad.sh", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := strings.ReplaceAll(tc.steps, "- run:", "- shell: bash\n  working-directory: .\n  run:")
			result := compositeAnalysis(t, root, steps, AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "executable-bit" && strings.Contains(d.Message, `"bad.sh"`)
			})
			if found != tc.finding {
				t.Fatalf("finding=%v, want %v: %+v", found, tc.finding, result.Diagnostics)
			}
		})
	}
}

func TestCompositeConditionalSelfRecheckoutPlacement(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	for _, tc := range []struct {
		name, condition, extra, inputs string
		read                           bool
	}{
		{"conditional self", "inputs.refresh", "", "path: source", true},
		{"disabled self", "false", "", "path: source", true},
		{"tolerated failure", "inputs.refresh", "\n  continue-on-error: true", "path: source", false},
		{"background", "inputs.refresh", "\n  background: true", "path: source", false},
		{"foreign repository", "inputs.refresh", "", "path: source, repository: other/repo", false},
		{"different ref", "inputs.refresh", "", "path: source, ref: other", false},
		{"after failure", "always()", "", "path: source", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: actions/checkout@v6\n  if: " + tc.condition + tc.extra + "\n  with: {" + tc.inputs + "}\n- uses: ./source/local"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "action" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "shell")
			})
			if found != tc.read {
				t.Fatalf("composite checked=%v, want %v: %+v", found, tc.read, result.Diagnostics)
			}
		})
	}
}
