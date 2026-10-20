package application_test

import (
	"context"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type detailedAnalyzer struct{ findings []diagnostic.Diagnostic }

func (a detailedAnalyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "detailed-check", Requires: []analyzer.Fact{analyzer.Source}}
}

func (a detailedAnalyzer) Analyze(context.Context, analyzer.Input) ([]diagnostic.Diagnostic, error) {
	return a.findings, nil
}

func TestCanonicalDiagnosticMetadataSurvivesPolicy(t *testing.T) {
	const content = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: 'echo ok'}]\n"
	base := diagnostic.Diagnostic{Rule: "detailed-check", Path: "ci.yml", Message: "same location and message", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 3}, Code: "A", Severity: "warning", Help: "help", Explanation: "explanation", DocumentationURL: "https://example.invalid/rule", Related: []diagnostic.Related{{Path: "other.yml", Message: "related", Start: diagnostic.Position{Line: 1, Column: 1}}}, Fixes: []diagnostic.Fix{{Applicability: "safe", Description: "replace", Edits: []diagnostic.Edit{{Path: "ci.yml", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 3}, Replacement: "on"}}}}}
	second := base
	second.Code = "B"
	third := base
	third.End = diagnostic.Position{Line: 2, Column: 1}
	input := []diagnostic.Diagnostic{base, second, third}
	result, err := application.Analyze(t.Context(), application.Options{Analyzers: []analyzer.Registration{{Analyzer: detailedAnalyzer{input}}}}, []workspace.Source{{Path: "ci.yml", Content: []byte(content)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 3 || len(result.Sources) != 3 {
		t.Fatalf("distinct diagnostics or sources lost: %+v", result)
	}
	for i, d := range result.Diagnostics {
		if d.Help != base.Help || d.Explanation != base.Explanation || d.DocumentationURL != base.DocumentationURL || !reflect.DeepEqual(d.Related, base.Related) || len(d.Fixes) != 1 || d.Fixes[0].Applicability != "safe" {
			t.Fatalf("metadata lost: %+v", d)
		}
		if string(result.Sources[i].Content) != content {
			t.Fatal("source snapshot lost")
		}
	}
	if input[0].Reference != nil || input[0].Fixes[0].Revisions != nil {
		t.Fatal("analysis mutated the analyzer's findings")
	}
}
