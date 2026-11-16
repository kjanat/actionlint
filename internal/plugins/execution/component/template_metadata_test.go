package component

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
)

func TestTemplateMetadataRequiresMatchingExport(t *testing.T) {
	t.Parallel()
	declared := []byte(`{"template_contract":"kjanat:actionlint/template-effects@0.1.0"}`)
	if err := verifyTemplateMetadata(declared, ""); err == nil {
		t.Fatal("metadata accepted without matching export")
	}
	if err := verifyTemplateMetadata([]byte(`{}`), inputs.TemplateEffectsContract); err == nil {
		t.Fatal("export accepted without matching metadata")
	}
	if err := verifyTemplateMetadata(declared, inputs.TemplateEffectsContract); err != nil {
		t.Fatal(err)
	}
}
