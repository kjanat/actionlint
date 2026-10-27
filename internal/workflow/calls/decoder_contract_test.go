package calls

import (
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
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
	if len(got.Inputs) != 2 || got.Inputs["name"] == nil || got.Inputs["aliased"] == nil {
		t.Fatalf("inputs = %#v, want case-insensitive keys name and aliased", got.Inputs)
	}
	if in := got.Inputs["name"]; in.Name != "name" || in.Required || in.Type != (exprlang.NumberType{}) {
		t.Errorf("last duplicate input = %#v, want name, optional number", in)
	}
	if in := got.Inputs["aliased"]; in.Name != "Aliased" || in.Required || in.Type != (exprlang.NumberType{}) {
		t.Errorf("alias-key input = %#v, want resolved key spelling Aliased and last number value", in)
	}
	if len(got.Outputs) != 2 || got.Outputs["result"] == nil || got.Outputs["result"].Name != "result" || got.Outputs["other"] == nil {
		t.Errorf("outputs = %#v, want last case-insensitive result key and ignored arbitrary payload", got.Outputs)
	}
	if len(got.Secrets) != 1 || got.Secrets["token"] == nil || got.Secrets["token"].Name != "token" || !got.Secrets["token"].Required {
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
		in := got.Inputs[name]
		if in == nil || !in.Required || in.Type != (exprlang.BoolType{}) {
			t.Errorf("input %q = %#v, want required boolean", name, in)
		}
	}
	if in := got.Inputs["empty"]; in == nil || in.Required || in.Type != (exprlang.AnyType{}) {
		t.Errorf("null input = %#v, want optional any", in)
	}
	for _, name := range []string{"definition", "copied"} {
		secret := got.Secrets[name]
		if secret == nil || !secret.Required {
			t.Errorf("secret %q = %#v, want required", name, secret)
		}
	}
	if secret := got.Secrets["empty"]; secret == nil || secret.Required {
		t.Errorf("null secret = %#v, want optional", secret)
	}
	for _, name := range []string{"copied", "alias", "empty"} {
		if got.Outputs[name] == nil {
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
	if len(got.Inputs) != 1 || got.Inputs["shared"] == nil || got.Inputs["shared"].Type != (exprlang.StringType{}) {
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
			typ      exprlang.Type
		}{
			"single":          {true, exprlang.NumberType{}},
			"sequence_first":  {true, exprlang.BoolType{}},
			"sequence_second": {false, exprlang.NumberType{}},
		}
		for name, expected := range want {
			in := got.Inputs[name]
			if in == nil || in.Required != expected.required || in.Type != expected.typ {
				t.Errorf("input %q = %#v, want required=%t type=%T", name, in, expected.required, expected.typ)
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
		for name, typ := range map[string]exprlang.Type{
			"explicit":   exprlang.BoolType{},
			"overridden": exprlang.BoolType{},
		} {
			if in := got.Inputs[name]; in == nil || in.Type != typ {
				t.Errorf("input %q = %#v, want type %T", name, in, typ)
			}
		}
		if len(got.Inputs) != 2 {
			t.Errorf("inputs = %#v, want explicit Inputs map to replace merged map", got.Inputs)
		}
		if len(got.Outputs) != 1 || got.Outputs["overridden"] == nil || got.Outputs["overridden"].Name != "Overridden" {
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
		if in := got.Inputs["inherited"]; in == nil || in.Type != (exprlang.StringType{}) {
			t.Errorf("merged input = %#v, want string input", in)
		}
		if out := got.Outputs["inherited"]; out == nil || out.Name != "inherited" {
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
