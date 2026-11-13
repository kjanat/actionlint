package configschema_test

import (
	"errors"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/manifests/configschema"
)

func TestCompileRefusesExternalReferences(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestValidationErrorsRetainDetachedPaths(t *testing.T) {
	t.Parallel()
	schema, err := configschema.Compile([]byte(`{"type":"object","additionalProperties":false,"properties":{"count":{"type":"integer"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ document, path, unknown string }{
		{`{"count":"three"}`, "/count", ""},
		{`{"extra/key":true}`, "", "extra/key"},
	} {
		err := schema.Validate([]byte(test.document))
		failure, ok := errors.AsType[*configschema.ValidationError](err)
		if !ok {
			t.Fatalf("missing structured schema error: %v", err)
		}
		found := false
		for _, unit := range failure.Failures {
			if unit.Path != test.path {
				continue
			}
			if test.unknown != "" && (len(unit.UnknownKeys) != 1 || unit.UnknownKeys[0] != test.unknown) {
				continue
			}
			found = true
		}
		if !found {
			t.Fatalf("paths: %#v, want %q and unknown key %q", failure.Failures, test.path, test.unknown)
		}
	}
}
