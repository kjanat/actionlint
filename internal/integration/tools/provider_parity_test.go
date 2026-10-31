package tools_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"actionlint.kjanat.dev/v2/internal/tools/backend"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/source"
)

// The parity gate compares the bundled manifests, run by the generic process
// transport, with snapshots captured from the bespoke ShellCheck and Ruff
// adapters at commit 6be5c7ab, the last commit that has them, with
// ShellCheck 0.11.0 and Ruff 0.17.0. The snapshots hold the adapters' raw
// output: ShellCheck's severities such as "style" are mapped through the
// manifest's severities table when compared, since the adapters left that
// mapping to renderers. A deliberate change in findings updates the snapshot
// by hand.

// parityDiagnostics are a route's tool findings and its error, in a stable,
// machine-independent form.
type parityDiagnostics struct {
	Findings []diagnostic.Diagnostic `json:"findings"`
	// Sources are the result's source evidence for Findings, in the same
	// order.
	Sources []paritySource `json:"sources,omitempty"`
	// Reported are the findings the tool runners returned to the host,
	// before suppression and policy.
	Reported []parityReport `json:"reported,omitempty"`
	// Captured are the result's captured sources keyed by alias.
	Captured map[string]parityCapture `json:"captured,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

// paritySource is a report.DiagnosticSource with its content hashed.
type paritySource struct {
	Content     string `json:"content,omitempty"`
	RawScript   bool   `json:"raw_script,omitempty"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

// parityReport is the provenance of one runner finding: where the tool
// reported it, the file content it carries, the unavailable-source marker and
// the location whose actionlint:ignore comments suppress it.
type parityReport struct {
	Rule        string              `json:"rule"`
	Code        string              `json:"code,omitempty"`
	Path        string              `json:"path"`
	Start       diagnostic.Position `json:"start"`
	Content     string              `json:"content,omitempty"`
	RawScript   bool                `json:"raw_script,omitempty"`
	Unavailable *parityUnavailable  `json:"unavailable,omitempty"`
	Suppression *paritySuppression  `json:"suppression,omitempty"`
}

// parityUnavailable is a present finding.UnavailableSource.
type parityUnavailable struct {
	Reason  string `json:"reason"`
	Address string `json:"address"`
}

// paritySuppression is a tools.Suppression.
type paritySuppression struct {
	Path        string `json:"path"`
	First, Last int
}

// parityCapture is a report.CapturedSource without its content, which the
// revision identifies.
type parityCapture struct {
	Revision             source.Revision    `json:"revision"`
	Coordinates          source.Coordinates `json:"coordinates"`
	CoordinatesAvailable bool               `json:"coordinates_available,omitempty"`
}

// contentHash identifies content in a snapshot; empty content has none.
func contentHash(content []byte) string {
	if content == nil {
		return ""
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// parityRecorder collects the findings the tool runners return to the host.
type parityRecorder struct {
	mu       sync.Mutex
	findings []tools.Finding
}

// wrap returns registrations whose runners record their findings in r.
func (r *parityRecorder) wrap(registrations []tools.Registration) []tools.Registration {
	out := slices.Clone(registrations)
	for i := range out {
		build := out[i].Build
		out[i].Build = func(c tools.Context) (tools.Runner, error) {
			runner, err := build(c)
			if err != nil || runner == nil {
				return runner, err
			}
			return recordingRunner{runner: runner, recorder: r}, nil
		}
	}
	return out
}

// recordingRunner passes a batch to runner and records the findings.
type recordingRunner struct {
	runner   tools.Runner
	recorder *parityRecorder
}

func (r recordingRunner) Analyze(ctx context.Context, batch tools.Batch) ([]tools.Finding, error) {
	findings, err := r.runner.Analyze(ctx, batch)
	r.recorder.mu.Lock()
	r.recorder.findings = append(r.recorder.findings, findings...)
	r.recorder.mu.Unlock()
	return findings, err
}

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

// paritySnapshots are captured parity results keyed by case.
type paritySnapshots map[string]parityDiagnostics

// parityRun analyzes a case with registrations and returns its tool
// findings, recording the runners' findings in recorder.
type parityRun func(registrations []tools.Registration, recorder *parityRecorder) parityDiagnostics

// assertParity runs the bundled providers with ShellCheck and Ruff from PATH
// and compares their findings with the snapshot of key. arguments are added
// to ShellCheck's command line.
func (s paritySnapshots) assertParity(t *testing.T, key string, run parityRun, shellcheck, ruff string, arguments ...string) {
	t.Helper()
	want, ok := s[key]
	if !ok {
		t.Fatalf("no parity snapshot for %q", key)
	}
	want = hostSeverities(t, want)
	var options *processconfig.Options
	if len(arguments) != 0 {
		options = &processconfig.Options{Arguments: arguments}
	}
	got := run(registry.Registrations(registry.Selections{"shellcheck": {Command: shellcheck, Process: options}, "ruff": {Command: ruff}}), &parityRecorder{})
	compareParity(t, want, got)
	t.Logf("%d identical findings, %d runner findings, error %q", len(got.Findings), len(got.Reported), got.Error)
}

// hostSeverities maps the snapshot's tool severities through each bundled
// manifest's severities table, as the manifest route does.
func hostSeverities(t *testing.T, snapshot parityDiagnostics) parityDiagnostics {
	t.Helper()
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Findings = slices.Clone(snapshot.Findings)
	for i, finding := range snapshot.Findings {
		for _, manifest := range manifests {
			if manifest.Rule.Name == finding.Rule {
				severity, _ := manifest.Capabilities.HostSeverity(finding.Severity)
				snapshot.Findings[i].Severity = string(severity)
			}
		}
	}
	return snapshot
}

// compareParity fails t when got differs from want.
func compareParity(t *testing.T, want, got parityDiagnostics) {
	t.Helper()
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("manifest route differs from the adapters' snapshot\nadapters:\n%s\nmanifests:\n%s", wantJSON, gotJSON)
	}
}

// toolFindings selects a result's tool findings, their source evidence, the
// runners' findings and the captured sources in a stable order and replaces
// the machine's directories, named by roots, with placeholders.
func toolFindings(t *testing.T, result *application.Result, err error, recorder *parityRecorder, roots map[string]string) parityDiagnostics {
	t.Helper()
	var out parityDiagnostics
	if err != nil {
		out.Error = err.Error()
	}
	if result != nil {
		for i, d := range result.Diagnostics {
			if d.Rule == "shellcheck" || d.Rule == "ruff" {
				out.Findings = append(out.Findings, d)
				if i < len(result.Sources) {
					evidence := result.Sources[i]
					out.Sources = append(out.Sources, paritySource{Content: contentHash(evidence.Content), RawScript: evidence.RawScript, Unavailable: evidence.Unavailable})
				}
			}
		}
		for alias, captured := range result.CapturedSources {
			if out.Captured == nil {
				out.Captured = map[string]parityCapture{}
			}
			out.Captured[alias] = parityCapture{Revision: captured.Revision, Coordinates: captured.Coordinates, CoordinatesAvailable: captured.CoordinatesAvailable}
		}
	}
	recorder.mu.Lock()
	for _, finding := range recorder.findings {
		if finding.UnavailableSource.Present() && finding.UnavailableSource.Provider() != finding.Rule {
			t.Errorf("unavailable source of a %s finding names provider %q", finding.Rule, finding.UnavailableSource.Provider())
		}
		report := parityReport{Rule: finding.Rule, Code: finding.Code, Path: finding.Path, Start: finding.Start, Content: contentHash(finding.Content), RawScript: finding.RawScript}
		if finding.UnavailableSource.Present() {
			report.Unavailable = &parityUnavailable{Reason: finding.UnavailableSource.Reason(), Address: finding.UnavailableSource.Address()}
		}
		if finding.Suppression != nil {
			report.Suppression = &paritySuppression{Path: finding.Suppression.Path, First: finding.Suppression.First, Last: finding.Suppression.Last}
		}
		out.Reported = append(out.Reported, report)
	}
	recorder.mu.Unlock()
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
	return normalizeParity(normalized)
}

// normalizeParity writes paths with forward slashes, drops node identities,
// which derive from the machine's paths, and sorts findings and runner
// findings.
func normalizeParity(snapshot parityDiagnostics) parityDiagnostics {
	type pair struct {
		finding diagnostic.Diagnostic
		source  *paritySource
	}
	pairs := make([]pair, len(snapshot.Findings))
	for i := range snapshot.Findings {
		finding := snapshot.Findings[i]
		finding.Path = filepath.ToSlash(finding.Path)
		if finding.Reference != nil {
			finding.Reference.Document = filepath.ToSlash(finding.Reference.Document)
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
		pairs[i].finding = finding
		if i < len(snapshot.Sources) {
			pairs[i].source = &snapshot.Sources[i]
		}
	}
	slices.SortStableFunc(pairs, func(a, b pair) int {
		return strings.Compare(a.finding.Path+"\x00"+a.finding.Message, b.finding.Path+"\x00"+b.finding.Message)
	})
	out := parityDiagnostics{Error: snapshot.Error}
	for _, p := range pairs {
		out.Findings = append(out.Findings, p.finding)
		if p.source != nil {
			out.Sources = append(out.Sources, *p.source)
		}
	}
	for _, report := range snapshot.Reported {
		report.Path = filepath.ToSlash(report.Path)
		if report.Unavailable != nil {
			report.Unavailable.Address = filepath.ToSlash(report.Unavailable.Address)
		}
		if report.Suppression != nil {
			report.Suppression.Path = filepath.ToSlash(report.Suppression.Path)
		}
		out.Reported = append(out.Reported, report)
	}
	slices.SortStableFunc(out.Reported, func(a, b parityReport) int {
		encodedA, _ := json.Marshal(a)
		encodedB, _ := json.Marshal(b)
		return bytes.Compare(encodedA, encodedB)
	})
	for alias, captured := range snapshot.Captured {
		if out.Captured == nil {
			out.Captured = map[string]parityCapture{}
		}
		out.Captured[filepath.ToSlash(alias)] = captured
	}
	return out
}

// TestProviderParityFixtures runs every workflow fixture through the
// manifest route.
func TestProviderParityFixtures(t *testing.T) {
	shellcheck, ruff := testutil.RequireTool(t, "shellcheck"), testutil.RequireTool(t, "ruff")
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
				snapshots.assertParity(t, subdir+"/"+name, func(registrations []tools.Registration, recorder *parityRecorder) parityDiagnostics {
					sources := testutil.FixtureSources(t, []string{path}, project, &repoconfig.Config{})
					result, err := application.Analyze(t.Context(), application.Options{Tools: recorder.wrap(registrations)}, sources)
					return toolFindings(t, result, err, recorder, map[string]string{repository: "<repository>"})
				}, shellcheck, ruff)
			})
		}
	}
}

// TestProviderParityCases runs repositories that exercise configuration,
// composite actions, sourced files, fixes and suppression through the
// manifest route. Every case runs again with only Ruff selected and is
// compared with the snapshot of the Ruff adapter run alone.
func TestProviderParityCases(t *testing.T) {
	shellcheck, ruff := testutil.RequireTool(t, "shellcheck"), testutil.RequireTool(t, "ruff")
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
			name:  "shellcheck sourced file suppressed at another line",
			steps: "- uses: ./local",
			files: map[string]string{
				"app/lib/check.sh": "echo $VALUE\n",
				"local/action.yml": "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      working-directory: app\n      run: |\n        echo ready # actionlint:ignore shellcheck -- not the source line\n        . ./lib/check.sh\n",
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
			run := func(registrations []tools.Registration, recorder *parityRecorder) parityDiagnostics {
				// Each route runs in its own repository.
				root, _ := testutil.ExecutableFixture(t, "shellcheck")
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
				result, err := application.Check(t.Context(), application.Options{WorkingDir: root, Tools: recorder.wrap(registrations)}, application.Input{Kind: application.Files, Paths: []string{workflow}})
				return toolFindings(t, result, err, recorder, map[string]string{root: "<root>"})
			}
			snapshots.assertParity(t, tc.name, run, shellcheck, ruff, tc.arguments...)
			// The Ruff adapter alone ran without execution facts; the
			// manifest route resolves them for Ruff's selectors.shells
			// (decision b4).
			alone, ok := snapshots[tc.name+" (ruff alone)"]
			if !ok {
				t.Fatalf("no Ruff-alone parity snapshot for %q", tc.name)
			}
			compareParity(t, hostSeverities(t, alone), run(registry.Registrations(registry.Selections{"ruff": {Command: ruff}}), &parityRecorder{}))
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
