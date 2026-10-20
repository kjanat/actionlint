package application_test

import (
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
)

func TestCompositePersistentCheckoutEnvironment(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	writeFixture(t, root, "prepare/action.yml", `name: prepare
description: test
runs:
  using: composite
  steps:
    - shell: bash
      run: echo 'GIT_WORK_TREE=/tmp/tree' >> "$GITHUB_ENV"
`)
	for _, tc := range []struct {
		name, before string
		read         bool
	}{
		{"Bash env write", "- shell: bash\n  run: echo 'GIT_WORK_TREE=/tmp/tree' >> \"$GITHUB_ENV\"", false},
		{"PowerShell env write", "- shell: pwsh\n  run: \"'GIT_WORK_TREE=D:\\\\other' >> $env:GITHUB_ENV\"", false},
		{"opaque program", "- shell: bash\n  run: make", false},
		{"opaque action", "- uses: other/setup@v1", false},
		{"composite env write", "- uses: $/prepare", false},
		{"conditional composite env write", "- uses: $/prepare\n  if: github.event_name == 'push'", false},
		{"skipped composite env write", "- uses: $/prepare\n  if: false", true},
		{"case skips composite env write", "- uses: $/prepare\n  if: case(true, false, true)", true},
		{"case enables composite env write", "- uses: $/prepare\n  if: case(false, false, true)", false},
		{"case fallback skips composite env write", "- uses: $/prepare\n  if: case(false, true, false)", true},
		{"skipped opaque action", "- uses: other/setup@v1\n  if: false", true},
		{"skipped run", `- shell: bash
  run: make
  if: false`, true},
		{"literal shell", "- shell: bash\n  run: echo ready && true", true},
		{"literal chmod", "- shell: bash\n  run: chmod +x bad.sh", true},
		{"step Git env", `- shell: bash
  run: echo ready
  env:
    GIT_WORK_TREE: /tmp/other
    HOME: /tmp/home`, true},
		{"custom shell", "- shell: bash -c {0}\n  run: echo ready", false},
		{"unknown shell", "- shell: ${{ inputs.shell }}\n  run: echo ready", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.before+`
- uses: actions/checkout@v6
- uses: ./local`, application.Options{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
		})
	}
	workflow := writeFixture(t, root, ".github/workflows/env-jobs.yml", `on: push
jobs:
  first:
    runs-on: ubuntu-latest
    steps:
      - shell: bash
        run: make
  second:
    runs-on: ubuntu-latest
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
	if !slices.Contains(result.Inputs, metadata) {
		t.Fatalf("persistent environment leaked across jobs: %s", strings.Join(result.Inputs, ", "))
	}
}

func TestCompositePersistentEnvironmentPlatforms(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: composite
  steps:
    - run: missing shell
`)
	for _, tc := range []struct {
		name, runner, run string
		read              bool
	}{
		{"Windows PATH", "windows-latest", `shell: bash
run: echo ready
env:
  path: ./tools`, false},
		{"Windows startup", "windows-latest", `shell: bash
run: echo ready
env:
  bash_env: startup.sh`, false},
		{"Windows Git env", "windows-latest", `shell: bash
run: echo ready
env:
  git_work_tree: other
  home: other`, true},
		{"POSIX distinct env", "ubuntu-latest", `shell: bash
run: echo ready
env:
  path: ./tools
  bash_env: startup.sh`, true},
		{"Windows default shell", "windows-latest", "run: echo ready", false},
		{"POSIX default shell", "ubuntu-latest", "run: echo ready", true},
		{"unknown default shell", "${{ inputs.runner }}", "run: echo ready", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := writeFixture(t, root, ".github/workflows/platform-env.yml", `on: push
jobs:
  test:
    runs-on: `+tc.runner+`
    steps:
      - `+strings.ReplaceAll(tc.run, "\n", "\n        ")+`
      - uses: actions/checkout@v6
      - uses: ./local
`)
			session := application.Options{WorkingDir: root}
			var err error

			result, err := application.Check(t.Context(), session, application.Input{Kind: application.Files, Paths: []string{workflow}})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
		})
	}
}
