package analysis_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/config"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/syntax"

	actionlint "actionlint.kjanat.dev"
)

const workflow = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

type scriptAnalyzer struct{ failure error }

func (scriptAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "custom-script", Requires: []analyzer.Fact{analyzer.Scripts}}
}

func (a scriptAnalyzer) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
	for _, script := range input.Scripts {
		findings = append(findings, diagnostic.Diagnostic{Message: "observed script", Start: script.Start})
	}
	return findings, a.failure
}

func TestPublicAnalysisContract(t *testing.T) {
	failure := errors.New("partial analysis")
	result, err := actionlint.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow)}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: scriptAnalyzer{failure}}}},
	})
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
	cfg, err := config.Parse([]byte("config-variables: [KNOWN]\n"))
	if err != nil {
		t.Fatal(err)
	}
	tree, findings := syntax.Parse([]byte(workflow))
	if tree == nil || tree.Jobs["build"] == nil || len(findings) != 0 {
		t.Fatalf("tree=%v findings=%v", tree, findings)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "workflow.yml")
	if err := os.WriteFile(path, []byte(workflow), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Files(t.Context(), []string{path}, analysis.FileOptions{WorkingDir: directory, SkipProjectConfig: true})
	if err != nil || result.FileCount != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	result, err = analysis.Analyze(t.Context(), analysis.Request{Sources: []analysis.Source{{Path: "workflow.yml", Content: []byte(workflow), Config: cfg}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestPublicActionScriptFacts(t *testing.T) {
	result, err := analysis.Analyze(t.Context(), analysis.Request{
		Sources: []analysis.Source{{Path: "action.yml", Content: []byte("name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - run: echo hello\n      shell: bash\n")}},
		Options: analysis.Options{Analyzers: []analyzer.Registration{{Analyzer: scriptAnalyzer{}}}},
	})
	if err != nil || result == nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "custom-script" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
