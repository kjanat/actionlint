package tools_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/processconfig"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

// parityRoutes are the bespoke adapters and the bundled manifests run by the
// generic process transport, each selecting the tools on PATH. Arguments
// are added to ShellCheck's command line.
func parityRoutes(shellcheck, ruff string, arguments []string) map[string][]tools.Registration {
	var options *processconfig.Options
	if len(arguments) != 0 {
		options = &processconfig.Options{Arguments: arguments}
	}
	return map[string][]tools.Registration{
		"adapters":  (adapters.Options{ShellCheck: shellcheck, ShellcheckOptions: options, Ruff: ruff}).Registrations(),
		"manifests": registry.Registrations(registry.Selections{"shellcheck": {Command: shellcheck, Process: options}, "ruff": {Command: ruff}}),
	}
}

// parityDiagnostics are a route's tool findings and its error, in a stable
// order and form for comparison.
type parityDiagnostics struct {
	Findings []diagnostic.Diagnostic
	Error    string
}

func toolFindings(result *application.Result, err error) parityDiagnostics {
	var out parityDiagnostics
	if err != nil {
		out.Error = err.Error()
	}
	if result == nil {
		return out
	}
	for _, d := range result.Diagnostics {
		if d.Rule == "shellcheck" || d.Rule == "ruff" {
			out.Findings = append(out.Findings, d)
		}
	}
	slices.SortStableFunc(out.Findings, func(a, b diagnostic.Diagnostic) int {
		return strings.Compare(a.Path+"\x00"+a.Message, b.Path+"\x00"+b.Message)
	})
	return out
}

// assertParity fails when the manifest route reports anything the adapters
// do not, or the reverse.
func assertParity(t *testing.T, run func([]tools.Registration) parityDiagnostics, shellcheck, ruff string, arguments ...string) {
	t.Helper()
	routes := parityRoutes(shellcheck, ruff, arguments)
	want := run(routes["adapters"])
	got := run(routes["manifests"])
	// Manifests map tool severities to host severities; the adapters passed
	// ShellCheck's style through for renderers to interpret.
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for i, finding := range want.Findings {
		for _, manifest := range manifests {
			if manifest.Rule.Name == finding.Rule {
				severity, _ := manifest.Capabilities.HostSeverity(finding.Severity)
				want.Findings[i].Severity = string(severity)
			}
		}
	}
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("manifest route differs from the adapters\nadapters:\n%s\nmanifests:\n%s", wantJSON, gotJSON)
	}
	t.Logf("%d identical findings, error %q", len(got.Findings), got.Error)
}

// TestProviderParityFixtures runs every workflow fixture through both
// routes.
func TestProviderParityFixtures(t *testing.T) {
	shellcheck, ruff := testutil.RequireShellcheck(t), testutil.RequireRuff(t)
	for _, subdir := range []string{"examples", "err", "ok"} {
		dir := filepath.Join("testdata", subdir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		project := workspace.Identity(dir)
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}
			path := filepath.Join(dir, name)
			t.Run(subdir+"/"+name, func(t *testing.T) {
				assertParity(t, func(registrations []tools.Registration) parityDiagnostics {
					sources := testutil.FixtureSources(t, []string{path}, project, &repoconfig.Config{})
					return toolFindings(application.Analyze(t.Context(), application.Options{Tools: registrations}, sources))
				}, shellcheck, ruff)
			})
		}
	}
}

// TestProviderParityCases runs repositories that exercise configuration,
// composite actions, sourced files, fixes and suppression through both
// routes.
func TestProviderParityCases(t *testing.T) {
	shellcheck, ruff := testutil.RequireShellcheck(t), testutil.RequireRuff(t)
	for _, tc := range []struct {
		name, config, steps string
		files               map[string]string
		// arguments are added to ShellCheck's command line.
		arguments []string
	}{
		{
			name:  "ruff findings and templates",
			steps: "- run: |\n    import os, sys\n    print(${{ github.event.number }}, missing)\n    x = 'a very long line that keeps going and going and going past the limit of characters'",
		},
		{
			name:   "ruff selection and target version",
			config: "tools: {ruff: {select: [F, E501], ignore: [F401], target-version: py38}}\n",
			steps:  "- shell: python3.12\n  run: |\n    import os\n    print(missing)\n    match x:\n        case 1: pass",
		},
		{
			name:  "ruff syntax error",
			steps: "- run: |\n    def broken(:\n        pass",
		},
		{
			name:  "ruff composite action",
			steps: "- uses: ./python-action",
			files: map[string]string{"python-action/action.yml": "name: Python\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: python\n      run: |\n        import os\n        print(missing)\n"},
		},
		{
			name:  "shellcheck findings, fixes and startup",
			steps: "- shell: bash\n  run: |\n    echo $1\n    cd /tmp\n    ls *.txt | grep x\n    [ $X == 1 ] && echo ${{ github.sha }}",
		},
		{
			name:  "shellcheck dialect directive and implicit shell",
			steps: "- shell: sh\n  run: |\n    # shellcheck shell=bash\n    echo ${ARR[@]}\n    [[ -n $Y ]]\n- run: echo $Z",
		},
		{
			name:   "shellcheck inline directives",
			config: "tools: {shellcheck: {config: {disable: [SC2086], enable: all, shell: dash}}}\n",
			steps:  "- shell: bash\n  run: |\n    echo $1\n    local x=1\n    test -n \"$(echo $Y)\"",
		},
		{
			name:   "shellcheck configuration file",
			config: "tools: {shellcheck: {config: shellcheckrc}}\n",
			steps:  "- shell: bash\n  run: |\n    echo $1\n    echo `date`",
			files:  map[string]string{".github/shellcheckrc": "disable=SC2006\n"},
		},
		{
			name:   "shellcheck invalid directive",
			config: "tools: {shellcheck: {config: {enable: [no-such-check]}}}\n",
			steps:  "- shell: bash\n  run: echo $1",
		},
		{
			name:  "shellcheck sourced file",
			steps: "- uses: ./local",
			files: map[string]string{
				"app/lib/check.sh": "echo $VALUE\n",
				"local/action.yml": "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: app\n      run: . ./lib/check.sh\n",
			},
			arguments: []string{"--check-sourced"},
		},
		{
			name:  "shellcheck sourced file suppressed at the script",
			steps: "- uses: ./local",
			files: map[string]string{
				"app/lib/check.sh": "echo $VALUE\n",
				"local/action.yml": "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: app\n      run: . ./lib/check.sh # actionlint:ignore shellcheck -- sourced helper\n",
			},
			arguments: []string{"--check-sourced"},
		},
		{
			name:   "tools disabled by override",
			config: "overrides:\n  - includes: ['local/**']\n    tools: {shellcheck: false}\n",
			steps:  "- uses: ./local\n- shell: bash\n  run: echo $1",
			files:  map[string]string{"local/action.yml": "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo $2\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertParity(t, func(registrations []tools.Registration) parityDiagnostics {
				root, _ := testutil.ExecutableFixture(t)
				config := "tools: {shellcheck: true, ruff: true}\n"
				if tc.config != "" {
					config = tc.config
				}
				testutil.WriteFixture(t, root, ".github/actionlint.yaml", config)
				for path, content := range tc.files {
					testutil.WriteFixture(t, root, path, content)
				}
				if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
					t.Fatal(err)
				}
				workflow := testutil.WriteFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: python\n    steps:\n      "+strings.ReplaceAll(tc.steps, "\n", "\n      ")+"\n")
				result, err := application.Check(t.Context(), application.Options{WorkingDir: root, Tools: registrations}, application.Input{Kind: application.Files, Paths: []string{workflow}})
				// Each route runs in its own repository.
				encoded, marshalErr := json.Marshal(toolFindings(result, err))
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				text := string(encoded)
				for _, spelling := range []string{root, filepath.ToSlash(root)} {
					quoted, _ := json.Marshal(spelling)
					text = strings.ReplaceAll(text, strings.Trim(string(quoted), `"`), "<root>")
				}
				var findings parityDiagnostics
				if err := json.Unmarshal([]byte(text), &findings); err != nil {
					t.Fatal(err)
				}
				for _, finding := range findings.Findings {
					if finding.Reference != nil {
						// Node identities derive from the repository path.
						finding.Reference.Node = ""
					}
				}
				return findings
			}, shellcheck, ruff, tc.arguments...)
		})
	}
}

// TestProviderParityEmbeddedRunner checks that an embedding program's
// in-process runner receives the same command line on both routes.
func TestProviderParityEmbeddedRunner(t *testing.T) {
	var calls [2][]string
	for _, registrations := range [][]tools.Registration{
		(adapters.Options{ShellcheckRunner: func(args []string, _ string) ([]byte, error) {
			calls[0] = args
			return []byte(`{"comments":[]}`), nil
		}}).Registrations(),
		registry.Registrations(registry.Selections{"shellcheck": backend.Selection{Runner: func(args []string, _ string) ([]byte, error) {
			calls[1] = args
			return []byte(`{"comments":[]}`), nil
		}}}),
	} {
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: echo $1\n"
		if _, err := application.Analyze(t.Context(), application.Options{WorkingDir: "/", Tools: registrations}, []workspace.Source{{Path: "test.yaml", Content: []byte(source)}}); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(calls[0], calls[1]) {
		t.Fatalf("embedded command lines differ:\nadapters:  %q\nmanifests: %q", calls[0], calls[1])
	}
}
