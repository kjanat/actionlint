package actionmeta

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func testDiffActionMetadata(t *testing.T, want, have *Metadata, opts ...cmp.Option) {
	t.Helper()
	opts = append(opts, cmpopts.IgnoreFields(Metadata{}, "Provenance", "SourceMapped", "Directory", "Filename", "Source", "Root"))
	if diff := cmp.Diff(want, have, opts...); diff != "" {
		t.Fatal(diff)
	}
}

func TestActionMetadataYAMLUnmarshalOK(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what  string
		input string
		want  Metadata
	}{
		{
			what:  "no input and no output",
			input: `name: Test`,
			want: Metadata{
				Name: "Test",
			},
		},
		{
			what: "inputs",
			input: `name: Test
inputs:
  input1:
    description: test
  input2:
    description: test
    required: false
  input3:
    description: test
    required: true
    default: 'default'
  input4:
    description: test
    required: false
    default: 'default'
  input5:
    description: test
    required: true
  input_snake-case:
    description: test
  camelCaseInput:
    description: test
`,
			want: Metadata{
				Name: "Test",
				Inputs: calls.Inputs{
					"input1":           {Name: "input1", Required: false, Deprecated: false, DeprecationMessage: ""},
					"input2":           {Name: "input2", Required: false, Deprecated: false, DeprecationMessage: ""},
					"input3":           {Name: "input3", Required: false, Deprecated: false, DeprecationMessage: ""},
					"input4":           {Name: "input4", Required: false, Deprecated: false, DeprecationMessage: ""},
					"input5":           {Name: "input5", Required: true, Deprecated: false, DeprecationMessage: ""},
					"input_snake-case": {Name: "input_snake-case", Required: false, Deprecated: false, DeprecationMessage: ""},
					"camelcaseinput":   {Name: "camelCaseInput", Required: false, Deprecated: false, DeprecationMessage: ""},
				},
				InputDefaults: []*KeyValue{
					{Name: "input3", Value: ExprString{Value: "default", Position: sourcetext.Position{Line: 11, Column: 14}}},
					{Name: "input4", Value: ExprString{Value: "default", Position: sourcetext.Position{Line: 15, Column: 14}}},
				},
			},
		},
		{
			what: "input default expressions are captured with position",
			input: `name: Test
inputs:
  token:
    description: test
    default: ${{ secrets.TOKEN }}
  region:
    description: test
    default: "${{ vars.REGION }}"
  flag:
    description: test
    default: true
  plain:
    description: test
`,
			want: Metadata{
				Name: "Test",
				Inputs: calls.Inputs{
					"token":  {Name: "token", Required: false, Deprecated: false, DeprecationMessage: ""},
					"region": {Name: "region", Required: false, Deprecated: false, DeprecationMessage: ""},
					"flag":   {Name: "flag", Required: false, Deprecated: false, DeprecationMessage: ""},
					"plain":  {Name: "plain", Required: false, Deprecated: false, DeprecationMessage: ""},
				},
				InputDefaults: []*KeyValue{
					{Name: "token", Value: ExprString{Value: "${{ secrets.TOKEN }}", Position: sourcetext.Position{Line: 5, Column: 14}}},
					{Name: "region", Value: ExprString{Value: "${{ vars.REGION }}", Position: sourcetext.Position{Line: 8, Column: 14}}},
				},
			},
		},
		{
			what: "outputs",
			input: `name: Test
outputs:
  output1:
    description: test
  output2:
    description: test
`,
			want: Metadata{
				Name: "Test",
				Outputs: calls.Outputs{
					"output1": {Name: "output1"},
					"output2": {Name: "output2"},
				},
			},
		},
		{
			what: "deprecated inputs",
			input: `name: Test
inputs:
  input1:
    description: test
    deprecationMessage: foo
  input2:
    description: test
    deprecationMessage: ' foo bar '
    required: true
  input3:
    description: test
    deprecationMessage:
  input4:
    description: test
    deprecationMessage: ''
`,
			want: Metadata{
				Name: "Test",
				Inputs: calls.Inputs{
					"input1": {Name: "input1", Required: false, Deprecated: true, DeprecationMessage: "foo"},
					"input2": {Name: "input2", Required: true, Deprecated: true, DeprecationMessage: "foo bar"},
					"input3": {Name: "input3", Required: false, Deprecated: true, DeprecationMessage: ""},
					"input4": {Name: "input4", Required: false, Deprecated: true, DeprecationMessage: ""},
				},
			},
		},
		{
			what: "composite steps capture expression fields",
			input: `name: Test
runs:
  using: composite
  steps:
    - if: runner.os == 'Linux'
      run: echo ${{ secrets.TOKEN }}
      shell: bash
      working-directory: sub
      name: step one
      env:
        FOO: ${{ vars.BAR }}
    - uses: actions/checkout@v5
      with:
        token: ${{ secrets.GH }}
`,
			want: Metadata{
				Name: "Test",
				Runs: Runs{
					Using: "composite",
					Steps: actionCompositeSteps{
						{
							Position: sourcetext.Position{Line: 5, Column: 7}, IsMapping: true,
							Keys:             []string{"if", "run", "shell", "working-directory", "name", "env"},
							If:               &ExprString{Value: "runner.os == 'Linux'", Position: sourcetext.Position{Line: 5, Column: 11}},
							Run:              &ExprString{Value: "echo ${{ secrets.TOKEN }}", Position: sourcetext.Position{Line: 6, Column: 12}},
							WorkingDirectory: &ExprString{Value: "sub", Position: sourcetext.Position{Line: 8, Column: 26}},
							StepName:         &ExprString{Value: "step one", Position: sourcetext.Position{Line: 9, Column: 13}},
							Env: []*KeyValue{
								{Name: "FOO", Value: ExprString{Value: "${{ vars.BAR }}", Position: sourcetext.Position{Line: 11, Column: 14}}},
							},
						},
						{
							Position: sourcetext.Position{Line: 12, Column: 7}, IsMapping: true,
							Keys: []string{"uses", "with"},
							Uses: new("actions/checkout@v5"),
							With: []*KeyValue{
								{Name: "token", Value: ExprString{Value: "${{ secrets.GH }}", Position: sourcetext.Position{Line: 14, Column: 16}}},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			have, err := ParseActionMetadata("action.yml", []byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			testDiffActionMetadata(t, &tc.want, have, cmpopts.IgnoreFields(CompositeStep{}, "Shell", "ContinueOnError", "WithExpression", "EnvExpression", "ID", "Node"))
		})
	}
}

func TestActionMetadataYAMLUnmarshalError(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		what  string
		input string
		want  string
	}{
		{
			what: "invalid inputs",
			input: `name: Test
inputs: "foo"`,
			want: "inputs must be mapping node",
		},
		{
			what: "invalid inputs.*",
			input: `name: Test
inputs:
  input1: "foo"`,
			want: "input definition must be mapping node but scalar node was found at line:3, col:11",
		},
		{
			what: "invalid outputs",
			input: `name: Test
outputs: "foo"`,
			want: "outputs must be mapping node",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			_, err := ParseActionMetadata("action.yml", []byte(tc.input))
			if err == nil {
				t.Fatal("error did not occur")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%q is not contained in error message %q", tc.want, msg)
			}
		})
	}
}

func TestActionMetadataAcceptsUnknownInputFields(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile("testdata/metadata/lighthouse.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, using := range []string{"node16", "node20"} {
		t.Run(using, func(t *testing.T) {
			t.Parallel()
			src := bytes.ReplaceAll(fixture, []byte("node16"), []byte(using))
			md, err := ParseActionMetadata("action.yml", src)
			if err != nil {
				t.Fatalf("runner ignores the upstream description typo: %v", err)
			}
			if md.Name != "Lighthouse CI Action" || md.Runs.Using != using || md.Runs.Main != "dist/index.js" {
				t.Fatalf("decoded metadata was lost: %+v", md)
			}
			if _, ok := md.Inputs["temporarypublicstorage"]; !ok {
				t.Fatalf("decoded inputs were lost: %+v", md)
			}
			if _, ok := md.Outputs["links"]; !ok {
				t.Fatalf("decoded inputs or outputs were lost: %+v", md)
			}
			if len(md.InputDefaults) != 1 || md.InputDefaults[0].Value.Position.Line != 5 {
				t.Fatalf("positioned default was lost: %+v", md.InputDefaults)
			}
		})
	}
}
