package actionlint

import (
	"slices"
	"strings"
	"testing"
)

func TestCompositePersistentCheckoutEnvironment(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	writeShellcheckFixture(t, root, "prepare/action.yml", "name: prepare\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo 'GIT_WORK_TREE=/tmp/tree' >> \"$GITHUB_ENV\"\n")
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
		{"skipped run", "- shell: bash\n  run: make\n  if: false", true},
		{"literal shell", "- shell: bash\n  run: echo ready && true", true},
		{"literal chmod", "- shell: bash\n  run: chmod +x bad.sh", true},
		{"step Git env", "- shell: bash\n  run: echo ready\n  env:\n    GIT_WORK_TREE: /tmp/other\n    HOME: /tmp/home", true},
		{"custom shell", "- shell: bash -c {0}\n  run: echo ready", false},
		{"unknown shell", "- shell: ${{ inputs.shell }}\n  run: echo ready", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.before+"\n- uses: actions/checkout@v6\n- uses: ./local", AnalysisOptions{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
		})
	}
	workflow := writeShellcheckFixture(t, root, ".github/workflows/env-jobs.yml", "on: push\njobs:\n  first:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: make\n  second:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v6\n      - uses: ./local\n")
	session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Files([]string{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Inputs, metadata) {
		t.Fatalf("persistent environment leaked across jobs: %s", strings.Join(result.Inputs, ", "))
	}
}

func TestCompositePersistentEnvironmentPlatforms(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	for _, tc := range []struct {
		name, runner, run string
		read              bool
	}{
		{"Windows PATH", "windows-latest", "shell: bash\nrun: echo ready\nenv:\n  path: ./tools", false},
		{"Windows startup", "windows-latest", "shell: bash\nrun: echo ready\nenv:\n  bash_env: startup.sh", false},
		{"Windows Git env", "windows-latest", "shell: bash\nrun: echo ready\nenv:\n  git_work_tree: other\n  home: other", true},
		{"POSIX distinct env", "ubuntu-latest", "shell: bash\nrun: echo ready\nenv:\n  path: ./tools\n  bash_env: startup.sh", true},
		{"Windows default shell", "windows-latest", "run: echo ready", false},
		{"POSIX default shell", "ubuntu-latest", "run: echo ready", true},
		{"unknown default shell", "${{ inputs.runner }}", "run: echo ready", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/platform-env.yml", "on: push\njobs:\n  test:\n    runs-on: "+tc.runner+"\n    steps:\n      - "+strings.ReplaceAll(tc.run, "\n", "\n        ")+"\n      - uses: actions/checkout@v6\n      - uses: ./local\n")
			session, err := NewAnalysisSession(AnalysisOptions{WorkingDir: root})
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.Files([]string{workflow}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
		})
	}
}
