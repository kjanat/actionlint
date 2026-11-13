package manifests_test

import (
	"strings"
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
)

func TestDocumentManifestFactsAndSelectors(t *testing.T) {
	t.Parallel()
	document := strings.Replace(minimal, `"role": "script"`, `"role": "document"`, 1)
	document = strings.Replace(document, `"rule": {`, `"rule": {"required_inputs":["source","dependencies"],`, 1)
	document = strings.Replace(document, `"selectors": {"filename": "actionlint.sh"}`, `"selectors":{"documents":["workflow","action"]}`, 1)
	document = strings.Replace(document, `"preparation": "kjanat:actionlint/shell-preparation@0.3.0"`, `"preparation":"generic"`, 1)
	manifest, err := manifests.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Role != manifests.RoleDocument || len(manifest.Rule.RequiredInputs) != 2 || manifest.Rule.RequiredInputs[1] != rulemeta.Dependencies || len(manifest.Selectors.Documents) != 2 {
		t.Fatal("document facts or selectors lost", manifest)
	}
	for name, invalid := range map[string]string{
		"root-requirements": strings.Replace(document, `"role": "document"`, `"role":"document","required_inputs":["source"]`, 1),
		"script-role":       strings.Replace(document, `"role": "document"`, `"role":"script"`, 1),
		"unknown-fact":      strings.Replace(document, `"dependencies"`, `"future"`, 1),
		"duplicate-fact":    strings.Replace(document, `"source","dependencies"`, `"source","source"`, 1),
		"unknown-selector":  strings.Replace(document, `["workflow","action"]`, `["unknown"]`, 1),
		"script-selector":   strings.Replace(document, `"documents":["workflow","action"]`, `"documents":["workflow"],"shells":["bash"]`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := manifests.Parse([]byte(invalid)); err == nil {
				t.Fatal("invalid document declaration accepted")
			}
		})
	}
}

func TestInvocationDefinitionManifest(t *testing.T) {
	t.Parallel()
	definition := `"invocation_definitions":[{"id":"example/python-wrapper","executables":["actions-shell"],"prefix":["python"],"delegate":"python-plugin/python"}],`
	document := strings.Replace(minimal, `"schema_version": 1,`, `"schema_version": 1,`+definition, 1)
	manifest, err := manifests.Parse([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.InvocationDefinitions) != 1 || manifest.InvocationDefinitions[0].Delegate != "python-plugin/python" {
		t.Fatalf("definitions: %+v", manifest.InvocationDefinitions)
	}
	for _, invalid := range []string{
		strings.Replace(document, `"delegate":"python-plugin/python"`, `"options":[{"short":"xx"}]`, 1),
		strings.Replace(document, `"delegate":"python-plugin/python"`, `"options":[{"short":"o","group":"set"}]`, 1),
	} {
		if _, err := manifests.Parse([]byte(invalid)); err == nil {
			t.Fatal("invalid option definition accepted")
		}
	}
}
