package runners

import (
	"context"
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
)

func TestHostedArchitectureCatalog(t *testing.T) {
	t.Parallel()
	for label, arch := range githubHostedArchitectures {
		if !slices.Contains(allGitHubHostedRunnerLabels, label) {
			t.Errorf("architecture for unknown hosted label %q", label)
		}
		if arch != "x64" && arch != "arm64" {
			t.Errorf("invalid architecture %q for %q", arch, label)
		}
	}
	for _, label := range allGitHubHostedRunnerLabels {
		if githubHostedArchitectures[label] == "" {
			t.Errorf("no architecture fact for hosted label %q", label)
		}
	}
}

func TestHostedArchitectureIgnoresCustomArmSuffix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		labels []string
		want   string
	}{
		{"hosted x64 with custom label", []string{"ubuntu-latest", "foo-arm"}, "x64"},
		{"custom label alone", []string{"foo-arm"}, ""},
		{"hosted arm64", []string{"ubuntu-24.04-arm"}, "arm64"},
		{"self hosted arm32", []string{"self-hosted", "linux", "arm"}, "arm32"},
		{"conflicting self hosted architectures", []string{"self-hosted", "linux", "arm", "arm64"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selector := runnerapi.Selector{}
			for _, label := range tc.labels {
				selector.Labels = append(selector.Labels, runnerapi.Constraint{Value: runnerapi.Literal(label)})
			}
			response, err := GitHubProvider(KnownLabels([]string{"foo-arm"})).ResolveRunners(context.Background(), runnerapi.Request{Target: "github", Scope: "scope", Selectors: []runnerapi.Selector{selector}})
			if err != nil {
				t.Fatal(err)
			}
			resolved, ok := response.Answers[0].Resolution.(runnerapi.Resolved)
			if !ok || len(resolved.Candidates) != 1 || resolved.Candidates[0].Architecture != tc.want {
				t.Fatalf("resolution = %+v, want architecture %q", response.Answers[0].Resolution, tc.want)
			}
		})
	}
}
