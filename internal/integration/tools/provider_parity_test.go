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
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

// The parity gate compares the bundled manifests, run by the generic process
// transport, with snapshots captured from the bespoke ShellCheck and Ruff
// adapters before they were deleted. Severities in the snapshots are mapped
// through each manifest's severities table, as the adapters passed
// ShellCheck's style through for renderers to interpret. A deliberate change
// in findings updates the snapshot by hand.

// parityDiagnostics are a route's tool findings and its error, in a stable,
// machine-independent form.
type parityDiagnostics struct {
	Findings []diagnostic.Diagnostic `json:"findings"`
	Error    string                  `json:"error,omitempty"`
}

// paritySnapshots are captured parity results keyed by case.
type paritySnapshots map[string]parityDiagnostics

// loadParitySnapshots reads testdata/provider-parity/<name>.json.
func loadParitySnapshots(t *testing.T, name string) paritySnapshots {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("internal", "integration", "tools", "testdata", "provider-parity", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshots paritySnapshots
	if err := json.Unmarshal(data, &snapshots); err != nil {
		t.Fatal(err)
	}
	return snapshots
}

// assertParity runs the bundled providers with ShellCheck and Ruff from PATH
// and compares their findings with the snapshot of key. arguments are added
// to ShellCheck's command line.
func (s paritySnapshots) assertParity(t *testing.T, key string, run func([]tools.Registration) parityDiagnostics, shellcheck, ruff string, arguments ...string) {
	t.Helper()
	var options *processconfig.Options
	if len(arguments) != 0 {
		options = &processconfig.Options{Arguments: arguments}
	}
	got := run(registry.Registrations(registry.Selections{"shellcheck": {Command: shellcheck, Process: options}, "ruff": {Command: ruff}}))
	want, ok := s[key]
	if !ok {
		t.Fatalf("no parity snapshot for %q", key)
	}
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("manifest route differs from the adapters' snapshot\nadapters:\n%s\nmanifests:\n%s", wantJSON, gotJSON)
	}
	t.Logf("%d identical findings, error %q", len(got.Findings), got.Error)
}

// toolFindings selects a result's tool findings in a stable order and
// replaces the machine's directories, named by roots, with placeholders.
func toolFindings(t *testing.T, result *application.Result, err error, roots map[string]string) parityDiagnostics {
	t.Helper()
	var out parityDiagnostics
	if err != nil {
		out.Error = err.Error()
	}
	if result != nil {
		for _, d := range result.Diagnostics {
			if d.Rule == "shellcheck" || d.Rule == "ruff" {
				out.Findings = append(out.Findings, d)
			}
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for root, placeholder := range roots {
		for _, spelling := range []string{root, filepath.ToSlash(root)} {
			quoted, _ := json.Marshal(spelling)
			text = strings.ReplaceAll(text, strings.Trim(string(quoted), `"`), placeholder)
		}
	}
	var normalized parityDiagnostics
	if err := json.Unmarshal([]byte(text), &normalized); err != nil {
		t.Fatal(err)
	}
	for i := range normalized.Findings {
		finding := &normalized.Findings[i]
		finding.Path = filepath.ToSlash(finding.Path)
		if finding.Reference != nil {
			finding.Reference.Document = filepath.ToSlash(finding.Reference.Document)
			// Node identities derive from the machine's paths.
			finding.Reference.Node = ""
		}
		for j := range finding.Fixes {
			for k := range finding.Fixes[j].Edits {
				finding.Fixes[j].Edits[k].Path = filepath.ToSlash(finding.Fixes[j].Edits[k].Path)
			}
		}
		for j := range finding.Related {
			finding.Related[j].Path = filepath.ToSlash(finding.Related[j].Path)
		}
	}
	slices.SortStableFunc(normalized.Findings, func(a, b diagnostic.Diagnostic) int {
		return strings.Compare(a.Path+"\x00"+a.Message, b.Path+"\x00"+b.Message)
	})
	return normalized
}

// TestProviderParityFixtures runs every workflow fixture through the
// manifest route.
func TestProviderParityFixtures(t *testing.T) {
	shellcheck, ruff := testutil.RequireShellcheck(t), testutil.RequireRuff(t)
	snapshots := loadParitySnapshots(t, "fixtures")
	repository, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
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
				snapshots.assertParity(t, subdir+"/"+name, func(registrations []tools.Registration) parityDiagnostics {
					sources := testutil.FixtureSources(t, []string{path}, project, &repoconfig.Config{})
					result, err := application.Analyze(t.Context(), application.Options{Tools: registrations}, sources)
					return toolFindings(t, result, err, map[string]string{repository: "<repository>"})
				}, shellcheck, ruff)
			})
		}
	}
}

// TestProviderParityCases runs repositories that exercise configuration,
// composite actions, sourced files, fixes and suppression through the
// manifest route.
func TestProviderParityCases(t *testing.T) {
	shellcheck, ruff := testutil.RequireShellcheck(t), testutil.RequireRuff(t)
	snapshots := loadParitySnapshots(t, "cases")
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
			snapshots.assertParity(t, tc.name, func(registrations []tools.Registration) parityDiagnostics {
				// Each route runs in its own repository.
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
				return toolFindings(t, result, err, map[string]string{root: "<root>"})
			}, shellcheck, ruff, tc.arguments...)
		})
	}
}

// TestProviderParityEmbeddedRunner checks the command line an embedding
// program's in-process runner receives against the one the ShellCheck
// adapter built.
func TestProviderParityEmbeddedRunner(t *testing.T) {
	var args []string
	registrations := registry.Registrations(registry.Selections{"shellcheck": backend.Selection{Runner: func(received []string, _ string) ([]byte, error) {
		args = received
		return []byte(`{"comments":[]}`), nil
	}}})
	source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: echo $1\n"
	if _, err := application.Analyze(t.Context(), application.Options{WorkingDir: "/", Tools: registrations}, []workspace.Source{{Path: "test.yaml", Content: []byte(source)}}); err != nil {
		t.Fatal(err)
	}
	adapter := []string{"--norc", "-f", "json1", "--shell", "bash", "-e", "SC1091,SC2194,SC2050,SC2153,SC2154,SC2157,SC2043", "-"}
	if !slices.Equal(args, adapter) {
		t.Fatalf("embedded command line differs:\nadapter:   %q\nmanifests: %q", adapter, args)
	}
}
