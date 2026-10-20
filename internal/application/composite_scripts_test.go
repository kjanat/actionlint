package application_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/internal/application"
	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/render"
	"actionlint.kjanat.dev/internal/tools/adapters"
)

func TestCompositeShellcheckAcrossWorkflows(t *testing.T) {
	root, _ := executableFixture(t)
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeFixture(t, root, "local/action.yml", `name: lint
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo $VALUE
`)
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local
`
	first := writeFixture(t, root, ".github/workflows/a.yml", workflow)
	second := writeFixture(t, root, ".github/workflows/b.yml", workflow)
	session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: shellcheckForTest(t)}).Registrations()}
	var err error

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || filepath.Join(root, result.Diagnostics[0].Path) != metadata {
		t.Fatalf("expected one shared-action finding: %+v", result.Diagnostics)
	}
	if result.FileCount != 2 {
		t.Fatalf("file and finding counts: %d, %d", result.FileCount, len(result.Diagnostics))
	}
	for _, path := range []string{first, second, metadata} {
		if !slices.Contains(result.Inputs, path) {
			t.Errorf("input missing: %s", path)
		}
	}
	renderer, err := render.New("", "{{json .}}", false, buildinfo.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := renderer.Render(&out, render.Prepare(*result)); err != nil {
		t.Fatal(err)
	}
	var findings []render.TemplateFields
	if err := json.Unmarshal([]byte(out.String()), &findings); err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || filepath.Join(root, findings[0].Filepath) != metadata || !strings.Contains(findings[0].Snippet, "echo $VALUE") {
		t.Fatalf("legacy rendering lost the shared source: %+v", findings)
	}
}

func TestCompositeShellcheckConfigValidation(t *testing.T) {
	command := shellcheckForTest(t)
	for _, tc := range []struct {
		name, actionSteps, workflowSteps string
		missingRC, wantError             bool
	}{
		{"python", "- shell: python\n  run: print('hello')", "- uses: ./local", false, false},
		{"python missing rc", "- shell: python\n  run: print('hello')", "- uses: ./local", true, true},
		{"powershell", "- shell: pwsh\n  run: Write-Output hello", "- uses: ./local", false, false},
		{"powershell missing rc", "- shell: pwsh\n  run: Write-Output hello", "- uses: ./local", true, true},
		{"uses only", "- uses: actions/checkout@v6", "- uses: ./local", false, false},
		{"uses only missing rc", "- uses: actions/checkout@v6", "- uses: ./local", true, true},
		{"no composite", "- uses: actions/checkout@v6", "- uses: actions/checkout@v6", false, true},
		{"workflow python", "- uses: actions/checkout@v6", `- uses: ./local
- shell: python
  run: print('hello')`, false, true},
		{"workflow bash", "- uses: actions/checkout@v6", `- uses: ./local
- shell: bash
  run: echo hello`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: {config: '${{ github.action_path }}/.shellcheckrc'}}\n")
			writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    `+strings.ReplaceAll(tc.actionSteps, "\n", "\n    ")+"\n")
			if !tc.missingRC {
				writeFixture(t, root, "local/.shellcheckrc", "disable=SC2086\n")
			}
			workflow := writeFixture(t, root, ".github/workflows/test.yml", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      `+strings.ReplaceAll(tc.workflowSteps, "\n", "\n      ")+"\n")
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "tools.shellcheck.config") {
					t.Fatalf("expected config error, got %v", err)
				}
			} else if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("valid action context rejected: %v, %+v", err, result)
			}
		})
	}
}

func TestCompositeMacOSExecutableBit(t *testing.T) {
	root, _ := executableFixture(t)
	writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	metadata := writeFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: ./SCRIPTS/BAD.SH
`)
	workflow := writeFixture(t, root, ".github/workflows/mac.yml", `on: push
jobs:
  test:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v6
      - uses: ./local
`)
	session := application.Options{WorkingDir: root}
	var err error

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
		return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, `script "scripts/bad.sh"`)
	}) {
		t.Fatalf("nested action lost macOS path matching: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedCheckoutGitEnvironment(t *testing.T) {
	root, _ := executableFixture(t)
	writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - uses: ./inner
`)
	for _, tc := range []struct {
		name, workflowEnv, jobEnv, callEnv, checkoutEnv string
		want                                            bool
	}{
		{"workflow", "env: {GIT_WORK_TREE: /tmp}\n", "", "", "", false},
		{"job", "", "    env: {GIT_WORK_TREE: /tmp}\n", "", "", false},
		{"caller", "", "", "        env: {GIT_WORK_TREE: /tmp}\n", "", false},
		{"checkout", "", "", "", "      env: {GIT_WORK_TREE: /tmp}\n", false},
		{"caller config", "", "", "        env: {GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: core.worktree, GIT_CONFIG_VALUE_0: /tmp}\n", "", false},
		{"caller trace", "", "", "        env: {GIT_TRACE: '1'}\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata := writeFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`+tc.checkoutEnv+`    - shell: bash
      run: ./bad.sh
`)
			workflow := writeFixture(t, root, ".github/workflows/env.yml", "on: push\n"+tc.workflowEnv+`jobs:
  test:
    runs-on: ubuntu-latest
`+tc.jobEnv+`    steps:
      - uses: ./local
`+tc.callEnv)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested metadata checks lost: %v", result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "executable-bit" })
			if found != tc.want {
				t.Fatalf("executable-bit = %v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeShellcheckWindowsDirectory(t *testing.T) {
	command := shellcheckForTest(t)
	for _, checkout := range []string{"", "source"} {
		t.Run(checkout, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
			prefix := ""
			if checkout != "" {
				prefix = checkout + "/"
			}
			writeFixture(t, root, "outer/action.yml", `name: outer
description: test
runs:
  using: composite
  steps:
    - uses: ./`+prefix+"inner\n")
			directory := strings.ReplaceAll(prefix+"scripts/build", "/", `\`)
			metadata := writeFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: bash
      working-directory: '`+directory+`'
      run: |
        . ./lib.sh
        echo $VALUE
`)
			writeFixture(t, root, "scripts/build/lib.sh", "VALUE=42\n")
			workflow := writeFixture(t, root, ".github/workflows/composite.yml", `on: push
jobs:
  test:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v6
        with: {path: '`+checkout+"'}\n      - uses: ./"+prefix+"outer\n")
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested metadata not analyzed: %v", result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "shellcheck" }) {
				t.Fatalf("Windows composite source not resolved: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeShellcheckCallerCheckoutAcrossJobs(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeFixture(t, root, "setup/action.yml", `name: setup
description: test
runs:
  using: composite
  steps:
    - uses: actions/checkout@v6
`)
	writeFixture(t, root, "lib.sh", "VALUE=42\n")
	// Either traversal order leaves an unknown checkout before the other job.
	job := `    runs-on: ubuntu-latest
    steps:
      - uses: $/setup
      - shell: bash
        run: |
          . ./lib.sh
          echo $VALUE
      - uses: actions/checkout@v6
        with: {repository: other/repo}
`
	workflow := writeFixture(t, root, ".github/workflows/jobs.yml", `on: push
jobs:
  first:
`+job+"  second:\n"+job)
	session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
	var err error

	result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == "shellcheck" }) {
		t.Fatalf("previous job checkout leaked into caller: %+v", result.Diagnostics)
	}
}
