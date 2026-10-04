package actionlint

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAnalysisWorkflowOutlines(t *testing.T) {
	const source = `name: Build
on: [push, workflow_dispatch]
jobs:
  ZBuild:
    name: "${{ github.ref }}"
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - id: script
        run: echo hi
  ADeploy:
    needs: ZBuild
    uses: ./.github/workflows/deploy.yml
`
	analysis, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{
		{Path: "clean.yml", Content: []byte(source)},
		{Path: "partial.yml", Content: []byte(strings.Replace(source, "run: echo hi", "unexpected: value", 1))},
		{Path: "broken.yml", Content: []byte("jobs: [")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Workflows) != 3 {
		t.Fatalf("lost input inventory: %+v", analysis.Workflows)
	}
	clean, partial, broken := analysis.Workflows[0], analysis.Workflows[1], analysis.Workflows[2]
	if clean.Path != "clean.yml" || clean.Name != "Build" || clean.ParseStatus != "complete" || !reflect.DeepEqual(clean.Triggers, []string{"push", "workflow_dispatch"}) {
		t.Fatalf("wrong workflow: %+v", clean)
	}
	if len(clean.Jobs) != 2 || clean.Jobs[0].ID != "ZBuild" || clean.Jobs[1].ID != "ADeploy" {
		t.Fatalf("job casing/source order lost: %+v", clean.Jobs)
	}
	build, deploy := clean.Jobs[0], clean.Jobs[1]
	if build.Name != "${{ github.ref }}" || build.Start == nil || *build.Start != (DiagnosticPosition{4, 3}) {
		t.Fatalf("declared expression/location lost: %+v", build)
	}
	if len(build.Steps) != 2 || build.Steps[0].Kind != "uses" || build.Steps[0].Uses != "actions/checkout@v7" || build.Steps[1].Kind != "run" || build.Steps[1].ID != "script" {
		t.Fatalf("wrong steps: %+v", build.Steps)
	}
	if build.Steps[0].Start == nil || *build.Steps[0].Start != (DiagnosticPosition{8, 9}) {
		t.Fatalf("step location lost: %+v", build.Steps[0])
	}
	if !reflect.DeepEqual(deploy.Needs, []string{"ZBuild"}) || deploy.Uses != "./.github/workflows/deploy.yml" || deploy.Steps == nil || len(deploy.Steps) != 0 {
		t.Fatalf("reusable call lost: %+v", deploy)
	}
	if partial.Path != "partial.yml" || partial.ParseStatus != "partial" || len(partial.Jobs) != 2 || partial.Jobs[0].Steps[1].Kind != "unknown" {
		t.Fatalf("partial tree lost: %+v", partial)
	}
	if broken.Path != "broken.yml" || broken.ParseStatus != "failed" || broken.Jobs == nil || len(broken.Jobs) != 0 {
		t.Fatalf("failed parse missing: %+v", broken)
	}
	renderer, err := NewAnalysisRenderer(OutputFormatJSON, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := renderer.Render(&output, analysis); err != nil {
		t.Fatal(err)
	}
	var report CheckResult
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Workflows, analysis.Workflows) {
		t.Fatalf("JSON lost outlines: %s", &output)
	}
	if strings.Contains(output.String(), "echo hi") {
		t.Fatal("outline unexpectedly includes script bodies")
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
	outline := workflowOutline("ci.yml", workflow, false)
	steps := outline.Jobs[0].Steps
	if len(steps) != 3 || steps[0].Kind != "parallel" || steps[1].Kind != "wait" || steps[2].Kind != "cancel" {
		t.Fatalf("wrong control step kinds: %+v", steps)
	}
	children := steps[0].Steps
	if len(children) != 2 || children[0].ID != "child" || children[0].Kind != "run" || children[1].Uses != "actions/checkout@v7" {
		t.Fatalf("parallel children lost: %+v", children)
	}
}
