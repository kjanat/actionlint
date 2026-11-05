package calls

import (
	"strings"
	"testing"
)

func TestMetadataDecoderDuplicateAndAliasKeys(t *testing.T) {
	src := `on:
  &event workflow_call:
    inputs:
      Name: {type: string}
      Name: {type: boolean}
      name: {type: number}
      &input_key Aliased: {type: boolean}
      *input_key: {type: number}
    outputs:
      Result: [ignored, output, payload]
      result: {value: second}
      Other: ignored-scalar-output-payload
    secrets:
      Token: {required: false}
      token: {required: true}
  *event:
    inputs:
      ignored: {type: string}
`

	got, err := ParseMetadata([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, hasName := got.Inputs["name"]
	_, hasAliased := got.Inputs["aliased"]
	if len(got.Inputs) != 2 || !hasName || !hasAliased {
		t.Fatalf("inputs = %#v, want case-insensitive keys name and aliased", got.Inputs)
	}
	if in := got.Inputs["name"]; in.Name != "name" || in.Required || in.Type != InputTypeNumber {
		t.Errorf("last duplicate input = %#v, want name, optional number", in)
	}
	if in := got.Inputs["aliased"]; in.Name != "Aliased" || in.Required || in.Type != InputTypeNumber {
		t.Errorf("alias-key input = %#v, want resolved key spelling Aliased and last number value", in)
	}
	_, hasOther := got.Outputs["other"]
	if len(got.Outputs) != 2 || got.Outputs["result"].Name != "result" || !hasOther {
		t.Errorf("outputs = %#v, want last case-insensitive result key and ignored arbitrary payload", got.Outputs)
	}
	if len(got.Secrets) != 1 || got.Secrets["token"].Name != "token" || !got.Secrets["token"].Required {
		t.Errorf("secrets = %#v, want last case-insensitive token definition", got.Secrets)
	}
}

func TestMetadataDecoderAliasesAndNullBodies(t *testing.T) {
	src := `on:
  workflow_call:
    inputs:
      definition: &definition {required: true, type: boolean}
      first: *definition
      second: *definition
      empty:
    secrets:
      definition: &secret {required: true}
      copied: *secret
      empty:
    outputs:
      copied: &output {value: ignored}
      alias: *output
      empty:
`

	got, err := ParseMetadata([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"definition", "first", "second"} {
		in, ok := got.Inputs[name]
		if !ok || !in.Required || in.Type != InputTypeBoolean {
			t.Errorf("input %q = %#v, want required boolean", name, in)
		}
	}
	if in, ok := got.Inputs["empty"]; !ok || in.Required || in.Type != InputTypeAny {
		t.Errorf("null input = %#v, want optional any", in)
	}
	for _, name := range []string{"definition", "copied"} {
		secret, ok := got.Secrets[name]
		if !ok || !secret.Required {
			t.Errorf("secret %q = %#v, want required", name, secret)
		}
	}
	if secret, ok := got.Secrets["empty"]; !ok || secret.Required {
		t.Errorf("null secret = %#v, want optional", secret)
	}
	for _, name := range []string{"copied", "alias", "empty"} {
		if _, ok := got.Outputs[name]; !ok {
			t.Errorf("output %q missing from %#v", name, got.Outputs)
		}
	}
}

func TestMetadataDecoderAliasedInputsMap(t *testing.T) {
	src := `definitions: &definitions
  Shared:
    type: string
on:
  workflow_call:
    inputs: *definitions
`

	got, err := ParseMetadata([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if shared, ok := got.Inputs["shared"]; len(got.Inputs) != 1 || !ok || shared.Type != InputTypeString {
		t.Fatalf("inputs = %#v, want alias-expanded Shared string input", got.Inputs)
	}
}

func TestMetadataDecoderMergeKeys(t *testing.T) {
	t.Run("definition maps use upstream single and sequence merge semantics", func(t *testing.T) {
		src := `on:
  workflow_call:
    inputs:
      single:
        <<: &single_base {required: true, type: boolean}
        type: number
      sequence_first:
        <<: [&first {required: true, type: boolean}, &second {required: false, type: number}]
      sequence_second:
        <<: [*second, *first]
`
		got, err := ParseMetadata([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]struct {
			required bool
			typ      InputType
		}{
			"single":          {true, InputTypeNumber},
			"sequence_first":  {true, InputTypeBoolean},
			"sequence_second": {false, InputTypeNumber},
		}
		for name, expected := range want {
			in, ok := got.Inputs[name]
			if !ok || in.Required != expected.required || in.Type != expected.typ {
				t.Errorf("input %q = %#v, want required=%t type=%s", name, in, expected.required, expected.typ)
			}
		}
	})

	t.Run("workflow call fields use sequence merge precedence and explicit override", func(t *testing.T) {
		src := `base: &base
  inputs:
    inherited: {type: string}
    overridden: {type: string}
  outputs:
    Overridden: {}
base2: &base2
  inputs:
    second: {type: number}
    overridden: {type: number}
  outputs:
    overridden: {}
on:
  workflow_call:
    <<: [*base, *base2]
    inputs:
      explicit: {type: boolean}
      overridden: {type: boolean}
`
		got, err := ParseMetadata([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		for name, typ := range map[string]InputType{
			"explicit":   InputTypeBoolean,
			"overridden": InputTypeBoolean,
		} {
			if in, ok := got.Inputs[name]; !ok || in.Type != typ {
				t.Errorf("input %q = %#v, want type %s", name, in, typ)
			}
		}
		if len(got.Inputs) != 2 {
			t.Errorf("inputs = %#v, want explicit Inputs map to replace merged map", got.Inputs)
		}
		if len(got.Outputs) != 1 || got.Outputs["overridden"].Name != "Overridden" {
			t.Errorf("outputs = %#v, want first merged declaration spelling Overridden", got.Outputs)
		}
	})

	t.Run("merged call fields apply when no explicit field replaces them", func(t *testing.T) {
		src := `base: &base
  inputs:
    inherited: {type: string}
  outputs:
    inherited: ignored-output-payload
on:
  workflow_call:
    <<: *base
`
		got, err := ParseMetadata([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if in, ok := got.Inputs["inherited"]; !ok || in.Type != InputTypeString {
			t.Errorf("merged input = %#v, want string input", in)
		}
		if out := got.Outputs["inherited"]; out.Name != "inherited" {
			t.Errorf("merged output = %#v, want key-only output metadata", out)
		}
	})
}

func TestMetadataDecoderNonMappingErrors(t *testing.T) {
	for _, tc := range []struct {
		field string
		want  string
	}{
		{"inputs", "on.workflow_call.inputs must be mapping node"},
		{"outputs", "on.workflow_call.outputs must be mapping node"},
		{"secrets", "on.workflow_call.secrets must be mapping node"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			src := "on:\n  workflow_call:\n    " + tc.field + ": []\n"
			_, err := ParseMetadata([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}
