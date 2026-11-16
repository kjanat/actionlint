package calls

import (
	"strings"
	"testing"

	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestParseMetadataRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "input",
			src:  "on:\n  workflow_call:\n    inputs:\n      Name: {type: string}\n      name: {type: number}\n",
			want: `line:5, column:7: key "name" is duplicated in "inputs" section. previously defined at line:4,col:7. note that this key is case insensitive`,
		},
		{
			name: "output",
			src:  "on:\n  workflow_call:\n    outputs:\n      Result: {value: first}\n      result: {value: second}\n",
			want: `line:5, column:7: key "result" is duplicated in "outputs" section`,
		},
		{
			name: "secret",
			src:  "on:\n  workflow_call:\n    secrets:\n      Token: {required: false}\n      token: {required: true}\n",
			want: `line:5, column:7: key "token" is duplicated in "secrets" section`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMetadata([]byte(tc.src))
			if got != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("metadata = %+v, error = %v, want error containing %q", got, err, tc.want)
			}
		})
	}
}

func TestParseMetadataAliasedDeclarations(t *testing.T) {
	t.Parallel()
	src := `on:
  workflow_call:
    inputs:
      definition: &definition {required: true, type: boolean}
      first: *definition
      second: *definition
    secrets:
      definition: &secret {required: true}
      copied: *secret
      empty:
    outputs:
      copied: &output {value: ignored}
      alias: *output
`

	got, err := ParseMetadata([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"definition", "first", "second"} {
		in, ok := got.Inputs[name]
		if !ok || !in.Required || in.Type != workflowlang.WorkflowCallEventInputTypeBoolean {
			t.Errorf("input %q = %#v, want required boolean", name, in)
		}
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
	for _, name := range []string{"copied", "alias"} {
		if _, ok := got.Outputs[name]; !ok {
			t.Errorf("output %q missing from %#v", name, got.Outputs)
		}
	}
}

func TestParseMetadataAliasedInputsMap(t *testing.T) {
	t.Parallel()
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
	if shared, ok := got.Inputs["shared"]; len(got.Inputs) != 1 || !ok || shared.Type != workflowlang.WorkflowCallEventInputTypeString {
		t.Fatalf("inputs = %#v, want alias-expanded Shared string input", got.Inputs)
	}
}

// GitHub Actions does not resolve YAML merge keys, so a callee that uses one
// in its workflow_call declarations cannot be called.
func TestParseMetadataRejectsMergeKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "input definition",
			src: `on:
  workflow_call:
    inputs:
      single:
        <<: &single_base {required: true, type: boolean}
        type: number
`,
			want: `line:5, column:9: GitHub Actions does not support YAML merge key "<<"`,
		},
		{
			name: "workflow_call fields",
			src: `base: &base
  inputs:
    inherited: {type: string}
on:
  workflow_call:
    <<: *base
`,
			want: `line:6, column:5: GitHub Actions does not support YAML merge key "<<"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMetadata([]byte(tc.src))
			if got != nil || err == nil || err.Error() != tc.want {
				t.Fatalf("metadata = %+v, error = %v, want %q", got, err, tc.want)
			}
		})
	}
}

func TestParseMetadataNonMappingErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field string
		want  string
	}{
		{"inputs", `line:3, column:13: "inputs" section is sequence node but mapping node is expected`},
		{"outputs", `line:3, column:14: "outputs" section is sequence node but mapping node is expected`},
		{"secrets", `line:3, column:14: "secrets" section is sequence node but mapping node is expected`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()
			src := "on:\n  workflow_call:\n    " + tc.field + ": []\n"
			_, err := ParseMetadata([]byte(src))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
