package configschema_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema"
)

func TestCompileRefusesExternalReferences(t *testing.T) {
	for _, document := range []string{
		`{"$ref": "https://example.invalid/schema.json"}`,
		`{"properties": {"rc": {"$ref": "file:///etc/schema.json"}}}`,
	} {
		if _, err := configschema.Compile([]byte(document)); err == nil {
			t.Fatalf("compiled %s", document)
		}
	}
	if _, err := configschema.Compile([]byte(`{"type": `)); err == nil {
		t.Fatal("compiled malformed JSON")
	}
}

func TestValidateNamesFailingLocations(t *testing.T) {
	schema, err := configschema.Compile([]byte(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {"select": {"type": "array", "items": {"type": "string", "minLength": 1}}}
}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate([]byte(`{"select": ["F", "B"]}`)); err != nil {
		t.Fatal(err)
	}
	for document, want := range map[string]string{
		`{"select": ["F", ""]}`: "at /select/1: minLength",
		`{"typo": true}`:        "at /: additional properties 'typo' not allowed",
		`[]`:                    "at /: got array, want object",
	} {
		err := schema.Validate([]byte(document))
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "actionlint.invalid") {
			t.Errorf("%s: got %v, want %q", document, err, want)
		}
	}
	if err := schema.Validate([]byte(`{`)); err == nil {
		t.Fatal("validated malformed JSON")
	}
}
