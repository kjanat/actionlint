package runner

import (
	"strconv"
	"strings"
	"testing"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

func TestRuleRunnerLabelDoNothingOnNoRunsOn(t *testing.T) {
	t.Parallel()
	if errs := LabelRejections(workflowdecl.JobView{}, KnownLabels(nil)); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestUbuntuRunnerArchitectures(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"22.04", "24.04", "26.04"} {
		arm := "ubuntu-" + version + "-arm"
		for _, x64 := range []string{"ubuntu-" + version, "ubuntu-latest", "ubuntu-latest-4-cores", "ubuntu-latest-8-cores", "ubuntu-latest-16-cores"} {
			t.Run(x64+"/"+arm, func(t *testing.T) {
				t.Parallel()
				fixture := runnerWorkflow(t, "["+strconv.Quote(x64)+", "+strconv.Quote(arm)+"]")
				errs := LabelRejections(fixture.Views["test"], KnownLabels(nil))
				if len(errs) != 1 || !strings.Contains(errs[0].Reason, "conflicts with") {
					t.Fatalf("incompatible architectures %s and %s: got %v", x64, arm, errs)
				}
			})
		}
		t.Run("linux/"+arm, func(t *testing.T) {
			t.Parallel()
			fixture := runnerWorkflow(t, "[linux, "+strconv.Quote(arm)+"]")
			if errs := LabelRejections(fixture.Views["test"], KnownLabels(nil)); len(errs) != 0 {
				t.Fatalf("Linux must remain compatible with %s: %v", arm, errs)
			}
		})
	}
}

func TestRuleRunnerLabelAllGitHubHostedRunnerLabels(t *testing.T) {
	t.Parallel()
	all := []string{}
	all = append(all, allGitHubHostedRunnerLabels...)
	all = append(all, selfHostedRunnerPresetOSLabels...)

	if len(all) != len(defaultRunnerOSCompats) {
		t.Errorf("%d elements in allGitHubHostedRunnerLabels but %d elements in githubHostedRunnerCompats", len(all), len(defaultRunnerOSCompats))
	}
	for _, l := range all {
		if l != strings.ToLower(l) {
			t.Errorf("label %q in allGitHubHostedRunnerLabels is not in lower-case", l)
		}
		if _, ok := defaultRunnerOSCompats[l]; !ok {
			t.Errorf("%q is included in allGitHubHostedRunnerLabels but not included in githubHostedRunnerCompats", l)
		}
	}
}
