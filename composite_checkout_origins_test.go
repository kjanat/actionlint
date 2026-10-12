package actionlint

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCompositeRetainedCheckoutLocations(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
outputs:
  answer:
    description: test
    value: '42'
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: source/local
      run: |
        . ./lib.sh
        echo $VALUE
`)
	writeShellcheckFixture(t, root, "local/lib.sh", "VALUE=42\n")
	for _, tc := range []struct {
		name, second string
		read         bool
	}{
		{"self sibling", "with: {path: mirror}", true},
		{"foreign sibling", "with: {path: mirror, repository: other/repo}", true},
		{"foreign replacement", "with: {path: source, repository: other/repo}", false},
		{"foreign child", "with: {path: source/local, repository: other/repo}", false},
		{"foreign ancestor", "with: {repository: other/repo}", false},
		{"self ancestor", "with: {path: .}", false},
		{"conditional sibling", "if: inputs.checkout\nwith: {path: mirror}", true},
		{"unknown destination", "with: {path: '${{ inputs.path }}'}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := `- uses: actions/checkout@v6
  with: {path: source}
- uses: actions/checkout@v6
  ` + strings.ReplaceAll(tc.second, "\n", "\n  ") + `
- uses: ./source/local
  id: local
- run: echo '${{ steps.local.outputs.missing }}'`
			result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			if tc.read && !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "expression" && strings.Contains(d.Message, `property "missing" is not defined`)
			}) {
				t.Fatalf("earlier checkout output metadata was lost: %+v", result.Diagnostics)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata }) {
				t.Fatalf("earlier checkout source was not followed: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeUnixColonCheckoutDestination(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, runner := range []string{"ubuntu-latest", "windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/colon.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: 'a:debug'}
      - uses: ./a:debug/local
`)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := runner == "ubuntu-latest" && runtime.GOOS != "windows"
			if slices.Contains(result.Inputs, metadata) != want {
				t.Fatalf("wrong colon checkout metadata selection: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeIndependentSelfRepositoryOrigin(t *testing.T) {
	command := shellcheckForTest(t)
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: ./bad.sh
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: |
        . ./lib.sh
        echo $VALUE
    - shell: bash
      working-directory: ${{ github.workspace }}
      run: |
        . ./lib.sh
        echo $VALUE
`)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	writeShellcheckFixture(t, root, "local/lib.sh", "VALUE=42\n")
	writeShellcheckFixture(t, root, "lib.sh", "VALUE=42\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, tc := range []struct {
		name, before, call string
		executable         bool
	}{
		{"foreign checkout", "- uses: actions/checkout@v6\n  with: {repository: other/repo}", "", true},
		{"unknown checkout", "- uses: actions/checkout@v6\n  with: {path: '${{ inputs.path }}'}", "", true},
		{"opaque execution", `- uses: actions/checkout@v6
  with: {repository: other/repo}
- uses: other/action@v1`, "", false},
		{"startup environment", "- uses: actions/checkout@v6\n  with: {repository: other/repo}", "\n  env: {BASH_ENV: setup.sh}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.before+"\n- uses: $/local"+tc.call+`
- shell: bash
  working-directory: .
  run: ./bad.sh`, AnalysisOptions{Shellcheck: command})
			var shell, executable []Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata {
					shell = append(shell, d)
				}
				if d.Rule == "executable-bit" {
					executable = append(executable, d)
				}
			}
			if len(shell) != 1 || shell[0].Start.Line != 18 || !strings.Contains(shell[0].Message, "SC2086") {
				t.Fatalf("only unknown workspace source should warn: %+v", shell)
			}
			if tc.executable {
				if len(executable) != 1 || filepath.Join(root, executable[0].Path) != metadata || !strings.Contains(executable[0].Message, `"local/bad.sh"`) {
					t.Fatalf("independent action script mode was not checked: %+v", executable)
				}
			} else if len(executable) != 0 {
				t.Fatalf("opaque execution restored mode certainty: %+v", executable)
			}
		})
	}
}

func TestCompositeWindowsCheckoutDestination(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, runner := range []string{"windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/windows.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: 'source\repo'}
      - uses: ./source/repo/local
`)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != (runner == "windows-latest") {
				t.Fatalf("wrong checkout metadata selection: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeSelfRepositoryKeepsWorkspaceUnknown(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: echo ok
`)
	result := compositeAnalysis(t, root, `- uses: actions/checkout@v6
  with: {repository: other/repo}
- uses: $/local
- shell: bash
  working-directory: .
  run: ./bad.sh`, AnalysisOptions{})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("self-repository action restored workspace certainty: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedIndependentActionPath(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: $/inner
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: cd scripts && ./bad.sh
`)
	writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: echo ok
`)
	writeShellcheckFixture(t, root, "inner/placeholder", "")
	metadata := writeShellcheckFixture(t, root, "outer/scripts/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "inner/placeholder", "outer/scripts/bad.sh")
	git("update-index", "--chmod=-x", "outer/scripts/bad.sh")
	result := compositeAnalysis(t, root, `- uses: actions/checkout@v6
  with: {repository: other/repo}
- uses: $/outer`, AnalysisOptions{})
	var executable []Diagnostic
	for _, d := range result.Diagnostics {
		if d.Rule == "executable-bit" {
			executable = append(executable, d)
		}
	}
	if len(executable) != 1 || !strings.Contains(executable[0].Message, `"outer/scripts/bad.sh"`) {
		t.Fatalf("nested action path leaked or relative script was skipped (%s): %+v", metadata, executable)
	}
}

func TestCompositeRetainedCheckoutChanges(t *testing.T) {
	root, git := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: source
      run: ./bad.sh
`)
	git("add", "bad.sh")
	git("update-index", "--chmod=-x", "bad.sh")
	for _, mutation := range []string{"chmod +x source/bad.sh", "unknown-command"} {
		t.Run(mutation, func(t *testing.T) {
			steps := `- uses: actions/checkout@v6
  with: {path: source}
- shell: bash
  working-directory: .
  run: ` + mutation + `
- uses: actions/checkout@v6
  with: {path: mirror}
- uses: actions/checkout@v6
  with: {path: mirror}
- uses: ./source/local`
			result := compositeAnalysis(t, root, steps, AnalysisOptions{})
			// Opaque commands may persist Git settings and redirect later checkouts.
			if slices.Contains(result.Inputs, metadata) != (mutation != "unknown-command") {
				t.Fatalf("wrong earlier checkout certainty after %q: %v", mutation, result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
				t.Fatalf("new checkout restored modified earlier copy: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeIndependentActionChangesSurviveCheckout(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: chmod +x bad.sh
    - uses: actions/checkout@v6
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: ./bad.sh
`)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- uses: $/local", AnalysisOptions{})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("workspace checkout restored independent action mode: %+v", result.Diagnostics)
	}
}

func TestCompositeConditionalModeChanges(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	writeShellcheckFixture(t, root, "probe/action.yml", "name: probe\n")
	git("add", "local/bad.sh", "probe/action.yml")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, condition := range []string{"github.event.repository.name", "true", "false"} {
		for _, independent := range []bool{false, true} {
			for _, checkIndependent := range []bool{false, true} {
				name := condition + "/workspace"
				mutationSpec, mutationDir := "./source/local", "source/local"
				if independent {
					name, mutationSpec, mutationDir = condition+"/independent", "$/local", "${{ github.action_path }}"
				}
				checkSpec, checkDir := "./source/probe", "source/local"
				if checkIndependent {
					name, checkSpec, checkDir = name+"/check-independent", "$/probe", "${{ github.action_path }}/../local"
				} else {
					name += "/check-workspace"
				}
				t.Run(name, func(t *testing.T) {
					writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+mutationDir+`'
      run: chmod +x bad.sh
`)
					metadata := writeShellcheckFixture(t, root, "probe/action.yml", `name: probe
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+checkDir+`'
      run: ./bad.sh
`)
					steps := `- uses: actions/checkout@v6
  with: {path: source}
- uses: ` + mutationSpec + "\n  if: " + condition + `
- uses: actions/checkout@v6
  with: {path: mirror}
- uses: ` + checkSpec
					var executable *RuleExecutableBit
					result := compositeAnalysis(t, root, steps, AnalysisOptions{OnRulesCreated: func(rules []Rule) []Rule {
						for _, rule := range rules {
							if candidate, ok := rule.(*RuleExecutableBit); ok {
								executable = candidate
							}
						}
						return rules
					}})
					if executable == nil || !slices.Contains(result.Inputs, metadata) {
						t.Fatalf("composite execution not inspected: %v", result.Inputs)
					}
					changed := executable.changed
					changedPath := "source/local/bad.sh"
					if independent {
						changed = executable.actionChanged
						changedPath = "local/bad.sh"
					}
					if changed[changedPath] != (condition != "false") {
						t.Errorf("conditional mutation lost: workspace=%v, independent=%v", executable.changed, executable.actionChanged)
					}
					found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata })
					want := condition == "false" || independent != checkIndependent
					if found != want {
						t.Fatalf("finding=%v, want %v: %+v", found, want, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestCompositeConditionalModeCheckoutReset(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      with: {path: source}
    - shell: bash
      run: chmod +x source/scripts/bad.sh
`)
	for _, tc := range []struct {
		condition string
		continued bool
	}{
		{"github.event.repository.name", false},
		{"github.event.repository.name", true},
		{"true", false},
		{"false", false},
	} {
		name, continuation := tc.condition, ""
		if tc.continued {
			name, continuation = name+"/continued", "\n  continue-on-error: true"
		}
		t.Run(name, func(t *testing.T) {
			var executable *RuleExecutableBit
			steps := `- uses: actions/checkout@v6
  with: {path: source}
- shell: bash
  working-directory: .
  run: chmod +x source/bad.sh
- uses: ./source/local
  if: ` + tc.condition + continuation
			compositeAnalysis(t, root, steps, AnalysisOptions{OnRulesCreated: func(rules []Rule) []Rule {
				for _, rule := range rules {
					if candidate, ok := rule.(*RuleExecutableBit); ok {
						executable = candidate
					}
				}
				return rules
			}})
			if executable == nil {
				t.Fatal("executable-bit rule not inspected")
			}
			if executable.changed["source/bad.sh"] != (tc.condition != "true") || executable.changed["source/scripts/bad.sh"] != (tc.condition != "false") {
				t.Fatalf("checkout branch modes not preserved: %v", executable.changed)
			}
		})
	}
}

func TestCompositeRunnerReadonlyAssignments(t *testing.T) {
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	for _, tc := range []struct {
		runner, shell string
		want          bool
	}{
		{"ubuntu-latest", "sh", true},
		{"ubuntu-latest", "bash", false},
		{"macos-latest", "sh", false},
	} {
		t.Run(tc.runner+"/"+tc.shell, func(t *testing.T) {
			metadata := writeShellcheckFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: `+tc.shell+`
      run: UID=0 ./bad.sh
`)
			workflow := writeShellcheckFixture(t, root, ".github/workflows/readonly.yml", `on: push
jobs:
  test:
    runs-on: `+tc.runner+`
    steps:
      - uses: actions/checkout@v6
      - uses: ./outer
`)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested action not inspected: %v", result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata })
			if found != tc.want {
				t.Fatalf("finding=%v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutPrefixCase(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: source/local
      run: |
        . ./lib.sh
        echo $VALUE
`)
	writeShellcheckFixture(t, root, "local/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest", "ubuntu-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: Source}
      - uses: ./source/local
`)
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != (runner == "windows-latest" || runner == "macos-latest") {
				t.Fatalf("wrong checkout metadata selection: %v", result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata }) {
				t.Fatalf("Windows checkout prefix lost source resolution: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeActionSubpathCase(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/Nested/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, runner := range []string{"windows-latest", "macos-latest"} {
		for _, tc := range []struct{ name, checkout, spec string }{
			{"root", "", "./LOCAL/nested"},
			{"placed", "        with: {path: Source}\n", "./source/LOCAL/nested"},
			{"independent", "        with: {path: Source}\n", "$/LOCAL/nested"},
		} {
			t.Run(runner+"/"+tc.name, func(t *testing.T) {
				workflow := writeShellcheckFixture(t, root, ".github/workflows/subpath-case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
`+tc.checkout+"      - uses: "+tc.spec+"\n")
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
				if err != nil {
					t.Fatal(err)
				}
				result, err := session.Files([]string{workflow}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(result.Inputs, metadata) {
					t.Fatalf("canonical metadata not inspected: %v", result.Inputs)
				}
				if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
					return d.Rule == "action" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "shell")
				}) {
					t.Fatalf("missing composite diagnostic: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeActionPathCaseSources(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/Nested/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: |
        . ./lib.sh
        echo $VALUE
    - shell: bash
      run: echo $OTHER
`)
	writeShellcheckFixture(t, root, "local/Nested/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest"} {
		for _, tc := range []struct{ name, checkouts, spec string }{
			{"root", "      - uses: actions/checkout@v6\n", "./LOCAL/nested"},
			{"placed", `      - uses: actions/checkout@v6
        with: {path: Source}
`, "./source/LOCAL/nested"},
			{"retained", `      - uses: actions/checkout@v6
        with: {path: Source}
      - uses: actions/checkout@v6
        with: {path: Mirror}
`, "./source/LOCAL/nested"},
			{"independent", "", "$/LOCAL/nested"},
		} {
			t.Run(runner+"/"+tc.name, func(t *testing.T) {
				workflow := writeShellcheckFixture(t, root, ".github/workflows/source-case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
`+tc.checkouts+"      - uses: "+tc.spec+"\n")
				session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root, Shellcheck: command})
				if err != nil {
					t.Fatal(err)
				}
				result, err := session.Files([]string{workflow}, nil)
				if err != nil {
					t.Fatal(err)
				}
				var findings []Diagnostic
				for _, d := range result.Diagnostics {
					if d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata {
						findings = append(findings, d)
					}
				}
				if len(findings) != 1 || findings[0].Start.Line != 12 || !strings.Contains(findings[0].Message, "SC2086") {
					t.Fatalf("only unsourced variable should warn: %+v", result.Diagnostics)
				}
			})
		}
	}
}

func TestCompositeForeignCheckoutMetadata(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, tc := range []struct {
		name, input, spec, condition string
		read                         bool
	}{
		{"foreign repository", "repository: other/repo", "./local", "", false},
		{"foreign server", "github-server-url: https://git.example.com", "./local", "", false},
		{"unknown repository", "repository: '${{ inputs.repository }}'", "./local", "", true},
		{"conditional foreign repository", "repository: other/repo", "./local", "\n  if: inputs.checkout", true},
		{"independent action", "repository: other/repo", "$/local", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {"+tc.input+"}"+tc.condition+"\n- uses: "+tc.spec, AnalysisOptions{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("wrong foreign metadata selection: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeIndependentModeChangesKeepWorkspaceFinding(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: chmod +x bad.sh
`)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, spec := range []string{"$/local", "./local"} {
		t.Run(spec, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- uses: "+spec+`
- shell: bash
  working-directory: .
  run: ./local/bad.sh`, AnalysisOptions{})
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "executable-bit" && strings.Contains(d.Message, `"local/bad.sh"`)
			})
			if found != strings.HasPrefix(spec, "$/") {
				t.Fatalf("mode changes crossed repository origins: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeWorkspaceModeChangesKeepIndependentFinding(t *testing.T) {
	root, git := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: ./bad.sh
`)
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := compositeAnalysis(t, root, `- uses: actions/checkout@v6
- shell: bash
  working-directory: .
  run: chmod +x local/bad.sh
- uses: $/local`, AnalysisOptions{})
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata }) {
		t.Fatalf("workspace mutation suppressed independent action finding: %+v", result.Diagnostics)
	}
}
