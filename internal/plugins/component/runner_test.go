package component

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/runner"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func loadRunnerFixture(t *testing.T) *Plugin {
	t.Helper()
	p, err := Load(t.Context(), "testdata/plugins/buildkite-runners.wasm", Options{})
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
	data, err := os.ReadFile("../../../testdata/runner-provider/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	r := runner.Request{Target: "buildkite", Scope: "fixture", Snapshot: runner.Snapshot{Content: string(data), Scope: "fixture", ObtainedAt: "2026-10-19T00:00:00Z", Freshness: runner.Fresh, Complete: true}}
	for _, term := range []runner.Term{runner.Literal("buildkite-windows"), runner.Literal("missing-queue"), runner.Expression("${{ vars.RUNNER }}")} {
		r.Selectors = append(r.Selectors, runner.Selector{Labels: []runner.Constraint{{Value: term, Position: diagnostic.Position{Line: 4, Column: 14}}}})
	}
	return r
}

func TestRunnerProviderComponent(t *testing.T) {
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

type runtimeObserver struct{ scripts []analyzer.Script }

func (*runtimeObserver) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "runtime-observer", Requires: []analyzer.Fact{analyzer.Execution}}
}

func (o *runtimeObserver) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	o.scripts = append(o.scripts, input.Scripts...)
	return nil, nil
}

func TestRunnerProviderSharedExecutionFacts(t *testing.T) {
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
			observer := &runtimeObserver{}
			result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte("on: push\njobs:\n  build:\n    runs-on: buildkite-windows\n    steps:\n      - " + tc.shell + "run: echo hello\n")}}, Options: analysis.Options{
				Runners:   runner.Options{Target: "buildkite", Scope: "fixture", Snapshot: tc.snapshot, Providers: []runner.Registration{{Provider: p, Identity: p.ArtifactDigest()}}},
				Analyzers: []analyzer.Registration{{Analyzer: observer}},
			}})
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
			if len(result.RunnerEvidence) != 1 || result.RunnerEvidence[0].Artifact != p.ArtifactDigest() {
				t.Fatal("missing provider provenance")
			}
		})
	}
}

func TestBuiltinCustomWindowsSharedExecutionFacts(t *testing.T) {
	observer := &runtimeObserver{}
	label := "self-32vcpu-windows-2022"
	result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{{
		Path:    "workflow.yml",
		Content: []byte("on: push\njobs:\n  build:\n    runs-on: " + label + "\n    steps:\n      - run: Write-Host 'hello'\n"),
		Config:  &config.Config{SelfHostedRunner: config.SelfHostedRunnerConfig{Labels: []string{label}}},
	}}, Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: observer}}}})
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

func TestRunnerContractShapeAndDigest(t *testing.T) {
	data, err := os.ReadFile("../../../plugins/wit/runner.wit")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))) != runnerContractDigest {
		t.Fatal("stale runner contract; regenerate")
	}
	data, err = os.ReadFile("testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRoleContracts(data, false, true); err != nil {
		t.Fatal(err)
	}
	altered := bytes.ReplaceAll(data, []byte("case-sensitivity"), []byte("case-sensitivitx"))
	if bytes.Equal(data, altered) {
		t.Fatal("fixture contains no candidate field")
	}
	if err := checkRoleContracts(altered, false, true); err == nil {
		t.Fatal("changed nested provider contract accepted")
	}
}

type parityAnswer struct {
	Kind       string
	Candidates []runner.Candidate
	Rejections []runner.Rejection
	Reason     string
	Evidence   runner.Evidence
}
type parityOutput struct {
	Digest, Name, Version string
	Targets               []string
	Answers               []parityAnswer
}

func TestRunnerProviderWasmtimeParity(t *testing.T) {
	host := os.Getenv("ACTIONLINT_RUNNER_HOST")
	if host == "" {
		t.Skip("set ACTIONLINT_RUNNER_HOST to the built tools/runner-host executable")
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
		v := parityAnswer{Evidence: a.Evidence}
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
	artifact, err := filepath.Abs("testdata/plugins/buildkite-runners.wasm")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := filepath.Abs("../../../testdata/runner-provider/snapshot.json")
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
