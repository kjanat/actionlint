package expressions_test

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

type expressionShellProvider struct{ shell string }

func (expressionShellProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "expression-shell", Version: "1", Targets: []string{"github"}}
}

func (p expressionShellProvider) ResolveRunners(_ context.Context, request runnerapi.Request) (runnerapi.Response, error) {
	answers := make([]runnerapi.Answer, len(request.Selectors))
	for i := range answers {
		var resolution runnerapi.Resolution = runnerapi.Unknown{Reason: "shell unavailable"}
		if p.shell != "" {
			resolution = runnerapi.Resolved{Candidates: []runnerapi.Candidate{{Name: "test", OS: runnerapi.Linux, DefaultShell: p.shell}}}
		}
		answers[i] = runnerapi.Answer{Resolution: resolution, DatasetStatus: runnerapi.DatasetStatus{DatasetID: "test", Scope: request.Scope, Freshness: runnerapi.Fresh, Complete: true}}
	}
	return runnerapi.Response{Answers: answers}, nil
}

func TestScriptInterpolationUsesRunnerProviderShell(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, label, providerShell, stepShell, defaults, container string
		wantFinding                                                bool
	}{
		{name: "provider replaces hosted default", label: "ubuntu-latest", providerShell: "python", wantFinding: true},
		{name: "provider resolves custom label", label: "custom-linux", providerShell: "bash"},
		{name: "unknown provider cannot borrow label defaults", label: "ubuntu-latest", wantFinding: true},
		{name: "step shell overrides provider", label: "ubuntu-latest", providerShell: "python", stepShell: "bash"},
		{name: "job shell overrides provider", label: "ubuntu-latest", providerShell: "python", defaults: "    defaults: {run: {shell: bash}}\n"},
		{name: "workflow shell overrides provider", label: "ubuntu-latest", providerShell: "python", defaults: "defaults: {run: {shell: bash}}\n"},
		{name: "container shell overrides provider", label: "ubuntu-latest", providerShell: "python", container: "    container: alpine\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := "on: push\n"
			if len(tc.defaults) > 0 && tc.defaults[0] != ' ' {
				content += tc.defaults
			}
			content += "jobs:\n  test:\n    runs-on: " + tc.label + "\n"
			if len(tc.defaults) > 0 && tc.defaults[0] == ' ' {
				content += tc.defaults
			}
			content += tc.container + "    steps:\n      - run: print(${{ github.repository }})\n"
			if tc.stepShell != "" {
				content += "        shell: " + tc.stepShell + "\n"
			}
			cfg, err := checking.ConfigurationRules().ParseConfig([]byte("plugins: []\ntools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			options := checking.Options{Runners: runnerapi.Options{
				Target: "github", Provider: "expression-shell", Scope: "test",
				Snapshot:  runnerapi.RunnerSnapshot{Scope: "test", Freshness: runnerapi.Fresh, Complete: true},
				Providers: []runnerapi.Registration{{ImplementationID: "test", Provider: expressionShellProvider{shell: tc.providerShell}}},
			}}
			result, err := checking.Analyze(t.Context(), options, []workspace.InputFile{{Path: "workflow.yml", Content: []byte(content), Config: cfg}})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				found = found || diagnostic.Rule == "script-interpolation"
			}
			if found != tc.wantFinding {
				t.Fatalf("script-interpolation=%t, want %t: %+v", found, tc.wantFinding, result.Diagnostics)
			}
		})
	}
}
