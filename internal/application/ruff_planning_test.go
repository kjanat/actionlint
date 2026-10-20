package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/processconfig"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestRuffProjectCatchAllOverrides(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, overrides string
		enabled         bool
	}{
		{"all tools disabled", "[{includes: ['**'], tools: {ruff: false}}]", false},
		{"all rules disabled", "[{includes: ['**'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"relative catch-all tools disabled", "[{includes: ['./**'], tools: {ruff: false}}]", false},
		{"file catch-all tools disabled", "[{includes: ['**/*'], tools: {ruff: false}}]", false},
		{"relative file catch-all rules disabled", "[{includes: ['./**/*'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"caller lint disabled", "[{includes: ['.github/workflows/**'], lint: {enabled: false}}]", false},
		{"caller lint disabled despite composite enabled", "[{includes: ['.github/workflows/**'], lint: {enabled: false}}, {includes: ['python-action/**'], lint: {enabled: true}}]", false},
		{"later tool reenabled", "[{includes: ['**'], tools: {ruff: false}}, {includes: ['python-action/action.yml'], tools: {ruff: true}}]", true},
		{"later rule reenabled", "[{includes: ['**'], lint: {rules: {external: {ruff: off}}}}, {includes: ['python-action/action.yml'], lint: {rules: {external: {ruff: on}}}}]", true},
		{"later catch-all disables tool", "[{includes: ['python-action/action.yml'], tools: {ruff: true}}, {includes: ['**'], tools: {ruff: false}}]", false},
		{"later catch-all disables rule", "[{includes: ['python-action/action.yml'], lint: {rules: {external: {ruff: on}}}}, {includes: ['**'], lint: {rules: {external: {ruff: off}}}}]", false},
		{"workflow-only disabled", "[{includes: ['.github/workflows/**'], tools: {ruff: false}}]", true},
		{"catch-all has exception", "[{includes: ['**'], excludes: ['python-action/**'], tools: {ruff: false}}]", true},
		{"catch-all has negative include", "[{includes: ['**', '!python-action/**'], tools: {ruff: false}}]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, root, ".git", "")
			workflow := writeFixture(t, root, ".github/workflows/ci.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/python-action
`)
			writeFixture(t, root, "python-action/action.yml", `name: Python
description: test
runs:
  using: composite
  steps:
    - shell: python
      run: print(missing)
`)
			writeFixture(t, root, ".github/actionlint.yaml", "overrides: "+tc.overrides+"\n")
			command := executable
			if !tc.enabled {
				command = filepath.Join(root, "missing-ruff")
			}
			options := application.Options{WorkingDir: root, Tools: (adapters.Options{RuffOptions: &processconfig.Options{Executable: &command, LauncherArguments: []string{"-test.run=^TestRuffPlanningHelper$", "--"}, Environment: []string{"ACTIONLINT_RUFF_PLANNING_HELPER=1"}}}).Registrations()}
			needed, err := application.RequiredTools(t.Context(), options, []string{workflow})
			if err != nil || needed["ruff"] != tc.enabled {
				t.Fatalf("needed=%+v, error=%v", needed, err)
			}
			if _, err := application.Check(t.Context(), options, application.Input{Kind: application.Files, Paths: []string{workflow}}); err != nil {
				t.Fatalf("checker initialization: %v", err)
			}
		})
	}
}

func TestRuffRequiredToolsCompositeOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, base, override string
		want                 bool
	}{
		{"tool reenabled", "tools: {ruff: false}", "tools: {ruff: true}", true},
		{"tool reset", "tools: {ruff: false}", "tools: null", true},
		{"rule reenabled", "lint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: on}}}", true},
		{"tool remains disabled", "tools: {ruff: false}", "tools: {ruff: {select: [F821]}}", false},
		{"tool explicitly disabled", "tools: {ruff: false}", "tools: {ruff: false}", false},
		{"rule remains disabled", "lint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: off}}}", false},
		{"rule blocked by tool", "tools: {ruff: false}\nlint: {rules: {external: {ruff: off}}}", "lint: {rules: {external: {ruff: on}}}", false},
		{"split tool and rule", "tools: {ruff: true}\nlint: {rules: {external: {ruff: off}}}", "tools: {ruff: false}\n    lint: {rules: {external: {ruff: on}}}", false},
		{"split rule and tool", "tools: {ruff: false}\nlint: {rules: {external: {ruff: on}}}", "tools: {ruff: true}\n    lint: {rules: {external: {ruff: off}}}", false},
		{"both reenabled", "tools: {ruff: false}\nlint: {rules: {external: {ruff: off}}}", "tools: {ruff: true}\n    lint: {rules: {external: {ruff: on}}}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeFixture(t, root, "actionlint.yml", tc.base+`
overrides:
  - includes: [action.yml]
    `+tc.override+"\n")
			for _, executable := range []string{"ruff", ""} {
				options := application.Options{WorkingDir: root, ConfigFile: path, Tools: (adapters.Options{Ruff: executable}).Registrations()}
				tools, err := application.RequiredTools(t.Context(), options, []string{"ci.yml"})
				if err != nil || tools["ruff"] != (tc.want && executable != "") {
					t.Fatalf("tools=%+v, error=%v", tools, err)
				}
			}
		})
	}
}

func TestRuffRequiredToolsManyOverrides(t *testing.T) {
	unrelated := strings.Repeat(`  - includes: ['**/action.yml']
    lint: {rules: {correctness: {expression: warn}}}
`, 8)
	for _, tc := range []struct {
		name, base, final string
		want              bool
	}{
		{"disabled remains disabled", "tools: {ruff: false}\n", "", false},
		{"universal tool disable", "", `  - includes: ['**']
    tools: {ruff: false}
`, false},
		{"universal rule disable", "", `  - includes: ['**']
    lint: {rules: {external: {ruff: off}}}
`, false},
		{"possible enable", "tools: {ruff: false}\n", `  - includes: ['python-action/**']
    tools: {ruff: true}
`, true},
		{"overlapping separate enables", `tools: {ruff: false}
lint: {rules: {external: {ruff: off}}}
`, `  - includes: ['python-action/**']
    tools: {ruff: true}
  - includes: ['**/action.yml']
    lint: {rules: {external: {ruff: on}}}
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := writeFixture(t, root, "actionlint.yml", tc.base+"overrides:\n"+unrelated+tc.final)
			options := application.Options{WorkingDir: root, ConfigFile: config, Tools: (adapters.Options{Ruff: "ruff"}).Registrations()}
			needed, err := application.RequiredTools(t.Context(), options, []string{"ci.yml"})
			if err != nil || needed["ruff"] != tc.want {
				t.Fatalf("needed=%+v, error=%v, want=%v", needed, err, tc.want)
			}
		})
	}
}

func TestRuffRequiredToolsOverlappingOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         bool
	}{
		{"separate gates", `tools: {ruff: false}
lint: {rules: {external: {ruff: off}}}
overrides:
  - includes: ['python-action/**']
    tools: {ruff: true}
  - includes: ['**/action.yml']
    lint: {rules: {external: {ruff: on}}}
`, true},
		{"explicit rule survives group", `lint: {rules: {external: off}}
overrides:
  - includes: ['python-action/**']
    lint: {rules: {external: {ruff: on}}}
  - includes: ['**']
    lint: {rules: {external: {level: off}}}
`, true},
		{"explicit off survives group", `lint: {rules: {external: {ruff: off}}}
overrides:
  - includes: ['python-action/**']
    lint: {rules: {external: {level: on}}}
`, false},
		{"default restores group", `lint: {rules: {external: {ruff: off}}}
overrides:
  - includes: ['python-action/**']
    lint: {rules: {external: {level: on}}}
  - includes: ['**/action.yml']
    lint: {rules: {external: {ruff: default}}}
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := writeFixture(t, root, "actionlint.yml", tc.config)
			options := application.Options{WorkingDir: root, ConfigFile: config, Tools: (adapters.Options{Ruff: "ruff"}).Registrations()}
			needed, err := application.RequiredTools(t.Context(), options, []string{"ci.yml"})
			if err != nil || needed["ruff"] != tc.want {
				t.Fatalf("needed=%+v, error=%v, want=%v", needed, err, tc.want)
			}
		})
	}
}

func TestRuffRequiredToolsOverrides(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "actionlint.yml", `tools: {shellcheck: false}
files: {excludes: [skip.yml]}
overrides:
  - includes: [disabled.yml]
    lint: {rules: {external: {ruff: off}}}
`)
	options := application.Options{WorkingDir: root, ConfigFile: path, Tools: (adapters.Options{Ruff: "ruff", ShellCheck: "shellcheck"}).Registrations()}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"skip.yml", false}, {"disabled.yml", false}} {
		tools, err := application.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || tools["ruff"] != tc.want || tools["shellcheck"] {
			t.Fatalf("%s: %+v %v", tc.path, tools, err)
		}
	}
}

func TestRuffRequiredToolsProjectCompositeBaseline(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		root := t.TempDir()
		writeFixture(t, root, ".git", "")
		workflow := writeFixture(t, root, ".github/workflows/ci.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/python-action
`)
		writeFixture(t, root, "python-action/action.yml", `name: Python
description: test
runs:
  using: composite
  steps:
    - shell: python
      run: print(missing)
`)
		base := ""
		if !enabled {
			base = "tools: {ruff: false}\n"
		}
		path := writeFixture(t, root, "actionlint.yml", base+`overrides:
  - includes: ['.github/workflows/**']
    tools: {ruff: false}
`)
		options := application.Options{WorkingDir: root, ConfigFile: path, Tools: (adapters.Options{Ruff: "ruff"}).Registrations()}
		tools, err := application.RequiredTools(t.Context(), options, []string{workflow})
		if err != nil || tools["ruff"] != enabled {
			t.Fatalf("enabled=%v: tools=%+v error=%v", enabled, tools, err)
		}
	}
}
