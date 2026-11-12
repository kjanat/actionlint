package checking_test

import (
	"context"
	"reflect"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/results"
)

type detailedAnalyzer struct{ findings []results.Diagnostic }

func (a detailedAnalyzer) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "detailed-check", Category: "correctness", RequiredInputs: []rulemeta.InputKind{rulemeta.Source}}
}

func (a detailedAnalyzer) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	return a.findings, nil
}

func TestCanonicalDiagnosticMetadataSurvivesPolicy(t *testing.T) {
	t.Parallel()
	const content = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: 'echo ok'}]\npermissions: {}\n"
	base := results.Diagnostic{Rule: "detailed-check", Path: "ci.yml", Message: "same location and message", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 3}, Code: "A", Severity: "warning", Help: "help", Explanation: "explanation", DocumentationURL: "https://example.invalid/rule", Related: []results.Related{{Path: "ci.yml", Message: "related", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 1}}}, Fixes: []results.Fix{{Applicability: results.FixSafe, Description: "replace", Edits: []results.Edit{{Path: "ci.yml", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 3}, Replacement: "on"}}}}}
	second := base
	second.Code = "B"
	third := base
	third.End = sourcepos.Position{Line: 2, Column: 1}
	input := []results.Diagnostic{base, second, third}
	result, err := checking.Analyze(t.Context(), checking.Options{Analyzers: []checkinputs.Registration{{Checker: detailedAnalyzer{input}}}}, []workspace.Source{{Path: "ci.yml", Content: []byte(content)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 3 || len(result.Sources) != 3 {
		t.Fatalf("distinct diagnostics or sources lost: %+v", result)
	}
	for i, d := range result.Diagnostics {
		if d.Help != base.Help || d.Explanation != base.Explanation || d.DocumentationURL != base.DocumentationURL || len(d.Related) != 1 || d.Related[0].Revision.Document() != d.Reference.Document || d.Related[0].Revision.Digest() != d.Reference.Revision || d.Related[0].Coordinates != sourcepos.YAML || len(d.Fixes) != 1 || d.Fixes[0].Applicability != results.FixSafe {
			t.Fatalf("metadata lost: %+v", d)
		}
		unbound := d.Related[0]
		unbound.Revision = sourcepos.Revision{}
		unbound.Coordinates = ""
		if !reflect.DeepEqual(unbound, base.Related[0]) {
			t.Fatal("related metadata changed while binding")
		}
		if string(result.Sources[i].Content) != content {
			t.Fatal("source snapshot lost")
		}
	}
	if input[0].Reference != nil || input[0].Fixes[0].Revisions != nil || input[0].Related[0].Revision.Valid() || input[0].Related[0].Coordinates != "" {
		t.Fatal("analysis mutated the analyzer's findings")
	}
}
