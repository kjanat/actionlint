package actionlint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	if len(analysis.Documents) != 3 {
		t.Fatalf("lost input inventory: %+v", analysis.Documents)
	}
	clean := requireWorkflowOutline(t, analysis.Documents[0])
	partial := requireWorkflowOutline(t, analysis.Documents[1])
	broken := requireWorkflowOutline(t, analysis.Documents[2])
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
	if !reflect.DeepEqual(report.Documents, analysis.Documents) {
		t.Fatalf("JSON lost outlines: %s", &output)
	}
	if strings.Contains(output.String(), "echo hi") {
		t.Fatal("outline unexpectedly includes script bodies")
	}
}

func TestAnalysisActionDocuments(t *testing.T) {
	dir := t.TempDir()
	const manifest = "name: Local\ndescription: Local test action\ninputs:\n  Token:\n    description: A token\n    required: true\n    default: ''\nruns:\n  using: composite\n  steps:\n    - id: child\n      uses: actions/checkout@v7\n"
	for name, content := range map[string]string{"action.yml": manifest, "broken/action.yml": "runs: ["} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/\n      - uses: $/\n      - uses: $/broken\n      - uses: $/missing\n"
	analysis, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source), Project: &Project{root: dir}}}, WorkingDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Documents) != 3 {
		t.Fatalf("wanted workflow and two read manifests: %+v", analysis.Documents)
	}
	action, ok := analysis.Documents[1].(ActionOutline)
	if !ok || action.Path != "action.yml" || action.ParseStatus != "complete" {
		t.Fatalf("wrong action document: %+v", analysis.Documents[1])
	}
	broken, ok := analysis.Documents[2].(ActionOutline)
	if !ok || broken.ParseStatus != "failed" {
		t.Fatalf("missing failed parse document: %+v", analysis.Documents[2])
	}
	data, err := json.Marshal(analysis.CheckResult())
	if err != nil {
		t.Fatal(err)
	}
	var result CheckResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Documents, analysis.Documents) {
		t.Fatalf("document roundtrip lost data: %s", data)
	}
}

func TestAnalysisActionOutlineValidationDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
		messages       []string
	}{
		{"missing identity", "runs: {using: composite, steps: []}", []string{"name is required", "description is required"}},
		{"conflicting runtime", "name: Test\ndescription: Test\nruns: {using: node24, main: main.mjs, image: docker://alpine:3}", []string{`"image" is not allowed in "runs" section`}},
		{"conflicting step", "name: Test\ndescription: Test\nruns: {using: composite, steps: [{run: echo hi, uses: actions/checkout@v7, shell: bash}]}", []string{`cannot have both "run" and "uses" keys`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "action.yml"), []byte(tc.manifest), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.name == "conflicting runtime" {
				if err := os.WriteFile(filepath.Join(dir, "main.mjs"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			const source = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: $/\n"
			analysis, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "ci.yml", Content: []byte(source), Project: &Project{root: dir}}}, WorkingDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			if len(analysis.Documents) != 2 {
				t.Fatalf("missing action inventory: %+v", analysis.Documents)
			}
			action, ok := analysis.Documents[1].(ActionOutline)
			if !ok || action.ParseStatus != "complete" {
				t.Fatalf("analysis validation changed decoding status: %+v", analysis.Documents[1])
			}
			for _, message := range tc.messages {
				found := false
				for _, diagnostic := range analysis.Diagnostics {
					if strings.Contains(diagnostic.Message, message) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("missing validation diagnostic containing %q: %+v", message, analysis.Diagnostics)
				}
			}
		})
	}
}

func requireWorkflowOutline(t *testing.T, document DocumentOutline) WorkflowOutline {
	t.Helper()
	workflow, ok := document.(WorkflowOutline)
	if !ok {
		t.Fatalf("expected workflow, got %T", document)
	}
	return workflow
}

func TestAnalysisEmptyDocuments(t *testing.T) {
	result, err := Analyze(t.Context(), AnalysisRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []CheckResult{result.CheckResult(), (&AnalysisResult{}).CheckResult(), NewCheckResult(3)} {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		if string(object["documents"]) != "[]" {
			t.Fatalf("empty inventory omitted or null: %s", data)
		}
	}
}

func TestAnalysisPartialWorkflowCall(t *testing.T) {
	const source = "on: push\njobs:\n  call:\n    uses: owner/repo/.github/workflows/build.yml@main\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"
	workflow, errs := Parse([]byte(source))
	if len(errs) == 0 || workflow.Jobs["call"].WorkflowCall != nil {
		t.Fatal("fixture must contain rejected workflow call")
	}
	outline := workflowOutline("ci.yml", workflow, true)
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
		{"uses before parallel", "uses: actions/checkout@v7\n        parallel:\n          - run: echo hi", "parallel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - " + tc.step + "\n"
			workflow, errs := Parse([]byte(source))
			if len(errs) == 0 {
				t.Fatal("fixture must contain conflicting execution keys")
			}
			outline := workflowOutline("ci.yml", workflow, true)
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
