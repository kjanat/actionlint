package action

import (
	"errors"
	"fmt"
	"strings"
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

func TestAliasedRunsKeepsExpansionBudget(t *testing.T) {
	var fields strings.Builder
	for i := range 650 {
		fmt.Fprintf(&fields, "key%d: value, ", i)
	}
	runs := "{using: docker, image: Dockerfile, env: {" + strings.TrimSuffix(fields.String(), ", ") + "}}"
	for _, tc := range []struct {
		name   string
		source string
		limit  bool
	}{
		{"literal", "runs: " + runs + "\n", false},
		{"alias", "template: &runs " + runs + "\nruns: *runs\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plain struct {
				Runs struct {
					Using string            `yaml:"using"`
					Image string            `yaml:"image"`
					Env   map[string]string `yaml:"env"`
				} `yaml:"runs"`
			}
			baseline := codec.Unmarshal([]byte(tc.source), &plain)
			if tc.limit {
				if baseline == nil || !strings.Contains(baseline.Error(), "excessive aliasing") {
					t.Fatalf("plain decode = %v, want alias limit", baseline)
				}
			} else if baseline != nil {
				t.Fatal(baseline)
			}
			metadata, err := Parse("action.yml", []byte(tc.source))
			if tc.limit {
				if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
					t.Fatalf("Parse = %v, want alias limit through runs", err)
				}
				return
			}
			if err != nil || len(metadata.Runs.Env) != len(plain.Runs.Env) {
				t.Fatalf("Parse decoded %d env entries, error %v; want %d entries", len(metadata.Runs.Env), err, len(plain.Runs.Env))
			}
		})
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
