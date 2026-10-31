package python

import (
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
)

func TestTemplateEffectsIgnorePresentation(t *testing.T) {
	start := analyzer.ScriptPosition{Row: 1, Column: 3}
	masks := Masks{quoted: map[analyzer.ScriptPosition]string{start: "_0"}}
	finding := analyzer.ScriptDiagnostic{
		Code: "renamed-rule", Message: "entirely different wording",
		Location: analyzer.ScriptPosition{Row: 1, Column: 1}, EndLocation: analyzer.ScriptPosition{Row: 1, Column: 8},
		TemplateEffect: analyzer.TemplateEffectExportedName,
	}
	if masks.Accept(finding) {
		t.Fatal("semantic export finding was emitted for mask")
	}
	finding.TemplateEffect = analyzer.TemplateEffectNone
	finding.Code = "F822"
	finding.Message = "Undefined name `_0`"
	if !masks.Accept(finding) {
		t.Fatal("presentation text overrode explicit independent effect")
	}
	finding.TemplateEffect = analyzer.TemplateEffectExportedName
	finding.Location = analyzer.ScriptPosition{Row: 2, Column: 1}
	finding.EndLocation = analyzer.ScriptPosition{Row: 2, Column: 8}
	if !masks.Accept(finding) {
		t.Fatal("static export outside the mask was dropped")
	}
}
