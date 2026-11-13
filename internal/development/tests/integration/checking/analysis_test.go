package checking_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	workspace "actionlint.kjanat.dev/v2/internal/input/workspace"
	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"
	"actionlint.kjanat.dev/v2/internal/results"
)

const workflow = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
permissions: {}
`

type scriptAnalyzer struct{ failure error }

func (scriptAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "custom-script", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts}}
}

func (a scriptAnalyzer) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, script := range input.Scripts {
		findings = append(findings, results.Diagnostic{Message: "observed script", Start: script.Start})
	}
	return findings, a.failure
}

func TestPublicAnalysisContract(t *testing.T) {
	t.Parallel()
	failure := errors.New("partial analysis")
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: scriptAnalyzer{failure}}}}, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow)}},
	)
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	if result == nil || result.FileCount != 1 || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%+v", result)
	}
	finding := result.Diagnostics[0]
	if finding.Rule != "custom-script" || finding.Path != "workflow.yml" || finding.Start.Line != 6 {
		t.Fatalf("finding=%+v", finding)
	}
}

func TestPublicConfigSyntaxAndFiles(t *testing.T) {
	t.Parallel()
	cfg, err := checking.ConfigurationRules().Parse([]byte("config-variables: [KNOWN]\n"))
	if err != nil {
		t.Fatal(err)
	}
	tree, findings := workflowparser.Parse([]byte(workflow))
	if tree == nil || tree.Jobs["build"] == nil || len(findings) != 0 {
		t.Fatalf("tree=%v findings=%v", tree, findings)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "workflow.yml")
	if err := os.WriteFile(path, []byte(workflow), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := checking.Check(t.Context(), checking.Options{WorkingDir: directory, SkipProjectConfig: true}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	result, err = checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestPublicActionScriptFacts(t *testing.T) {
	t.Parallel()
	result, err := checking.Analyze(t.Context(),

		checking.Options{Analyzers: []checkinputs.Registration{{Checker: scriptAnalyzer{}}}}, []workspace.Source{{Path: "action.yml", Content: []byte("name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo hello\n      shell: bash\n")}},
	)
	if err != nil || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "custom-script" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
