package plugins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"

	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/checkinputs"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"actionlint.kjanat.dev/v2/internal/results"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func loadRunnerFixture(t *testing.T) *component.Plugin {
	t.Helper()
	p, err := component.LoadComponentPlugin(t.Context(), "../../../../../internal/plugins/pluginexecution/component/testdata/plugins/buildkite-runners.wasm", component.Options{ReservedRules: checking.BuiltinRuleNames()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if p.HasAnalyzer() || !p.HasRunnerProvider() {
		t.Fatal("provider-only component advertised wrong roles")
	}
	return p
}

func runnerFixtureRequest(t *testing.T) runnerapi.Request {
	t.Helper()
	data, err := os.ReadFile("../../../../../internal/model/runners/testdata/provider/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	r := runnerapi.Request{Target: "buildkite", Scope: "fixture", Snapshot: runnerapi.RunnerSnapshot{Content: string(data), Scope: "fixture", ObtainedAt: "2026-10-19T00:00:00Z", Freshness: runnerapi.Fresh, Complete: true}}
	for _, term := range []runnerapi.Term{runnerapi.Literal("buildkite-windows"), runnerapi.Literal("missing-queue"), runnerapi.Expression("${{ vars.RUNNER }}")} {
		r.Selectors = append(r.Selectors, runnerapi.Selector{Labels: []runnerapi.Constraint{{Value: term, Position: sourcetext.Position{Line: 4, Column: 14}}}})
	}
	return r
}

func TestRunnerProviderComponent(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	response, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Answers) != 3 {
		t.Fatal(response.Answers)
	}
	resolved, ok := response.Answers[0].Resolution.(runnerapi.Resolved)
	if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].OS != runnerapi.Windows || resolved.Candidates[0].DefaultShell != "pwsh" {
		t.Fatalf("%#v", response.Answers[0])
	}
	if _, ok := response.Answers[1].Resolution.(runnerapi.NoMatch); !ok {
		t.Fatalf("%#v", response.Answers[1])
	}
	if _, ok := response.Answers[2].Resolution.(runnerapi.Unknown); !ok {
		t.Fatalf("%#v", response.Answers[2])
	}
	request.Snapshot.Freshness = runnerapi.Stale
	response, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.Answers[0].Resolution.(runnerapi.Unknown); !ok {
		t.Fatal("stale evidence resolved a runner")
	}
	request.Target = "github"
	response, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.Answers[0].Resolution.(runnerapi.NotApplicable); !ok {
		t.Fatal("provider took authority over another target")
	}
	request.Snapshot.Content = "{"
	if _, err := p.ResolveRunners(t.Context(), request); err == nil {
		t.Fatal("malformed snapshot succeeded")
	}
}

type runtimeObserver struct{ scripts []checkinputs.Script }

func (*runtimeObserver) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "runtime-observer", Category: "correctness", RequiredInputs: []rule.InputKind{rule.Execution}}
}

func (o *runtimeObserver) AnalyzeDocument(_ context.Context, input checkinputs.AnalyzerInput) ([]results.Diagnostic, error) {
	o.scripts = append(o.scripts, input.Scripts...)
	return nil, nil
}

func TestRunnerProviderSharedExecutionFacts(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	for _, tc := range []struct {
		name, shell string
		snapshot    runnerapi.RunnerSnapshot
		wantShell   string
	}{
		{"resolved", "", runnerFixtureRequest(t).Snapshot, "pwsh"},
		{"missing", "", runnerapi.RunnerSnapshot{}, ""},
		{"explicit shell survives missing evidence", "shell: bash\n        ", runnerapi.RunnerSnapshot{}, "bash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observer := &runtimeObserver{}
			result, err := checking.Analyze(t.Context(), checking.Options{
				Runners:   runnerapi.Options{Target: "buildkite", Scope: "fixture", Snapshot: tc.snapshot, Providers: []runnerapi.Registration{{Provider: p, ImplementationID: p.ArtifactDigest()}}},
				Analyzers: []results.AnalyzerRegistration{{Analyzer: observer}},
			}, []workspace.InputFile{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: buildkite-windows\n    steps:\n      - " + tc.shell + "run: echo hello\n")}})
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range result.Diagnostics {
				if d.Rule == "runner-label" {
					t.Fatal(d)
				}
			}
			if len(observer.scripts) != 1 || observer.scripts[0].Runtime == nil {
				t.Fatalf("missing shared execution facts: %#v", observer.scripts)
			}
			runtime := observer.scripts[0].Runtime
			if runtime.Shell.Name != tc.wantShell || runtime.Shell.Known() != (tc.wantShell != "") {
				t.Fatalf("runtime = %#v", runtime)
			}
			if len(result.RunnerResults) != 1 || result.RunnerResults[0].ImplementationID != p.ArtifactDigest() {
				t.Fatal("missing provider provenance")
			}
		})
	}
}

func TestBuiltinCustomWindowsSharedExecutionFacts(t *testing.T) {
	t.Parallel()
	observer := &runtimeObserver{}
	label := "self-32vcpu-windows-2022"
	cfg, err := (configuration.RuleSet{}).ParseConfig([]byte("repository: {runners: [" + label + "]}"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []results.AnalyzerRegistration{{Analyzer: observer}}}, []workspace.InputFile{{
			Path:    "workflow.yml",
			Content: []byte("on: push\njobs:\n  build:\n    runs-on: " + label + "\n    steps:\n      - run: Write-Host 'hello'\n"),
			Config:  cfg,
		}})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range result.Diagnostics {
		if d.Rule == "runner-label" || d.Rule == "shell-name" {
			t.Fatal(d)
		}
	}
	if len(observer.scripts) != 1 || observer.scripts[0].Runtime == nil {
		t.Fatalf("missing shared execution facts: %#v", observer.scripts)
	}
	if runtime := observer.scripts[0].Runtime; runtime.Shell.Name != "pwsh" || !runtime.Shell.Known() {
		t.Fatalf("Windows runner lost its default shell: %#v", runtime)
	}
}

type parityAnswer struct {
	Kind          string
	Candidates    []runnerapi.Candidate
	Rejections    []runnerapi.Rejection
	Reason        string
	DatasetStatus runnerapi.DatasetStatus
}
type parityOutput struct {
	Digest, Name, Version string
	Targets               []string
	Answers               []parityAnswer
}

func TestRunnerProviderWasmtimeParity(t *testing.T) {
	t.Parallel()
	host := os.Getenv("ACTIONLINT_RUNNER_HOST")
	if host == "" {
		t.Skip("set ACTIONLINT_RUNNER_HOST to the built internal/development/tests/conformance/wasm/runner-host executable")
	}
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	response, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	d := p.RunnerDescriptor()
	want := parityOutput{Digest: p.ArtifactDigest(), Name: d.Name, Version: d.Version, Targets: d.Targets}
	for _, a := range response.Answers {
		v := parityAnswer{DatasetStatus: a.DatasetStatus}
		switch r := a.Resolution.(type) {
		case runnerapi.Resolved:
			v.Kind, v.Candidates = "resolved", r.Candidates
		case runnerapi.NoMatch:
			v.Kind, v.Rejections = "no-match", r.Rejections
		case runnerapi.Unknown:
			v.Kind, v.Reason = "unknown", r.Reason
		case runnerapi.NotApplicable:
			v.Kind = "not-applicable"
		}
		want.Answers = append(want.Answers, v)
	}
	artifact, err := filepath.Abs("../../../../../internal/plugins/pluginexecution/component/testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := filepath.Abs("../../../../../internal/model/runners/testdata/provider/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := exec.CommandContext(t.Context(), host, artifact, snapshot).CombinedOutput()
	if err != nil {
		t.Fatalf("Wasmtime: %v\n%s", err, data)
	}
	var got parityOutput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatal(diff)
	}
	if strings.TrimSpace(got.Digest) == "" {
		t.Fatal("parity did not identify artifact bytes")
	}
}
