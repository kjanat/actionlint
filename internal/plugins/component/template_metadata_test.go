package component

import (
	"actionlint.kjanat.dev/v2/analyzer"
	"testing"
)

func TestTemplateMetadataRequiresMatchingExport(t *testing.T) {
	declared := []byte(`{"template_contract":"kjanat:actionlint/template-effects@0.1.0"}`)
	if err := verifyTemplateMetadata(declared, ""); err == nil {
		t.Fatal("metadata accepted without matching export")
	}
	if err := verifyTemplateMetadata([]byte(`{}`), analyzer.TemplateEffectsContract); err == nil {
		t.Fatal("export accepted without matching metadata")
	}
	if err := verifyTemplateMetadata(declared, analyzer.TemplateEffectsContract); err != nil {
		t.Fatal(err)
	}
}
