package action

import (
	"strings"
	"testing"

	reference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"
)

func TestDecodePartialNullAndEmptyMetadata(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"", "null\n"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			metadata, err := Parse("action.yml", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if metadata.Root != nil {
				t.Fatalf("metadata root = %#v, want nil", metadata.Root)
			}
		})
	}
}

func TestDecodePartialNullInputsOutputsAndDefinition(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte("inputs: null\noutputs: null\n"))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Inputs != nil || metadata.Outputs != nil {
		t.Fatalf("null sections = inputs %#v, outputs %#v", metadata.Inputs, metadata.Outputs)
	}

	metadata, err = Parse("action.yml", []byte("inputs:\n  token: null\n"))
	if err != nil {
		t.Fatal(err)
	}
	if input := metadata.Inputs["token"]; input == nil || input.Name != "token" || input.Required {
		t.Fatalf("null input definition = %#v", input)
	}
}

func TestDecodePartialDuplicateInputKeepsAllDefaults(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte(`inputs:
  before:
    default: first
  token:
    default: second
  TOKEN:
    default: duplicate
  after:
    default: fourth
`))
	if err == nil {
		t.Fatal("duplicate input ID returned no error")
	}
	if metadata.Inputs != nil {
		t.Errorf("duplicate input IDs left partial inputs: %#v", metadata.Inputs)
	}
	if got := inputDefaultNames(metadata.InputDefaults); strings.Join(got, ",") != "before,token,TOKEN,after" {
		t.Errorf("defaults = %v", got)
	}
}

func TestDecodePartialNonMappingInputKeepsLaterDefaults(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte(`inputs:
  invalid: true
  after:
    default: later
`))
	if err == nil {
		t.Fatal("non-mapping input definition returned no error")
	}
	if metadata.Inputs != nil {
		t.Errorf("invalid input definition left partial inputs: %#v", metadata.Inputs)
	}
	if got := inputDefaultNames(metadata.InputDefaults); strings.Join(got, ",") != "after" {
		t.Errorf("defaults = %v", got)
	}
}

func TestDecodePartialDuplicateDefinitionFieldsKeepsDefaults(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte(`inputs:
  token:
    default: first
    DEFAULT: second
`))
	if err == nil {
		t.Fatal("duplicate definition field returned no error")
	}
	if metadata.Inputs != nil {
		t.Errorf("duplicate definition fields left partial inputs: %#v", metadata.Inputs)
	}
	if got := inputDefaultNames(metadata.InputDefaults); strings.Join(got, ",") != "token,token" {
		t.Errorf("defaults = %v", got)
	}
}

func TestDecodeIgnoresUnknownInputMetadata(t *testing.T) {
	t.Parallel()
	// actions/runner 1ba885c2065fd9be317808ab3fd406f392635cf7:
	// action_yaml.json permits arbitrary values for extra input properties.
	const source = `name: extensions
description: Input metadata extensions
runs: {using: node24, main: index.js}
inputs:
  token:
    type: boolean
    extraMapping: {format: [true, false]}
    required: true
    default: false
`
	metadata, err := Parse("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if input := metadata.Inputs["token"]; input == nil || input.Name != "token" || input.Required {
		t.Fatalf("input metadata was lost: %#v", metadata.Inputs)
	}
	if problems := Validate(metadata).Problems; len(problems) != 0 {
		t.Fatalf("valid extension metadata was rejected: %+v", problems)
	}
	reference.CheckAction(t, source, true)
}

func TestDecodePartialOutputIgnoresRecursiveAliasPayload(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte(`outputs:
  result: &loop {self: *loop}
`))
	if err != nil {
		t.Fatal(err)
	}
	if output := metadata.Outputs["result"]; output == nil || output.Name != "result" {
		t.Fatalf("output declaration = %#v", output)
	}
}

func TestDecodePartialDuplicateOutputDiscardsSection(t *testing.T) {
	t.Parallel()
	metadata, err := Parse("action.yml", []byte("outputs:\n  result: {}\n  RESULT: {}\nname: example\n"))
	if err == nil || !strings.Contains(err.Error(), `output "RESULT" is duplicated`) {
		t.Fatalf("duplicate output error = %v", err)
	}
	if metadata.Outputs != nil {
		t.Errorf("duplicate output IDs left partial outputs: %#v", metadata.Outputs)
	}
	if metadata.Name != "example" {
		t.Errorf("name following invalid outputs = %q", metadata.Name)
	}
}

func inputDefaultNames(defaults []*KeyValue) []string {
	names := make([]string, len(defaults))
	for i, item := range defaults {
		names[i] = item.Name
	}
	return names
}
