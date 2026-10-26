package action

import (
	"errors"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
)

func TestAliasKeysDecodeOriginalMetadataValues(t *testing.T) {
	const source = `nameKey: &name name
usingKey: &using using
iconKey: &icon icon
requiredKey: &required required
? *name
: example
runs:
  ? *using
  : node24
  main: index.js
branding:
  ? *icon
  : book
inputs:
  token:
    ? *required
    : true
`
	metadata, err := Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "example" || metadata.Runs.Using != "node24" || metadata.Runs.Main != "index.js" || metadata.Branding.Icon != "book" || metadata.Inputs["token"] == nil || !metadata.Inputs["token"].Required {
		t.Fatalf("alias-key metadata: %+v", metadata)
	}
}

func TestNormalizedDuplicateFieldPreservesFirstAndOtherMetadata(t *testing.T) {
	metadata, err := Parse("action.yml", []byte("name: first\nName: second\nruns: {using: node24, main: index.js}\n"))
	if metadata.Name != "first" || metadata.Runs.Using != "node24" {
		t.Fatalf("partial metadata lost: %+v", metadata)
	}
	decoded, ok := errors.AsType[*codec.DecodeError](err)
	if !ok || len(decoded.Errors) != 1 || decoded.Errors[0].Mark.Line != 2 || decoded.Errors[0].Mark.Column != 1 {
		t.Fatalf("duplicate error = %#v", err)
	}
	const want = "yaml: construct errors: line 2: duplicate action metadata key \"Name\""
	if err.Error() != want {
		t.Fatalf("duplicate error = %q, want %q", err.Error(), want)
	}
}

func TestMetadataDecoderErrorsNameSchemaFields(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"scalar\n", "yaml: construct errors: line 1: yaml: action metadata must be mapping node but scalar node was found at line:1, col:1"},
		{"runs: scalar\n", "yaml: construct errors: line 1: yaml: runs must be mapping node but scalar node was found at line:1, col:7"},
		{"inputs: {token: scalar}\n", "yaml: construct errors: line 1: yaml: input definition must be mapping node but scalar node was found at line:1, col:17"},
	} {
		_, err := Parse("action.yml", []byte(tc.source))
		if err == nil || err.Error() != tc.want {
			t.Errorf("Parse(%q) = %v, want %q", tc.source, err, tc.want)
		}
	}
}
