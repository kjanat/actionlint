package tools_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

// The parity gate compares the bundled manifests, run by the generic process
// transport, with snapshots captured from the bespoke ShellCheck and Ruff
// adapters they replace. ACTIONLINT_CAPTURE_PARITY=1 captures the snapshots
// from the adapters again.

// parityDiagnostics are a route's tool findings and its error, in a stable,
// machine-independent form.
type parityDiagnostics struct {
	Findings []diagnostic.Diagnostic `json:"findings"`
	Error    string                  `json:"error,omitempty"`
}

// paritySnapshots are captured parity results keyed by case.
type paritySnapshots struct {
	path    string
	capture bool
	mu      sync.Mutex
	values  map[string]parityDiagnostics
}

// loadParitySnapshots reads the snapshots of one test, or prepares to
// capture them, writing them when the test finishes.
func loadParitySnapshots(t *testing.T, name string) *paritySnapshots {
	t.Helper()
	s := &paritySnapshots{path: filepath.Join("internal", "integration", "tools", "testdata", "provider-parity", name+".json"), capture: os.Getenv("ACTIONLINT_CAPTURE_PARITY") == "1", values: map[string]parityDiagnostics{}}
	if s.capture {
		t.Cleanup(func() {
			data, err := json.MarshalIndent(s.values, "", "  ")
			if err == nil {
				err = os.WriteFile(s.path, append(data, '\n'), 0o600)
			}
			if err != nil {
				t.Error(err)
			}
		})
		return s
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &s.values); err != nil {
		t.Fatal(err)
	}
	return s
}

// selections select ShellCheck and Ruff on PATH; arguments are added to
// ShellCheck's command line.
func paritySelections(shellcheck, ruff string, arguments []string) (registry.Selections, adapters.Options) {
	var options *processconfig.Options
	if len(arguments) != 0 {
		options = &processconfig.Options{Arguments: arguments}
	}
	return registry.Selections{"shellcheck": {Command: shellcheck, Process: options}, "ruff": {Command: ruff}},
		adapters.Options{ShellCheck: shellcheck, ShellcheckOptions: options, Ruff: ruff}
}

// assertParity checks the manifest route against the snapshot of key.
func (s *paritySnapshots) assertParity(t *testing.T, key string, run func([]tools.Registration) parityDiagnostics, shellcheck, ruff string, arguments ...string) {
	t.Helper()
	selections, adapted := paritySelections(shellcheck, ruff, arguments)
	got := run(registry.Registrations(selections))
	if s.capture {
		want := run(adapted.Registrations())
		// Manifests map tool severities to host severities; the adapters
		// passed ShellCheck's style through for renderers to interpret.
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
		s.mu.Lock()
		s.values[key] = want
		s.mu.Unlock()
	}
	want, ok := s.values[key]
	if !ok {
		t.Fatalf("no parity snapshot for %q; capture with ACTIONLINT_CAPTURE_PARITY=1", key)
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
// program's in-process runner receives, as the adapters built it.
func TestProviderParityEmbeddedRunner(t *testing.T) {
	var calls [2][]string
	for i, registrations := range [][]tools.Registration{
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
		t.Logf("route %d: %q", i, calls[i])
	}
	if !slices.Equal(calls[0], calls[1]) {
		t.Fatalf("embedded command lines differ:\nadapters:  %q\nmanifests: %q", calls[0], calls[1])
	}
}
