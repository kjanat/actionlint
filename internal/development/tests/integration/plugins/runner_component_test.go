package plugins_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	componenthost "actionlint.kjanat.dev/v2/internal/plugins/execution/component"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/results"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func loadRunnerFixture(t *testing.T) *componenthost.Plugin {
	t.Helper()
	p, err := componenthost.Load(t.Context(), "../../../../../internal/plugins/execution/component/testdata/plugins/buildkite-runners.wasm", componenthost.Options{ReservedRules: checking.BuiltinRuleNames()})
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

func runnerFixtureRequest(t *testing.T) runner.Request {
	t.Helper()
	data, err := os.ReadFile("../../../../../internal/model/runners/testdata/provider/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	r := runner.Request{Target: "buildkite", Scope: "fixture", Snapshot: runner.Snapshot{Content: string(data), Scope: "fixture", ObtainedAt: "2026-10-19T00:00:00Z", Freshness: runner.Fresh, Complete: true}}
	for _, term := range []runner.Term{runner.Literal("buildkite-windows"), runner.Literal("missing-queue"), runner.Expression("${{ vars.RUNNER }}")} {
		r.Selectors = append(r.Selectors, runner.Selector{Labels: []runner.Constraint{{Value: term, Position: sourcepos.Position{Line: 4, Column: 14}}}})
	}
	return r
}

func TestRunnerProviderComponent(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	request := runnerFixtureRequest(t)
	answers, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 3 {
		t.Fatal(answers)
	}
	resolved, ok := answers[0].Resolution.(runner.Resolved)
	if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].OS != runner.Windows || resolved.Candidates[0].DefaultShell != "pwsh" {
		t.Fatalf("%#v", answers[0])
	}
	if _, ok := answers[1].Resolution.(runner.NoMatch); !ok {
		t.Fatalf("%#v", answers[1])
	}
	if _, ok := answers[2].Resolution.(runner.Unknown); !ok {
		t.Fatalf("%#v", answers[2])
	}
	request.Snapshot.Freshness = runner.Stale
	answers, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := answers[0].Resolution.(runner.Unknown); !ok {
		t.Fatal("stale evidence resolved a runner")
	}
	request.Target = "github"
	answers, err = p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := answers[0].Resolution.(runner.NotApplicable); !ok {
		t.Fatal("provider took authority over another target")
	}
	request.Snapshot.Content = "{"
	if _, err := p.ResolveRunners(t.Context(), request); err == nil {
		t.Fatal("malformed snapshot succeeded")
	}
}

type runtimeObserver struct{ scripts []checkinputs.Script }

func (*runtimeObserver) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "runtime-observer", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Execution}}
}

func (o *runtimeObserver) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	o.scripts = append(o.scripts, input.Scripts...)
	return nil, nil
}

func TestRunnerProviderSharedExecutionFacts(t *testing.T) {
	t.Parallel()
	p := loadRunnerFixture(t)
	for _, tc := range []struct {
		name, shell string
		snapshot    runner.Snapshot
		wantShell   string
	}{
		{"resolved", "", runnerFixtureRequest(t).Snapshot, "pwsh"},
		{"missing", "", runner.Snapshot{}, ""},
		{"explicit shell survives missing evidence", "shell: bash\n        ", runner.Snapshot{}, "bash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observer := &runtimeObserver{}
			result, err := checking.Analyze(t.Context(), checking.Options{
				Runners:   runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: tc.snapshot, Providers: []runner.Registration{{Provider: p, ImplementationID: p.ArtifactDigest()}}},
				Analyzers: []checkinputs.Registration{{Checker: observer}},
			}, []workspace.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: buildkite-windows\n    steps:\n      - " + tc.shell + "run: echo hello\n")}})
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
			if runtime.Shell != tc.wantShell || runtime.ShellKnown != (tc.wantShell != "") {
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
	cfg, err := (config.RuleSet{}).Parse([]byte("repository: {runners: [" + label + "]}"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: observer}}}, []workspace.Source{{
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
	if runtime := observer.scripts[0].Runtime; runtime.Shell != "pwsh" || !runtime.ShellKnown {
		t.Fatalf("Windows runner lost its default shell: %#v", runtime)
	}
}

type parityAnswer struct {
	Kind          string
	Candidates    []runner.Candidate
	Rejections    []runner.Rejection
	Reason        string
	DatasetStatus runner.DatasetStatus
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
	answers, err := p.ResolveRunners(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	d := p.RunnerDescriptor()
	want := parityOutput{Digest: p.ArtifactDigest(), Name: d.Name, Version: d.Version, Targets: d.Targets}
	for _, a := range answers {
		v := parityAnswer{DatasetStatus: a.DatasetStatus}
		switch r := a.Resolution.(type) {
		case runner.Resolved:
			v.Kind, v.Candidates = "resolved", r.Candidates
		case runner.NoMatch:
			v.Kind, v.Rejections = "no-match", r.Rejections
		case runner.Unknown:
			v.Kind, v.Reason = "unknown", r.Reason
		case runner.NotApplicable:
			v.Kind = "not-applicable"
		}
		want.Answers = append(want.Answers, v)
	}
	artifact, err := filepath.Abs("../../../../../internal/plugins/execution/component/testdata/plugins/buildkite-runners.wasm")
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
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatal(diff)
	}
	if strings.TrimSpace(got.Digest) == "" {
		t.Fatal("parity did not identify artifact bytes")
	}
}
