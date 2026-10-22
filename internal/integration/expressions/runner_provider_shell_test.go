package expressions_test

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
	"actionlint.kjanat.dev/v2/runner"
)

type expressionShellProvider struct{ shell string }

func (expressionShellProvider) RunnerDescriptor() runner.Descriptor {
	return runner.Descriptor{Name: "expression-shell", Version: "1", Targets: []string{"github"}}
}

func (p expressionShellProvider) ResolveRunners(_ context.Context, request runner.Request) ([]runner.Answer, error) {
	answers := make([]runner.Answer, len(request.Selectors))
	for i := range answers {
		var resolution runner.Resolution = runner.Unknown{Reason: "shell unavailable"}
		if p.shell != "" {
			resolution = runner.Resolved{Candidates: []runner.Candidate{{Name: "test", OS: runner.Linux, DefaultShell: p.shell}}}
		}
		answers[i] = runner.Answer{Resolution: resolution, Evidence: runner.Evidence{Dataset: "test", Scope: request.Scope, Freshness: runner.Fresh, Complete: true}}
	}
	return answers, nil
}

func TestScriptInterpolationUsesRunnerProviderShell(t *testing.T) {
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
			cfg, err := repoconfig.Parse([]byte("plugins: []\ntools: {shellcheck: false, ruff: false}"))
			if err != nil {
				t.Fatal(err)
			}
			options := application.Options{Runners: runner.Options{
				Target: "github", Provider: "expression-shell", Scope: "test",
				Snapshot:  runner.Snapshot{Scope: "test", Freshness: runner.Fresh, Complete: true},
				Providers: []runner.Registration{{Identity: "test", Provider: expressionShellProvider{shell: tc.providerShell}}},
			}}
			result, err := application.Analyze(t.Context(), options, []workspace.Source{{Path: "workflow.yml", Content: []byte(content), Config: cfg}})
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
