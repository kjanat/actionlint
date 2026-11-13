package rules_test

import (
	"slices"
	"strings"
	"testing"

	runnerinput "actionlint.kjanat.dev/v2/internal/input/runners"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestShellPlatformsDoNotDependOnTraversalOrder(t *testing.T) {
	t.Parallel()
	const source = `on: push
defaults:
  run: {shell: cmd}
jobs:
  windows:
    runs-on: windows-latest
    defaults:
      run: {shell: sh}
    steps:
      - run: echo invalid
        shell: sh
      - run: echo valid
        shell: cmd
  linux:
    runs-on: ubuntu-latest
    defaults:
      run: {shell: cmd}
    steps:
      - run: echo invalid
        shell: cmd
      - run: echo valid
        shell: sh
`
	_, snapshots := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := snapshots.Document("workflow.yml", []byte(source), parsing.Workflow([]byte(source)))
	if err != nil {
		t.Fatal(err)
	}
	knowledge, err := runnerinput.Prepare(t.Context(), snapshot, nil, runnerapi.Options{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	workflow, available := snapshot.WorkflowView()
	if !available {
		t.Fatal("fixture workflow is unavailable")
	}
	check := correctness.ShellNameCheck(knowledge)
	for range 2 {
		for _, name := range []string{"linux", "windows"} {
			job, found := workflow.Job(name)
			if !found {
				t.Fatal("fixture job is missing")
			}
			steps := slices.Collect(job.Steps())
			// Invoke steps before any workflow or job callback, mixing platforms.
			for _, index := range []int{1, 0} {
				findings := check.Step(steps[index])
				if index == 1 {
					if len(findings) != 0 {
						t.Fatalf("valid %s shell rejected: %+v", name, findings)
					}
					continue
				}
				if len(findings) != 1 || findings[0].Step.Reference() != steps[index].Reference() || !strings.Contains(findings[0].Message, " on ") {
					t.Fatalf("lost %s platform or source owner: %+v", name, findings)
				}
				if got := correctness.ShellNameCheck(nil).Step(steps[index]); len(got) != 0 {
					t.Fatalf("unknown platform rejected a supported shell: %+v", got)
				}
			}
			if findings := check.Job(job); len(findings) != 1 {
				t.Fatalf("job defaults lost %s platform: %+v", name, findings)
			}
		}
		if findings := check.Workflow(workflow); len(findings) != 0 {
			t.Fatalf("workflow defaults used a job platform: %+v", findings)
		}
	}
}
