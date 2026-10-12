package actionlint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuffCompositeAliasConfiguration(t *testing.T) {
	command := ruffForTest(t)
	root := t.TempDir()
	writeShellcheckFixture(t, root, "python-action/action.yml", `name: Python
description: test
runs:
  using: composite
  steps:
    - shell: python
      run: |
        import os
        print(missing)
`)
	if err := os.Symlink(filepath.Join(root, "python-action"), filepath.Join(root, "python-alias")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	config, err := ParseConfig([]byte(`overrides:
  - includes: ['python-action/action.yml']
    tools: {ruff: {select: [F401]}}
    lint: {rules: {external: {ruff: warn}}}
  - includes: ['python-alias/action.yml']
    tools: {ruff: {select: [F821]}}
    lint: {rules: {external: {ruff: info}}}
`))
	if err != nil {
		t.Fatal(err)
	}
	source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/python-action
      - uses: $/python-alias
`
	result, err := Analyze(t.Context(), AnalysisRequest{Ruff: command, WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/ci.yml"), Content: []byte(source), Config: config, Project: &Project{root: root}}}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"F401": "warning", "F821": "info"}
	if len(result.Diagnostics) != len(want) {
		t.Fatalf("findings=%+v", result.Diagnostics)
	}
	for _, diagnostic := range result.Diagnostics {
		if severity, ok := want[diagnostic.Code]; !ok || diagnostic.Severity != severity {
			t.Errorf("alias lost its configuration: %+v", diagnostic)
		}
		delete(want, diagnostic.Code)
	}
}

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
		{"workflow only override retains composite defaults", `overrides:
  - includes: ['.github/workflows/**']
    tools: {ruff: false}
`, "", "print(missing)", "F821"},
		{"caller lint disabled", `overrides:
  - includes: ['.github/workflows/**']
    lint: {enabled: false}
`, "", "print(missing)", ""},
		{"caller lint disabled despite composite enabled", "lint: {enabled: false}\n", "lint: {enabled: true, rules: {external: {ruff: on}}}", "print(missing)", ""},
		{"composite warning", "", "lint: {rules: {external: {ruff: warn}}}", "print(missing)", "F821"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".github/workflows/ci.yml")
			writeShellcheckFixture(t, root, ".github/workflows/ci.yml", commandGoodWorkflow)
			writeShellcheckFixture(t, root, "python-action/action.yml", `name: Python
description: test
runs:
  using: composite
  steps:
    - shell: python
      run: |
        `+strings.ReplaceAll(tc.script, "\n", "\n        ")+"\n")
			configText := tc.base
			if tc.override != "" {
				configText += `overrides:
  - includes: ['python-action/action.yml']
    ` + tc.override + "\n"
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
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/python-action
`
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
			if tc.name == "composite warning" && result.Diagnostics[0].Severity != "warning" {
				t.Fatalf("composite severity = %s, want warning", result.Diagnostics[0].Severity)
			}
		})
	}
}
