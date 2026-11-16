package python

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
)

func TestTemplateEffectsIgnorePresentation(t *testing.T) {
	t.Parallel()
	start := inputs.ScriptPosition{Row: 1, Column: 3}
	masks := Masks{quoted: map[inputs.ScriptPosition]string{start: "_0"}}
	finding := inputs.ScriptDiagnostic{
		Code: "renamed-rule", Message: "entirely different wording",
		Location: inputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: inputs.ScriptPosition{Row: 1, Column: 8},
		TemplateEffect: inputs.TemplateEffectExportedName,
	}
	if masks.Accept(finding) {
		t.Fatal("semantic export finding was emitted for mask")
	}
	finding.TemplateEffect = inputs.TemplateEffectNone
	finding.Code = "F822"
	finding.Message = "Undefined name `_0`"
	if !masks.Accept(finding) {
		t.Fatal("presentation text overrode explicit independent effect")
	}
	finding.TemplateEffect = inputs.TemplateEffectExportedName
	finding.Location = inputs.ScriptPosition{Row: 2, Column: 1}
	finding.EndLocation = inputs.ScriptPosition{Row: 2, Column: 8}
	if !masks.Accept(finding) {
		t.Fatal("static export outside the mask was dropped")
	}
}
