package documentparse

import (
	"reflect"
	"testing"

	documentmodel "actionlint.kjanat.dev/document"
)

func TestOutlineLocalReferenceScope(t *testing.T) {
	const source = `on: push
jobs:
  local:
    uses: ./.github/workflows/deploy.yml
  self:
    uses: $/.github/workflows/deploy.yml
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/setup
      - uses: $/.github/actions/setup
`
	workflow, errs := Parse([]byte(source))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	outline := WorkflowOutline("ci.yml", workflow, false)
	got := map[string]documentmodel.UsesReference{}
	for _, job := range outline.Jobs {
		got[job.ID] = job.Reference
		for _, step := range job.Steps {
			got[step.Uses] = step.Reference
		}
	}
	want := map[string]documentmodel.UsesReference{
		"local":                   documentmodel.SelfRepositoryReference{Path: ".github/workflows/deploy.yml"},
		"self":                    documentmodel.SelfRepositoryReference{Path: ".github/workflows/deploy.yml"},
		"build":                   nil,
		"./.github/actions/setup": documentmodel.WorkspaceReference{Path: ".github/actions/setup"},
		"$/.github/actions/setup": documentmodel.SelfRepositoryReference{Path: ".github/actions/setup"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references by scope:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestAnalysisPartialWorkflowCall(t *testing.T) {
	const source = `on: push
jobs:
  call:
    uses: owner/repo/.github/workflows/build.yml@main
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	workflow, errs := Parse([]byte(source))
	if len(errs) == 0 || workflow.Jobs["call"].WorkflowCall != nil {
		t.Fatal("fixture must contain rejected workflow call")
	}
	outline := WorkflowOutline("ci.yml", workflow, true)
	job := outline.Jobs[0]
	if outline.ParseStatus != "partial" || job.Uses != "owner/repo/.github/workflows/build.yml@main" || job.Reference == nil || len(job.Steps) != 1 {
		t.Fatalf("partial call declaration lost: %+v", outline)
	}
}

func TestAnalysisPartialStepUses(t *testing.T) {
	for _, tc := range []struct {
		name, step, kind string
	}{
		{"uses before run", "uses: actions/checkout@v7\n        run: echo hi", "run"},
		{"uses after run", "run: echo hi\n        uses: actions/checkout@v7", "uses"},
		{"literal uses before run", "uses: ${{ 'actions/checkout@v7' }}\n        run: echo hi", "run"},
		{"uses before wait", "uses: actions/checkout@v7\n        wait: child", "wait"},
		{"uses before cancel", "uses: actions/checkout@v7\n        cancel: child", "cancel"},
		{"uses before parallel", `uses: actions/checkout@v7
        parallel:
          - run: echo hi`, "parallel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - ` + tc.step + "\n"
			workflow, errs := Parse([]byte(source))
			if len(errs) == 0 {
				t.Fatal("fixture must contain conflicting execution keys")
			}
			outline := WorkflowOutline("ci.yml", workflow, true)
			step := outline.Jobs[0].Steps[0]
			if outline.ParseStatus != "partial" || step.Kind != tc.kind || step.Uses != "actions/checkout@v7" || step.Reference == nil {
				t.Fatalf("partial uses declaration lost: %+v", step)
			}
		})
	}
}

func TestAnalysisParallelOutline(t *testing.T) {
	const source = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - parallel:
          - id: child
            run: echo hi
          - uses: actions/checkout@v7
      - wait: child
      - cancel: child
`
	workflow, errors := Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("unexpected parse errors: %v", errors)
	}
	outline := WorkflowOutline("ci.yml", workflow, false)
	steps := outline.Jobs[0].Steps
	if len(steps) != 3 || steps[0].Kind != "parallel" || steps[1].Kind != "wait" || steps[2].Kind != "cancel" {
		t.Fatalf("wrong control step kinds: %+v", steps)
	}
	children := steps[0].Steps
	if len(children) != 2 || children[0].ID != "child" || children[0].Kind != "run" || children[1].Uses != "actions/checkout@v7" {
		t.Fatalf("parallel children lost: %+v", children)
	}
}
