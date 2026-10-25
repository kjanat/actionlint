package ruff

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/tools"
	"strings"
	"testing"
)

func TestPreparedInputSharesPythonTemplateMasks(t *testing.T) {
	for _, source := range []string{"print(${{ inputs.value }})\nprint(missing)", "print('${{ inputs.value }}')\nprint(missing)"} {
		prepared, err := prepareInput(tools.Script{Text: source})
		if err != nil || prepared.Skip || prepared.AcceptDiagnostic == nil {
			t.Fatalf("preparation failed: %+v %v", prepared, err)
		}
		if strings.Contains(prepared.Source, "${{") {
			t.Fatalf("unmasked template: %q", prepared.Source)
		}
		// Ruff reports each identifier; synthetic template identifiers alone are filtered.
		start := strings.Index(prepared.Source, "_")
		if start < 0 {
			t.Fatalf("missing Python opaque value: %q", prepared.Source)
		}
		end := start
		for end < len(prepared.Source) && (prepared.Source[end] == '_' || prepared.Source[end] >= '0' && prepared.Source[end] <= '9' || prepared.Source[end] >= 'a' && prepared.Source[end] <= 'z') {
			end++
		}
		if source[6] != '\'' && prepared.AcceptDiagnostic(analyzer.ScriptDiagnostic{Code: "F821", TemplateEffect: analyzer.TemplateEffectOpaqueName, Location: Position{Row: 1, Column: start + 1}, EndLocation: Position{Row: 1, Column: end + 1}}) {
			t.Fatal("synthetic undefined-name finding escaped")
		}
		if !prepared.AcceptDiagnostic(analyzer.ScriptDiagnostic{Code: "F821", TemplateEffect: analyzer.TemplateEffectOpaqueName, Location: Position{Row: 2, Column: 7}, EndLocation: Position{Row: 2, Column: 14}}) {
			t.Fatal("unrelated undefined name suppressed")
		}
	}
}
