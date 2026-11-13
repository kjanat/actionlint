package python

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
)

func TestTemplateEffectsIgnorePresentation(t *testing.T) {
	t.Parallel()
	start := checkinputs.ScriptPosition{Row: 1, Column: 3}
	masks := Masks{quoted: map[checkinputs.ScriptPosition]string{start: "_0"}}
	finding := checkinputs.ScriptDiagnostic{
		Code: "renamed-rule", Message: "entirely different wording",
		Location: checkinputs.ScriptPosition{Row: 1, Column: 1}, EndLocation: checkinputs.ScriptPosition{Row: 1, Column: 8},
		TemplateEffect: checkinputs.TemplateEffectExportedName,
	}
	if masks.Accept(finding) {
		t.Fatal("semantic export finding was emitted for mask")
	}
	finding.TemplateEffect = checkinputs.TemplateEffectNone
	finding.Code = "F822"
	finding.Message = "Undefined name `_0`"
	if !masks.Accept(finding) {
		t.Fatal("presentation text overrode explicit independent effect")
	}
	finding.TemplateEffect = checkinputs.TemplateEffectExportedName
	finding.Location = checkinputs.ScriptPosition{Row: 2, Column: 1}
	finding.EndLocation = checkinputs.ScriptPosition{Row: 2, Column: 8}
	if !masks.Accept(finding) {
		t.Fatal("static export outside the mask was dropped")
	}
}
