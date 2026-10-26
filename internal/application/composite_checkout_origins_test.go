package application_test

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestCompositeUnixColonCheckoutDestination(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, runner := range []string{"ubuntu-latest", "windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeFixture(t, root, ".github/workflows/colon.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: 'a:debug'}
      - uses: ./a:debug/local
`)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
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

func TestCompositeWindowsCheckoutDestination(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, runner := range []string{"windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeFixture(t, root, ".github/workflows/windows.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: 'source\repo'}
      - uses: ./source/repo/local
`)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != (runner == "windows-latest") {
				t.Fatalf("wrong checkout metadata selection: %v", result.Inputs)
			}
		})
	}
}

func TestCompositeRunnerReadonlyAssignments(t *testing.T) {
	root, _ := executableFixture(t)
	writeFixture(t, root, "outer/action.yml", `name: outer
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
			metadata := writeFixture(t, root, "inner/action.yml", `name: inner
description: test
runs:
  using: composite
  steps:
    - shell: `+tc.shell+`
      run: UID=0 ./bad.sh
`)
			workflow := writeFixture(t, root, ".github/workflows/readonly.yml", `on: push
jobs:
  test:
    runs-on: `+tc.runner+`
    steps:
      - uses: actions/checkout@v6
      - uses: ./outer
`)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("nested action not inspected: %v", result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "executable-bit" && filepath.Join(root, d.Path) == metadata
			})
			if found != tc.want {
				t.Fatalf("finding=%v, want %v: %+v", found, tc.want, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutPrefixCase(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeFixture(t, root, "local/action.yml", `name: local
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
	writeFixture(t, root, "local/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest", "ubuntu-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeFixture(t, root, ".github/workflows/case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
        with: {path: Source}
      - uses: ./source/local
`)
			session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != (runner == "windows-latest" || runner == "macos-latest") {
				t.Fatalf("wrong checkout metadata selection: %v", result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
				return d.Rule == "shellcheck" && filepath.Join(root, d.Path) == metadata
			}) {
				t.Fatalf("Windows checkout prefix lost source resolution: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeActionSubpathCase(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/Nested/action.yml", `name: local
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
				workflow := writeFixture(t, root, ".github/workflows/subpath-case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
      - uses: actions/checkout@v6
`+tc.checkout+"      - uses: "+tc.spec+"\n")
				session := application.Options{WorkingDir: root}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(result.Inputs, metadata) {
					t.Fatalf("canonical metadata not inspected: %v", result.Inputs)
				}
				if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool {
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
	writeFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeFixture(t, root, "local/Nested/action.yml", `name: local
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
	writeFixture(t, root, "local/Nested/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest"} {
		for _, tc := range []struct{ name, checkouts, spec string }{
			{"root", "      - uses: actions/checkout@v6\n", "./LOCAL/nested"},
			{"placed", `      - uses: actions/checkout@v6
        with: {path: Source}
`, "./source/LOCAL/nested"},
			{"repeated", `      - uses: actions/checkout@v6
        with: {path: Source}
      - uses: actions/checkout@v6
        with: {path: Mirror}
`, "./source/LOCAL/nested"},
			{"independent", "", "$/LOCAL/nested"},
		} {
			t.Run(runner+"/"+tc.name, func(t *testing.T) {
				workflow := writeFixture(t, root, ".github/workflows/source-case.yml", `on: push
jobs:
  test:
    runs-on: `+runner+`
    steps:
`+tc.checkouts+"      - uses: "+tc.spec+"\n")
				session := application.Options{WorkingDir: root, Tools: (adapters.Options{ShellCheck: command}).Registrations()}
				var err error

				result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
				if err != nil {
					t.Fatal(err)
				}
				var findings []diagmodel.Diagnostic
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
