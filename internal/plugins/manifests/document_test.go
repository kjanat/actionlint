package manifests_test

import (
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestDocumentManifestFactsAndSelectors(t *testing.T) {
	document := strings.Replace(minimal, `"role": "script"`, `"role": "document", "required_inputs":["source","dependencies"]`, 1)
	document = strings.Replace(document, `"selectors": {"filename": "actionlint.sh"}`, `"selectors":{"documents":["workflow","action"]}`, 1)
	document = strings.Replace(document, `"preparation": "kjanat:actionlint/shell-preparation@0.2.0"`, `"preparation":"generic"`, 1)
	manifest, err := manifests.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Role != manifests.RoleDocument || len(manifest.RequiredInputs) != 2 || manifest.RequiredInputs[1] != rulemeta.Dependencies || len(manifest.Selectors.Documents) != 2 {
		t.Fatal("document facts or selectors lost", manifest)
	}
	for name, invalid := range map[string]string{
		"script-role":      strings.Replace(document, `"role": "document"`, `"role":"script"`, 1),
		"unknown-fact":     strings.Replace(document, `"dependencies"`, `"future"`, 1),
		"duplicate-fact":   strings.Replace(document, `"source","dependencies"`, `"source","source"`, 1),
		"unknown-selector": strings.Replace(document, `["workflow","action"]`, `["unknown"]`, 1),
		"script-selector":  strings.Replace(document, `"documents":["workflow","action"]`, `"documents":["workflow"],"shells":["bash"]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manifests.Parse([]byte(invalid)); err == nil {
				t.Fatal("invalid document declaration accepted")
			}
		})
	}
}
