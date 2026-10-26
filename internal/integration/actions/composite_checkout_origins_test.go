package actions_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestCompositeCheckoutLocations(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
	testutil.WriteFixture(t, root, "local/lib.sh", "VALUE=42\n")
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
			result := testutil.Composite(t, root, steps, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			if tc.read && !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "expression-reference" && strings.Contains(d.Message, `property "missing" is not defined`)
			}) {
				t.Fatalf("earlier checkout output metadata was lost: %+v", result.Diagnostics)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata
			}) {
				t.Fatalf("earlier checkout source was not followed: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeIndependentSelfRepositoryOrigin(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, git := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
	testutil.WriteFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	testutil.WriteFixture(t, root, "local/lib.sh", "VALUE=42\n")
	testutil.WriteFixture(t, root, "lib.sh", "VALUE=42\n")
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
			result := testutil.Composite(t, root, tc.before+"\n- uses: $/local"+tc.call+`
- shell: bash
  working-directory: .
  run: ./bad.sh`, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			var shell, executable []diagmodel.Diagnostic
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

func TestCompositeSelfRepositoryKeepsWorkspaceUnknown(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: echo ok
`)
	result := testutil.Composite(t, root, `- uses: actions/checkout@v6
  with: {repository: other/repo}
- uses: $/local
- shell: bash
  working-directory: .
  run: ./bad.sh`, application.Options{})
	if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("self-repository action restored workspace certainty: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedIndependentActionPath(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: $/inner
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: cd scripts && ./bad.sh
`)
	testutil.WriteFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: echo ok
`)
	testutil.WriteFixture(t, root, "inner/placeholder", "")
	metadata := testutil.WriteFixture(t, root, "outer/scripts/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "inner/placeholder", "outer/scripts/bad.sh")
	git("update-index", "--chmod=-x", "outer/scripts/bad.sh")
	result := testutil.Composite(t, root, `- uses: actions/checkout@v6
  with: {repository: other/repo}
- uses: $/outer`, application.Options{})
	var executable []diagmodel.Diagnostic
	for _, d := range result.Diagnostics {
		if d.Rule == "executable-bit" {
			executable = append(executable, d)
		}
	}
	if len(executable) != 1 || !strings.Contains(executable[0].Message, `"outer/scripts/bad.sh"`) {
		t.Fatalf("nested action path leaked or relative script was skipped (%s): %+v", metadata, executable)
	}
}

func TestCompositeCheckoutChanges(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
			result := testutil.Composite(t, root, steps, application.Options{
				// Opaque commands may persist Git settings and redirect later checkouts.
			})

			if slices.Contains(result.Inputs, metadata) != (mutation != "unknown-command") {
				t.Fatalf("wrong earlier checkout certainty after %q: %v", mutation, result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" }) {
				t.Fatalf("new checkout restored modified earlier copy: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeIndependentActionChangesSurviveCheckout(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
	testutil.WriteFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n- uses: $/local", application.Options{})
	if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("workspace checkout restored independent action mode: %+v", result.Diagnostics)
	}
}

func TestCompositeConditionalModeChanges(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	testutil.WriteFixture(t, root, "probe/action.yml", "name: probe\n")
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
					testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+mutationDir+`'
      run: chmod +x bad.sh
`)
					metadata := testutil.WriteFixture(t, root, "probe/action.yml", `name: probe
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
					result := testutil.Composite(t, root, steps, application.Options{})
					if !slices.Contains(result.Inputs, metadata) {
						t.Fatalf("composite execution not inspected: %v", result.Inputs)
					}

					found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
						return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
					})
					want := condition == "false" || independent != checkIndependent
					if found != want {
						t.Fatalf("finding=%v, want %v: %+v", found, want, result.Diagnostics)
					}
				})
			}
		}
	}
}

func TestCompositeForeignCheckoutMetadata(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
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
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: {"+tc.input+"}"+tc.condition+"\n- uses: "+tc.spec, application.Options{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("wrong foreign metadata selection: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeIndependentModeChangesKeepWorkspaceFinding(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: chmod +x bad.sh
`)
	testutil.WriteFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, spec := range []string{"$/local", "./local"} {
		t.Run(spec, func(t *testing.T) {
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n- uses: "+spec+`
- shell: bash
  working-directory: .
  run: ./local/bad.sh`, application.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && strings.Contains(d.Message, `"local/bad.sh"`)
			})
			if found != strings.HasPrefix(spec, "$/") {
				t.Fatalf("mode changes crossed repository origins: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeWorkspaceModeChangesKeepIndependentFinding(t *testing.T) {
	root, git := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: ./bad.sh
`)
	testutil.WriteFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := testutil.Composite(t, root, `- uses: actions/checkout@v6
- shell: bash
  working-directory: .
  run: chmod +x local/bad.sh
- uses: $/local`, application.Options{})
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
	}) {
		t.Fatalf("workspace mutation suppressed independent action finding: %+v", result.Diagnostics)
	}
}
