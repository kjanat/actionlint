package actionlint

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRuffCompositeFileConfiguration(t *testing.T) {
	command := ruffForTest(t)
	for _, tc := range []struct {
		name, base, override, script, code string
	}{
		{"disabled tool", "", "tools: {ruff: false}", "print(missing)", ""},
		{"disabled rule", "", "lint: {rules: {external: {ruff: off}}}", "print(missing)", ""},
		{"selected code", "", "tools: {ruff: {select: [F401]}}", "import os\nprint(missing)", "F401"},
		{"ignored code", "", "tools: {ruff: {ignore: [F821]}}", "print(missing)", ""},
		{"empty selection", "", "tools: {ruff: {select: []}}", "print(missing)", ""},
		{"target version", "", "tools: {ruff: {target-version: py39}}", "match 1:\n  case 1:\n    pass", "invalid-syntax"},
		{"workflow disabled composite enabled", "tools: {ruff: false}\n", "tools: {ruff: true}", "print(missing)", "F821"},
		{"workflow rule disabled composite enabled", "lint: {rules: {external: {ruff: off}}}\n", "lint: {rules: {external: {ruff: on}}}", "print(missing)", "F821"},
		{"workflow only override retains composite defaults", "overrides:\n  - includes: ['.github/workflows/**']\n    tools: {ruff: false}\n", "", "print(missing)", "F821"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".github/workflows/ci.yml")
			writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
			writeShellcheckFixture(t, root, "python-action/action.yml", "name: Python\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: python\n      run: |\n        "+strings.ReplaceAll(tc.script, "\n", "\n        ")+"\n")
			configText := tc.base
			if tc.override != "" {
				configText += "overrides:\n  - includes: ['python-action/action.yml']\n    " + tc.override + "\n"
			}
			config, err := ParseConfig([]byte(configText))
			if err != nil {
				t.Fatal(err)
			}
			if tc.code != "" {
				if enabled, err := ruffConfigMayEnable((RulePresets{}).apply(config)); err != nil || !enabled {
					t.Fatalf("composite checker cannot be provisioned: enabled=%v, error=%v", enabled, err)
				}
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/python-action\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: root, Sources: []SourceUnit{{Path: path, Content: []byte(source), Config: config, Project: &Project{root: root}}}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if len(result.Diagnostics) != 0 {
					t.Fatalf("unexpected findings: %+v", result.Diagnostics)
				}
				return
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != tc.code {
				t.Fatalf("findings = %+v, want %s", result.Diagnostics, tc.code)
			}
		})
	}
}
