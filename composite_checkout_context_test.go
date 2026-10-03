package actionlint

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCompositeConditionalCheckoutKeepsUnaffectedPaths(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	for _, tc := range []struct {
		name, destination, extra string
		read                     bool
	}{
		{"sibling", "mirror", "", true},
		{"same self checkout", "source", "", true},
		{"ancestor", ".", "", false},
		{"child", "source/local", "", false},
		{"unknown", "${{ inputs.path }}", "", false},
		{"tolerated sibling", "mirror", "\n  continue-on-error: true", true},
		{"tolerated same path", "source", "\n  continue-on-error: true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeShellcheckFixture(t, root, "refresh/action.yml", "name: refresh\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n      with: {path: '"+tc.destination+"'}\n")
			steps := "- uses: actions/checkout@v6\n  with: {path: source}\n- uses: ./source/refresh\n  if: inputs.refresh" + tc.extra + "\n- uses: ./source/local"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{})
			if slices.Contains(result.Inputs, metadata) != tc.read {
				t.Fatalf("metadata read=%v, want %v: %v", slices.Contains(result.Inputs, metadata), tc.read, result.Inputs)
			}
			found := slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool {
				return d.Rule == "action" && filepath.Join(root, d.Path) == metadata && strings.Contains(d.Message, "shell")
			})
			if found != tc.read {
				t.Fatalf("composite checked=%v, want %v: %+v", found, tc.read, result.Diagnostics)
			}
		})
	}
}

func TestCompositeCheckoutEnvironmentPlacement(t *testing.T) {
	root, _ := executableFixture(t)
	metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: missing shell\n")
	writeShellcheckFixture(t, root, "refresh/action.yml", "name: refresh\ndescription: test\nruns:\n  using: composite\n  steps:\n    - uses: actions/checkout@v6\n")
	for _, scope := range []string{"workflow", "job", "step", "composite"} {
		for _, tc := range []struct {
			name, environment string
			read              bool
		}{
			{"redirected worktree", "GIT_WORK_TREE: /tmp/tree", false},
			{"Git wrapper", "PATH: tools", false},
			{"ordinary env", "MY_VALUE: enabled", true},
			{"empty loader", "NODE_OPTIONS: ''", true},
		} {
			t.Run(scope+"/"+tc.name, func(t *testing.T) {
				workflowEnv, jobEnv, stepEnv := "", "", ""
				call := "actions/checkout@v6"
				switch scope {
				case "workflow":
					workflowEnv = "env: {" + tc.environment + "}\n"
				case "job":
					jobEnv = "    env: {" + tc.environment + "}\n"
				case "step", "composite":
					stepEnv = "        env: {" + tc.environment + "}\n"
					if scope == "composite" {
						call = "$/refresh"
					}
				}
				workflow := writeShellcheckFixture(t, root, ".github/workflows/environment.yml", "on: push\n"+workflowEnv+"jobs:\n  test:\n    runs-on: ubuntu-latest\n"+jobEnv+"    steps:\n      - uses: "+call+"\n"+stepEnv+"      - uses: ./local\n")
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
}

func TestRunDirectoryCaseInsensitiveSuffix(t *testing.T) {
	root := t.TempDir()
	writeShellcheckFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	want, err := filepath.EvalSymlinks(filepath.Join(root, "scripts", "Nested"))
	if err != nil {
		t.Fatal(err)
	}
	for _, checkout := range []string{"", "source"} {
		paths := runPaths{workspace: root, analysis: root, placements: &checkoutPlacement{
			directory: runDirectory{directoryKnown, checkout}, caseInsensitive: true,
		}}
		mapped, known := paths.analysisPath(joinRunnerPath(checkout, "SCRIPTS/nested"))
		if !known || mapped != filepath.Join(root, "scripts", "Nested") {
			t.Fatalf("checkout %q: mapped=%q, known=%v", checkout, mapped, known)
		}
		got := paths.resolve(runDirectory{directoryKnown, joinRunnerPath(checkout, "SCRIPTS/nested")})
		if got.kind != directoryKnown || got.path != want {
			t.Fatalf("checkout %q: directory=%+v, want %q", checkout, got, want)
		}
	}
}

func TestCompositeWorkingDirectorySuffixSources(t *testing.T) {
	command := shellcheckForTest(t)
	root, _ := executableFixture(t)
	writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: true}\n")
	writeShellcheckFixture(t, root, "scripts/Nested/lib.sh", "VALUE=42\n")
	writeShellcheckFixture(t, root, "local/scripts/Nested/lib.sh", "VALUE=42\n")
	for _, runner := range []string{"windows-latest", "macos-latest"} {
		for _, tc := range []struct{ name, spec, directory string }{
			{"workspace", "./source/local", "source/SCRIPTS/nested"},
			{"independent", "$/local", "${{ github.action_path }}/SCRIPTS/nested"},
		} {
			t.Run(runner+"/"+tc.name, func(t *testing.T) {
				metadata := writeShellcheckFixture(t, root, "local/action.yml", "name: local\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: "+tc.directory+"\n      run: |\n        . ./lib.sh\n        echo $VALUE\n        echo $OTHER\n")
				workflow := writeShellcheckFixture(t, root, ".github/workflows/suffix.yml", "on: push\njobs:\n  test:\n    runs-on: "+runner+"\n    steps:\n      - uses: actions/checkout@v6\n        with: {path: source}\n      - uses: "+tc.spec+"\n")
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
				if len(findings) != 1 || findings[0].Start.Line != 11 || !strings.Contains(findings[0].Message, "SC2086") {
					t.Fatalf("only unsourced variable should warn: %+v", result.Diagnostics)
				}
			})
		}
	}
}
