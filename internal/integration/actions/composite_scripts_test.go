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

func TestCompositeShellcheck(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	for _, filename := range []string{"action.yml", "action.yaml"} {
		t.Run(filename, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
			metadata := testutil.WriteFixture(t, root, ".github/actions/lint/"+filename, `name: lint
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        echo $VALUE
`)
			result := testutil.Composite(t, root, "- uses: ./.github/actions/lint\n- uses: ./.github/actions/lint", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			var findings []diagmodel.Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" {
					findings = append(findings, d)
				}
			}
			if len(findings) != 1 || filepath.Join(root, findings[0].Path) != metadata || findings[0].Start != (diagmodel.Position{Line: 8, Column: 14}) || !strings.Contains(findings[0].Message, "SC2086") {
				t.Fatalf("metadata diagnostic: %+v", result.Diagnostics)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("metadata not tracked: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckActionPaths(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	t.Setenv("GITHUB_WORKSPACE", "")
	foreign := t.TempDir()
	t.Setenv("GITHUB_ACTION_PATH", foreign)
	testutil.WriteFixture(t, foreign, ".shellcheckrc", "disable=SC2086\n")
	testutil.WriteFixture(t, foreign, "lib/value.sh", "VALUE=42\n")
	for _, selection := range []string{
		`"${{ github.action_path }}/.shellcheckrc"`,
		`{source-path: ["${{ github.action_path }}/lib"]}`,
	} {
		t.Run(selection, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: "+selection+"}}\n")
			outerRC := testutil.WriteFixture(t, root, "outer/.shellcheckrc", "disable=SC2086\n")
			innerRC := testutil.WriteFixture(t, root, "inner/.shellcheckrc", "# Inner action keeps SC2086 enabled.\n")
			testutil.WriteFixture(t, root, "outer/lib/value.sh", "VALUE=42\n")
			testutil.WriteFixture(t, root, "inner/lib/value.sh", "VALUE='two words'\n")
			testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        . value.sh
        echo $VALUE
    - uses: ./inner
`)
			inner := testutil.WriteFixture(t, root, "inner/action.yaml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        . value.sh
        echo $VALUE
`)
			result := testutil.Composite(t, root, "- uses: ./outer\n- uses: ./outer", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			var findings []diagmodel.Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" {
					findings = append(findings, d)
				}
			}
			if len(findings) != 1 || filepath.Join(root, findings[0].Path) != inner || !strings.Contains(findings[0].Message, "SC2086") {
				t.Fatalf("action configuration leaked between invocations: %+v", findings)
			}
			if strings.Contains(selection, ".shellcheckrc") {
				for _, path := range []string{outerRC, innerRC} {
					if !slices.Contains(result.Inputs, path) {
						t.Errorf("action-specific rc file missing from inputs: %s", path)
					}
				}
			}
		})
	}
}

func TestCompositeSyntaxAndNestedActions(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	metadata := testutil.WriteFixture(t, root, "inner/action.yaml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        if then
    - shell: python
      run: |
        print(
    - uses: ./outer
`)
	result := testutil.Composite(t, root, "- uses: ./outer", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	for _, rule := range []string{"shellcheck"} {
		if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == rule && filepath.Join(root, d.Path) == metadata }) {
			t.Errorf("missing %s syntax finding: %+v", rule, result.Diagnostics)
		}
	}
	if !slices.Contains(result.Inputs, metadata) {
		t.Fatalf("nested metadata not tracked: %v", result.Inputs)
	}
}

func TestCompositeExecutableBit(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: ./bad.sh
`)
	for _, tc := range []struct {
		name, steps string
		want        bool
	}{
		{"checkout", "- uses: actions/checkout@v6\n- uses: ./local", true},
		{"true call", `- uses: actions/checkout@v6
- uses: ./local
  if: true`, true},
		{"true expression call", `- uses: actions/checkout@v6
- uses: ./local
  if: ${{ true }}`, true},
		{"skipped call", `- uses: actions/checkout@v6
- uses: ./local
  if: false`, false},
		{"skipped expression call", `- uses: actions/checkout@v6
- uses: ./local
  if: ${{ false }}`, false},
		{"conditional call", `- uses: actions/checkout@v6
- uses: ./local
  if: github.event_name == 'push'`, true},
		{"conditional repository call", `- uses: actions/checkout@v6
- uses: ./local
  if: github.event.repository.name`, true},
		{"no checkout", "- uses: ./local", false},
		{"earlier chmod", `- uses: actions/checkout@v6
- run: chmod +x bad.sh
  shell: bash
  working-directory: ''
- uses: ./local`, false},
		{"startup env", `- uses: actions/checkout@v6
- uses: ./local
  env:
    BASH_ENV: setup.sh`, false},
		{"caller PATH", `- uses: actions/checkout@v6
- uses: ./local
  env: {PATH: /usr/bin}`, true},
		{"caller PATH and startup script", `- uses: actions/checkout@v6
- uses: ./local
  env: {PATH: /usr/bin, BASH_ENV: setup.sh}`, false},
		{"background", `- uses: actions/checkout@v6
- uses: ./local
  background: true`, false},
		{"background false", `- uses: actions/checkout@v6
- uses: ./local
  background: false`, true},
		{"background expression false", `- uses: actions/checkout@v6
- uses: ./local
  background: ${{ false }}`, true},
		{"background expression true", `- uses: actions/checkout@v6
- uses: ./local
  background: ${{ true }}`, false},
		{"background expression unknown", `- uses: actions/checkout@v6
- uses: ./local
  background: ${{ inputs.background }}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := testutil.Composite(t, root, tc.steps, application.Options{})
			found := false
			for _, d := range result.Diagnostics {
				if d.Rule == "executable-bit" {
					found = true
					if filepath.Join(root, d.Path) != metadata || !strings.Contains(d.Message, `script "bad.sh"`) {
						t.Fatalf("caller defaults or source leaked: %+v", d)
					}
				}
			}
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestConditionalCompositeRetainsStaticChecks(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	inner := testutil.WriteFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        ./bad.sh
        echo $VALUE
`)
	for _, tc := range []struct {
		outer, inner string
		finding      bool
	}{
		{"github.event.repository.name", "true", true},
		{"true", "github.event.repository.name", true},
		{"true", "true", true},
		{"false", "true", false},
		{"false", "github.event.repository.name", false},
		{"github.event.repository.name", "false", false},
	} {
		t.Run(tc.outer+"/"+tc.inner, func(t *testing.T) {
			testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
      if: `+tc.inner+"\n")
			result := testutil.Composite(t, root, `- uses: actions/checkout@v6
- uses: ./local
  if: `+tc.outer, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			finding := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == inner
			})
			if finding != tc.finding {
				t.Errorf("conditional finding=%v, want %v: %+v", finding, tc.finding, result.Diagnostics)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == inner && strings.Contains(d.Message, "SC2086")
			}) {
				t.Fatalf("conditional nested script lost static checks: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeScriptConfig(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo $VALUE
`)
	for _, config := range []string{"tools: {shellcheck: false}\n", `paths:
  'local/**':
    ignore: ['SC2086']
`} {
		testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
		result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
		if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "shellcheck" }) {
			t.Fatalf("config ignored: %+v", result.Diagnostics)
		}
	}
}

func TestCompositeShellcheckWorkingDirectory(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "scripts/lib.sh", "if then\n")
	testutil.WriteFixture(t, root, "lib.sh", "echo ok\n")
	for _, directory := range []string{"", "      working-directory: scripts\n"} {
		file := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
`+directory+"      run: . ./lib.sh\n")
		result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
		found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
			return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == file
		})
		if found != (directory != "") {
			t.Fatalf("directory %q: %+v", directory, result.Diagnostics)
		}
	}
}

func TestCompositeActionPathWorkingDirectory(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "lib.sh", "echo ok\n")
	testutil.WriteFixture(t, root, "outer/lib.sh", "echo ok\n")
	testutil.WriteFixture(t, root, "inner/lib.sh", "if then\n")
	inner := testutil.WriteFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: . ./lib.sh
`)
	outer := testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
    - shell: bash
      working-directory: ${{ github.action_path }}
      run: . ./lib.sh
`)
	result := testutil.Composite(t, root, "- uses: ./outer", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == inner
	}) {
		t.Fatalf("missing sourced-file finding in nested action: %+v", result.Diagnostics)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == outer
	}) {
		t.Fatalf("nested action_path leaked into caller: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedMetadataErrors(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	outer := testutil.WriteFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	for _, content := range []string{"runs: [\n", `name: inner
description: test
runs:
  using: composite
  steps:
    - run: echo ok
`} {
		inner := testutil.WriteFixture(t, root, "inner/action.yml", content)
		result := testutil.Composite(t, root, "- uses: ./outer\n- uses: ./inner", application.Options{})
		if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
			return d.Rule == "action" && (filepath.Join(root, d.Path) == outer && strings.Contains(d.Message, "could not parse") || filepath.Join(root, d.Path) == inner && strings.Contains(d.Message, "shell"))
		}) {
			t.Fatalf("nested metadata error lost: %+v", result.Diagnostics)
		}
	}
}

func TestCompositeExecutionState(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	for _, tc := range []struct {
		name, actionSteps, workflowSteps string
		want                             bool
	}{
		{"numeric startup env", `- shell: bash
  env: {BASH_ENV: 123}
  run: ./bad.sh`, "- uses: actions/checkout@v6\n- uses: ./local", false},
		{"caller PATH chmod uncertainty", "- shell: bash\n  run: chmod +x good.sh && ./bad.sh", `- uses: actions/checkout@v6
- uses: ./local
  env: {PATH: tools}`, false},
		{"caller PATH builtin", "- shell: bash\n  run: echo ok; ./bad.sh", `- uses: actions/checkout@v6
- uses: ./local
  env: {PATH: /usr/bin}`, true},
		{"checkout clean false", `- uses: actions/checkout@v6
  with: {clean: false}
- shell: bash
  run: ./bad.sh`, "- uses: ./local", false},
		{"conditional checkout", "- uses: actions/checkout@v6", `- uses: ./local
  if: false
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"conditional checkout and invocation", `- uses: actions/checkout@v6
- shell: bash
  run: ./bad.sh`, "- uses: ./local\n  if: false", false},
		{"checkout after conditional call", "- shell: bash\n  run: echo ok", `- uses: ./local
  if: false
- uses: actions/checkout@v6
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
		{"skipped call preserves caller", "- shell: bash\n  run: chmod +x bad.sh", `- uses: actions/checkout@v6
- uses: ./local
  if: ${{ false }}
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
		{"contradictory call preserves caller", "- shell: bash\n  run: chmod +x bad.sh", `- uses: actions/checkout@v6
- uses: ./local
  if: success() && failure()
- shell: bash
  working-directory: .
  run: ./bad.sh`, true},
		{"skipped background preserves caller", "- shell: bash\n  run: echo ok", `- uses: actions/checkout@v6
- uses: ./local
  if: false
  background: true
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
		{"false background preserves caller", "- shell: bash\n  run: echo ok", `- uses: actions/checkout@v6
- uses: ./local
  background: ${{ false }}
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
		{"true composite checkout", "- uses: actions/checkout@v6", `- uses: ./local
  if: ${{ true }}
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
		{"opaque composite preserves uncertainty", "- run: git config core.fileMode false && chmod +x bad.sh\n  shell: bash", `- uses: actions/checkout@v6
- uses: ./local
- uses: actions/checkout@v6
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"conditional opaque composite preserves uncertainty", "- run: git config core.fileMode false && chmod +x bad.sh\n  shell: bash", `- uses: actions/checkout@v6
- uses: ./local
  if: github.event_name == 'push'
- uses: actions/checkout@v6
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"conditional background call", "- shell: bash\n  run: echo ok", `- uses: ./local
  if: github.event_name == 'push'
  background: true
- uses: actions/checkout@v6
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"conditional inner background", `- shell: bash
  background: true
  run: echo ok`, `- uses: ./local
  if: github.event_name == 'push'
- uses: actions/checkout@v6
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"inner chmod", "- shell: bash\n  run: chmod +x bad.sh", `- uses: actions/checkout@v6
- uses: ./local
- shell: bash
  working-directory: ''
  run: ./bad.sh`, false},
		{"read-only composite", "- shell: bash\n  run: echo ok", `- uses: actions/checkout@v6
- uses: ./local
- shell: bash
  working-directory: ''
  run: ./bad.sh`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    `+strings.ReplaceAll(tc.actionSteps, "\n", "\n    ")+"\n")
			result := testutil.Composite(t, root, tc.workflowSteps, application.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeToleratedFailureState(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	for _, setting := range []string{"true", "${{ github.event_name == 'push' }}", "false", "${{ false }}"} {
		t.Run(setting, func(t *testing.T) {
			metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
    - shell: bash
      run: ./bad.sh
`)
			result := testutil.Composite(t, root, "- uses: ./local\n  continue-on-error: "+setting+`
- shell: bash
  working-directory: .
  run: ./bad.sh`, application.Options{})
			inner, outer := false, false
			for _, finding := range result.Diagnostics {
				if finding.Rule != "executable-bit" {
					continue
				}
				if filepath.Join(root, finding.Path) == metadata {
					inner = true
				} else {
					outer = true
				}
			}
			if !inner {
				t.Fatalf("current invocation lost finding: %+v", result.Diagnostics)
			}
			// A direct script call itself invalidates subsequent permissions. Use a
			// checkout-only body to isolate tolerated failure propagation below.
			if outer {
				t.Fatalf("script effects remained certain: %+v", result.Diagnostics)
			}
			testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`)
			result = testutil.Composite(t, root, "- uses: ./local\n  continue-on-error: "+setting+`
- shell: bash
  working-directory: .
  run: ./bad.sh`, application.Options{})
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" })
			want := setting == "false" || setting == "${{ false }}"
			if found != want {
				t.Fatalf("outgoing certainty = %v, want %v: %+v", found, want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutEmptyExpressions(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: ./source/bad.sh
`)
	for _, inputs := range []string{
		"path: ' source '",
		"path: \"${{ ' source ' }}\"",
		"repository: \"${{ '' }}\"",
		"ref: \"${{ '' }}\"",
		"sparse-checkout: \"${{ '' }}\"",
		"repository: \"${{ '' }}\", ref: \"${{ '' }}\", sparse-checkout: \"${{ '' }}\"",
	} {
		t.Run(inputs, func(t *testing.T) {
			checkout := inputs
			if !strings.HasPrefix(checkout, "path:") {
				checkout = "path: source, " + checkout
			}
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: {"+checkout+"}\n- uses: ./source/local", application.Options{})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("checkout metadata not resolved: %v", result.Inputs)
			}
			if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
			}) {
				t.Fatalf("missing composite script finding: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutActionOutputs(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
outputs:
  answer:
    description: test
    value: '42'
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
	for _, spec := range []string{"./source/local", "$/local"} {
		t.Run(spec, func(t *testing.T) {
			result := testutil.Composite(t, root, `- uses: actions/checkout@v6
  with: {path: source}
- uses: `+spec+`
  id: local
- run: echo '${{ steps.local.outputs.missing }}'`, application.Options{})
			if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "expression-reference" && strings.Contains(d.Message, `property "missing" is not defined`)
			}) {
				t.Fatalf("action outputs were not resolved: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeParallelCheckoutPlacement(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: echo missing shell
`)
	for _, tc := range []struct {
		name, steps string
		read        bool
	}{
		{"sibling", `- parallel:
    - uses: actions/checkout@v6
      with: {path: source}
    - uses: ./source/local`, false},
		{"following step", `- parallel:
    - uses: actions/checkout@v6
      with: {path: source}
- uses: ./source/local`, false},
		{"later checkout", `- parallel:
    - uses: actions/checkout@v6
      with: {path: source}
- uses: actions/checkout@v6
  with: {path: source}
- uses: ./source/local`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := testutil.Composite(t, root, tc.steps, application.Options{})
			if read := slices.Contains(result.Inputs, metadata); read != tc.read {
				t.Fatalf("metadata read = %v, want %v: %v", read, tc.read, result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckRelocatedCheckout(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      with: {path: relocated}
    - shell: bash
      working-directory: relocated
      run: . ./lib.sh
`)
	testutil.WriteFixture(t, root, "lib.sh", "if then\n")
	testutil.WriteFixture(t, root, "relocated/lib.sh", "echo wrong file\n")
	result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata
	}) {
		t.Fatalf("relocated checkout source was not checked: %+v", result.Diagnostics)
	}
}

func TestCompositeShellcheckCallerCheckoutState(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      with: {path: relocated}
`)
	testutil.WriteFixture(t, root, "lib.sh", "VALUE=42\n")
	run := `- shell: bash
  working-directory: .
  run: |
    . ./lib.sh
    echo $VALUE`
	for _, tc := range []struct {
		name, steps string
		warnings    int
	}{
		{"relocated root", "- uses: ./local\n" + run, 1},
		{"relocated directory", "- uses: ./local\n" + strings.Replace(run, "working-directory: .", "working-directory: relocated", 1), 0},
		// Uncertain checkout at relocated leaves the existing root source intact.
		{"conditional call retains root", `- uses: actions/checkout@v6
- uses: ./local
  if: inputs.checkout
` + run, 0},
		{"tolerated failure retains root", `- uses: actions/checkout@v6
- uses: ./local
  continue-on-error: true
` + run, 0},
		{"conditional destination unknown", `- uses: actions/checkout@v6
- uses: ./local
  if: inputs.checkout
` + strings.Replace(run, "working-directory: .", "working-directory: relocated", 1), 1},
		{"failed destination unknown", `- uses: actions/checkout@v6
- uses: ./local
  continue-on-error: true
` + strings.Replace(run, "working-directory: .", "working-directory: relocated", 1), 1},
		{"skipped call", `- uses: actions/checkout@v6
- uses: ./local
  if: false
` + run, 0},
		{"retained root", `- uses: actions/checkout@v6
- uses: ./local
` + run, 0},
		{"parallel children and following step", `- uses: actions/checkout@v6
- parallel:
    ` + strings.ReplaceAll(run, "\n", "\n    ") + "\n" + run, 2},
		{"checkout after parallel", `- uses: actions/checkout@v6
- parallel:
    - shell: bash
      run: echo ok
- uses: actions/checkout@v6
` + run, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := testutil.Composite(t, root, tc.steps, application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
			warnings := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "shellcheck" && strings.Contains(diagnostic.Message, "SC2086") {
					warnings++
				}
			}
			if warnings != tc.warnings {
				t.Fatalf("ShellCheck warnings=%d, want %d: %+v", warnings, tc.warnings, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutEmptyPath(t *testing.T) {
	root, _ := testutil.ExecutableFixture(t)
	metadata := testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ok
`)
	for _, value := range []string{"", "${{ '' }}"} {
		t.Run(value, func(t *testing.T) {
			result := testutil.Composite(t, root, "- uses: actions/checkout@v6\n  with: {path: \""+value+"\"}\n- uses: ./local", application.Options{})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("empty checkout path did not use workspace root: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeShellcheckUnknownCheckout(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	root, _ := testutil.ExecutableFixture(t)
	testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
      with: {path: '${{ github.event.repository.name }}'}
    - shell: bash
      run: . ./lib.sh
`)
	testutil.WriteFixture(t, root, "lib.sh", "if then\n")
	result := testutil.Composite(t, root, "- uses: ./local", application.Options{Tools: (adapters.Options{ShellCheck: command}).Registrations()})
	if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "shellcheck" }) {
		t.Fatalf("unknown checkout sourced local decoy: %+v", result.Diagnostics)
	}
}
