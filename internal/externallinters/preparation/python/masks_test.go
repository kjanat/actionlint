package python

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func TestTemplateEffectsIgnorePresentation(t *testing.T) {
	t.Parallel()
	start := sourcetext.Position{Line: 1, Column: 3}
	masks := Masks{quoted: map[sourcetext.Position]string{start: "_0"}}
	finding := scriptengine.Diagnostic{
		Code: "renamed-rule", Message: "entirely different wording",
		Location: sourcetext.Position{Line: 1, Column: 1}, EndLocation: sourcetext.Position{Line: 1, Column: 8},
		TemplateEffect: scriptengine.TemplateEffectExportedName,
	}
	if masks.Accept(finding) {
		t.Fatal("semantic export finding was emitted for mask")
	}
	finding.TemplateEffect = scriptengine.TemplateEffectNone
	finding.Code = "F822"
	finding.Message = "Undefined name `_0`"
	if !masks.Accept(finding) {
		t.Fatal("presentation text overrode explicit independent effect")
	}
	finding.TemplateEffect = scriptengine.TemplateEffectExportedName
	finding.Location = sourcetext.Position{Line: 2, Column: 1}
	finding.EndLocation = sourcetext.Position{Line: 2, Column: 8}
	if !masks.Accept(finding) {
		t.Fatal("static export outside the mask was dropped")
	}
}
