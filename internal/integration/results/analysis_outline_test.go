package results_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	documentmodel "actionlint.kjanat.dev/v2/document"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/buildinfo"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/render"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
	"actionlint.kjanat.dev/v2/internal/workspace"
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
	analysis, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{
		{Path: "clean.yml", Content: []byte(source)},
		{Path: "partial.yml", Content: []byte(strings.Replace(source, "run: echo hi", "unexpected: value", 1))},
		{Path: "broken.yml", Content: []byte("jobs: [")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Documents) != 3 {
		t.Fatalf("lost input inventory: %+v", analysis.Documents)
	}
	clean := requireWorkflowOutline(t, analysis.Documents[0])
	partial := requireWorkflowOutline(t, analysis.Documents[1])
	broken := requireWorkflowOutline(t, analysis.Documents[2])
	if clean.DocumentPath() != "clean.yml" || clean.Name() != "Build" || clean.ParseStatus() != "complete" || !reflect.DeepEqual(clean.Triggers(), []string{"push", "workflow_dispatch"}) {
		t.Fatalf("wrong workflow: %+v", clean)
	}
	if len(clean.Jobs()) != 2 || clean.Jobs()[0].ID() != "ZBuild" || clean.Jobs()[1].ID() != "ADeploy" {
		t.Fatalf("job casing/source order lost: %+v", clean.Jobs())
	}
	build, deploy := clean.Jobs()[0], clean.Jobs()[1]
	if build.Name() != "${{ github.ref }}" || build.Start() == nil || *build.Start() != (diagmodel.Position{Line: 4, Column: 3}) {
		t.Fatalf("declared expression/location lost: %+v", build)
	}
	if len(build.Steps()) != 2 || build.Steps()[0].Kind() != "uses" || build.Steps()[0].Uses() != "actions/checkout@v7" || build.Steps()[1].Kind() != "run" || build.Steps()[1].ID() != "script" {
		t.Fatalf("wrong steps: %+v", build.Steps())
	}
	if build.Steps()[0].Start() == nil || *build.Steps()[0].Start() != (diagmodel.Position{Line: 8, Column: 9}) {
		t.Fatalf("step location lost: %+v", build.Steps()[0])
	}
	if !reflect.DeepEqual(deploy.Needs(), []string{"ZBuild"}) || deploy.Uses() != "./.github/workflows/deploy.yml" || deploy.Steps() == nil || len(deploy.Steps()) != 0 {
		t.Fatalf("reusable call lost: %+v", deploy)
	}
	if !reflect.DeepEqual(deploy.Reference(), documentmodel.SelfRepositoryReference{Path: ".github/workflows/deploy.yml"}) {
		t.Fatalf("local workflow call is not a self-repository reference: %#v", deploy.Reference())
	}
	if partial.DocumentPath() != "partial.yml" || partial.ParseStatus() != "partial" || len(partial.Jobs()) != 2 || partial.Jobs()[0].Steps()[1].Kind() != "unknown" {
		t.Fatalf("partial tree lost: %+v", partial)
	}
	if broken.DocumentPath() != "broken.yml" || broken.ParseStatus() != "failed" || broken.Jobs() == nil || len(broken.Jobs()) != 0 {
		t.Fatalf("failed parse missing: %+v", broken)
	}
	renderer, err := render.New(render.FormatJSON, "", false, buildinfo.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := renderer.Render(&output, render.Prepare(*analysis)); err != nil {
		t.Fatal(err)
	}
	var report render.CheckResult
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
	const manifest = `name: Local
description: Local test action
inputs:
  Token:
    description: A token
    required: true
    default: ''
runs:
  using: composite
  steps:
    - id: child
      uses: actions/checkout@v7
`
	for name, content := range map[string]string{"action.yml": manifest, "broken/action.yml": "runs: ["} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const source = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/
      - uses: $/
      - uses: $/broken
      - uses: $/missing
`
	analysis, err := application.Analyze(t.Context(), application.Options{WorkingDir: dir}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Project: workspace.Identity(dir)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Documents) != 3 {
		t.Fatalf("wanted workflow and two read manifests: %+v", analysis.Documents)
	}
	action, ok := analysis.Documents[1].(documentmodel.Action)
	if !ok || action.DocumentPath() != "action.yml" || action.ParseStatus() != "complete" {
		t.Fatalf("wrong action document: %+v", analysis.Documents[1])
	}
	broken, ok := analysis.Documents[2].(documentmodel.Action)
	if !ok || broken.ParseStatus() != "failed" {
		t.Fatalf("missing failed parse document: %+v", analysis.Documents[2])
	}
	data, err := json.Marshal(render.Prepare(*analysis).CheckResult())
	if err != nil {
		t.Fatal(err)
	}
	var result render.CheckResult
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
		{"conflicting runtime", `name: Test
description: Test
runs: {using: node24, main: main.mjs, image: docker://alpine:3}`, []string{`"image" is not allowed in "runs" section`}},
		{"conflicting step", `name: Test
description: Test
runs: {using: composite, steps: [{run: echo hi, uses: actions/checkout@v7, shell: bash}]}`, []string{`cannot have both "run" and "uses" keys`}},
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
			const source = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: $/
`
			analysis, err := application.Analyze(t.Context(), application.Options{WorkingDir: dir}, []workspace.Source{{Path: "ci.yml", Content: []byte(source), Project: workspace.Identity(dir)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(analysis.Documents) != 2 {
				t.Fatalf("missing action inventory: %+v", analysis.Documents)
			}
			action, ok := analysis.Documents[1].(documentmodel.Action)
			if !ok || action.ParseStatus() != "complete" {
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

func requireWorkflowOutline(t *testing.T, document documentmodel.Outline) documentmodel.Workflow {
	t.Helper()
	workflow, ok := document.(documentmodel.Workflow)
	if !ok {
		t.Fatalf("expected workflow, got %T", document)
	}
	return workflow
}

func TestAnalysisFailureKeepsOutline(t *testing.T) {
	root := t.TempDir()
	testutil.WriteFixture(t, root, "local/index.js", "console.log('ok');\n")
	testutil.WriteFixture(t, root, "local/action.yml", `name: local
description: test
runs:
  using: node24
  main: index.js
`)
	const workflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local
`
	bogus := func(include string) *repoconfig.Config {
		lint := &repoconfig.LintConfig{Rules: repoconfig.LintRulesConfig{Correctness: repoconfig.Group{Rules: map[string]repoconfig.Setting{"inline-suppression": {Level: "bogus"}}}}}
		return &repoconfig.Config{Overrides: []repoconfig.Override{{Includes: []string{include}, Lint: lint}}}
	}
	action := filepath.FromSlash("local/action.yml") + ":complete"
	for _, tc := range []struct {
		name    string
		config  *repoconfig.Config
		ruff    string
		sources map[string]string
		wantErr string
		want    []string
	}{
		{"workflow config", bogus("**"), "", map[string]string{"ci.yml": workflow}, "rule level", []string{"ci.yml:complete"}},
		{"workflow config with partial parse", bogus("**"), "", map[string]string{"ci.yml": strings.Replace(workflow, "    steps:", "    unknown: x\n    steps:", 1)}, "rule level", []string{"ci.yml:partial"}},
		{"workflow config with failed parse", bogus("**"), "", map[string]string{"ci.yml": "on: [\n"}, "rule level", []string{"ci.yml:failed"}},
		{"ruff", nil, filepath.Join(root, "missing-ruff"), map[string]string{"ci.yml": workflow}, "could not initialize Ruff", []string{"ci.yml:complete"}},
		{"metadata config", bogus("local/action.yml"), "", map[string]string{"ci.yml": workflow}, "rule level", []string{"ci.yml:complete", action}},
		{"one of several sources", bogus("bad.yml"), "", map[string]string{"bad.yml": workflow, "ok.yml": workflow}, "rule level", []string{"bad.yml:complete", "ok.yml:complete", action}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sources []workspace.Source
			for _, path := range slices.Sorted(maps.Keys(tc.sources)) {
				sources = append(sources, workspace.Source{Path: path, Content: []byte(tc.sources[path]), Project: workspace.Identity(root), Config: tc.config})
			}
			analysis, err := application.Analyze(t.Context(), application.Options{WorkingDir: root, Tools: (adapters.Options{Ruff: tc.ruff}).Registrations()}, sources)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("fixture must fail with %q: %v", tc.wantErr, err)
			}
			if analysis == nil {
				t.Fatalf("analysis failure discarded result: %v", err)
			}
			got := make([]string, 0, len(analysis.Documents))
			for _, document := range analysis.Documents {
				switch document := document.(type) {
				case documentmodel.Workflow:
					got = append(got, document.DocumentPath()+":"+document.ParseStatus())
				case documentmodel.Action:
					got = append(got, document.DocumentPath()+":"+document.ParseStatus())
				default:
					t.Fatalf("unexpected document %T", document)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("analysis failure changed documents: got %v, want %v (%v)", got, tc.want, err)
			}
		})
	}
}
