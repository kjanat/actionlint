package actionlint

import (
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCompositeRetainedCheckoutLocations(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\noutputs:\n  answer:\n    description: test\n    value: '42'\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: source/local\n      run: |\n        . ./lib.sh\n        echo $VALUE\n")
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
			steps := "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: actions/checkout@v6\n  " + strings.ReplaceAll(tc.second, "\n", "\n  ") + "\n- uses: ./source/local\n  id: local\n- run: echo '${{ steps.local.outputs.missing }}'"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			if tc.read && !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "expression" && strings.Contains(d.Message, `property "missing" is not defined`)
			}) {
				t.Fatalf("earlier checkout output metadata was lost: %+v", result.Diagnostics)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == metadata }) {
				t.Fatalf("earlier checkout source was not followed: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeUnixColonCheckoutDestination(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	for _, runner := range []string{"ubuntu-latest", "windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/colon.yml", "on: push\njobs:\n  test:\n    runs-on: "+runner+"\n    steps:\n      - uses: actions/checkout@v6\n        with: {path: 'a:debug'}\n      - uses: ./a:debug/local\n")
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
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: ./bad.sh\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: |\n        . ./lib.sh\n        echo $VALUE\n    - shell: bash\n      working-directory: ${{ github.workspace }}\n      run: |\n        . ./lib.sh\n        echo $VALUE\n")
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
		{"opaque execution", "- uses: actions/checkout@v6\n  with: {repository: other/repo}\n- uses: other/action@v1", "", false},
		{"startup environment", "- uses: actions/checkout@v6\n  with: {repository: other/repo}", "\n  env: {BASH_ENV: setup.sh}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := compositeAnalysis(t, root, tc.before+"\n- uses: $/local"+tc.call+"\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", AnalysisOptions{Shellcheck: command})
			var shell, executable []Diagnostic
			for _, d := range result.Diagnostics {
				if d.Rule == "shellcheck" && d.Path == metadata {
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
				if len(executable) != 1 || executable[0].Path != metadata || !strings.Contains(executable[0].Message, `"local/bad.sh"`) {
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
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	for _, runner := range []string{"windows-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/windows.yml", "on: push\njobs:\n  test:\n    runs-on: "+runner+"\n    steps:\n      - uses: actions/checkout@v6\n        with: {path: 'source\\repo'}\n      - uses: ./source/repo/local\n")
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
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: echo ok\n")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {repository: other/repo}\n- uses: $/local\n- shell: bash\n  working-directory: .\n  run: ./bad.sh", AnalysisOptions{})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("self-repository action restored workspace certainty: %+v", result.Diagnostics)
	}
}

func TestCompositeNestedIndependentActionPath(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "outer/action.yml", "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: $/inner\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: cd scripts && ./bad.sh\n")
	writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: echo ok\n")
	writeShellcheckFixture(t, root, "inner/placeholder", "")
	metadata := writeShellcheckFixture(t, root, "outer/scripts/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "inner/placeholder", "outer/scripts/bad.sh")
	git("update-index", "--chmod=-x", "outer/scripts/bad.sh")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n  with: {repository: other/repo}\n- uses: $/outer", AnalysisOptions{})
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
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: source\n      run: ./bad.sh\n")
	git("add", "bad.sh")
	git("update-index", "--chmod=-x", "bad.sh")
	for _, mutation := range []string{"chmod +x source/bad.sh", "unknown-command"} {
		t.Run(mutation, func(t *testing.T) {
			steps := "- uses: actions/checkout@v6\n  with: {path: source}\n- shell: bash\n  working-directory: .\n  run: " + mutation + "\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- uses: actions/checkout@v6\n  with: {path: mirror}\n- uses: ./source/local"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{})
			if !slices.Contains(result.Inputs, metadata) {
				t.Fatalf("earlier checkout metadata was lost: %v", result.Inputs)
			}
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
				t.Fatalf("new checkout restored modified earlier copy: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeIndependentActionChangesSurviveCheckout(t *testing.T) {
	root, git := executableFixture(t)
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: chmod +x bad.sh\n    - uses: actions/checkout@v6\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: ./bad.sh\n")
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- uses: $/local", AnalysisOptions{})
	if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" }) {
		t.Fatalf("workspace checkout restored independent action mode: %+v", result.Diagnostics)
	}
}

func TestCompositeCheckoutPrefixCase(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: source/local\n      run: |\n        . ./lib.sh\n        echo $VALUE\n")
	writeShellcheckFixture(t, root, "local/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest", "ubuntu-latest", "self-hosted"} {
		t.Run(runner, func(t *testing.T) {
			workflow := writeShellcheckFixture(t, root, ".github/workflows/case.yml", "on: push\njobs:\n  test:\n    runs-on: "+runner+"\n    steps:\n      - uses: actions/checkout@v6\n        with: {path: Source}\n      - uses: ./source/local\n")
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
			if slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "shellcheck" && d.Path == metadata }) {
				t.Fatalf("Windows checkout prefix lost source resolution: %+v", result.Diagnostics)
			}
		})
	}
}

func TestCompositeForeignCheckoutMetadata(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
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
	writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: chmod +x bad.sh\n")
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	for _, spec := range []string{"$/local", "./local"} {
		t.Run(spec, func(t *testing.T) {
			result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- uses: "+spec+"\n- shell: bash\n  working-directory: .\n  run: ./local/bad.sh", AnalysisOptions{})
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
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: ${{ github.action_path }}\n      run: ./bad.sh\n")
	writeShellcheckFixture(t, root, "local/bad.sh", "#!/bin/sh\necho bad\n")
	git("add", "local/bad.sh")
	git("update-index", "--chmod=-x", "local/bad.sh")
	result := compositeAnalysis(t, root, "- uses: actions/checkout@v6\n- shell: bash\n  working-directory: .\n  run: chmod +x local/bad.sh\n- uses: $/local", AnalysisOptions{})
	if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == "executable-bit" && d.Path == metadata }) {
		t.Fatalf("workspace mutation suppressed independent action finding: %+v", result.Diagnostics)
	}
}
