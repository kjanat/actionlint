package action

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"actionlint.kjanat.dev/internal/yaml"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func testDiffActionMetadata(t *testing.T, want, have *Metadata, opts ...cmp.Option) {
	t.Helper()
	opts = append(opts, cmpopts.IgnoreUnexported(Metadata{}), cmpopts.IgnoreFields(Metadata{}, "SourceMapped", "Directory", "Filename", "Source", "Root"))
	if diff := cmp.Diff(want, have, opts...); diff != "" {
		t.Fatal(diff)
	}
}
func TestActionMetadataYAMLUnmarshalOK(t *testing.T) {
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
				Inputs: Inputs{
					"input1":           {"input1", false, false, ""},
					"input2":           {"input2", false, false, ""},
					"input3":           {"input3", false, false, ""},
					"input4":           {"input4", false, false, ""},
					"input5":           {"input5", true, false, ""},
					"input_snake-case": {"input_snake-case", false, false, ""},
					"camelcaseinput":   {"camelCaseInput", false, false, ""},
				},
				InputDefaults: []*KeyValue{
					{Name: "input3", Value: ExprString{Value: "default", Line: 11, Column: 14}},
					{Name: "input4", Value: ExprString{Value: "default", Line: 15, Column: 14}},
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
				Inputs: Inputs{
					"token":  {"token", false, false, ""},
					"region": {"region", false, false, ""},
					"flag":   {"flag", false, false, ""},
					"plain":  {"plain", false, false, ""},
				},
				InputDefaults: []*KeyValue{
					{Name: "token", Value: ExprString{Value: "${{ secrets.TOKEN }}", Line: 5, Column: 14}},
					{Name: "region", Value: ExprString{Value: "${{ vars.REGION }}", Line: 8, Column: 14}},
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
				Outputs: Outputs{
					"output1": {"output1"},
					"output2": {"output2"},
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
				Inputs: Inputs{
					"input1": {"input1", false, true, "foo"},
					"input2": {"input2", true, true, "foo bar"},
					"input3": {"input3", false, true, ""},
					"input4": {"input4", false, true, ""},
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
							Line: 5, Column: 7, IsMapping: true,
							Keys:             []string{"if", "run", "shell", "working-directory", "name", "env"},
							If:               &ExprString{Value: "runner.os == 'Linux'", Line: 5, Column: 11},
							Run:              &ExprString{Value: "echo ${{ secrets.TOKEN }}", Line: 6, Column: 12},
							WorkingDirectory: &ExprString{Value: "sub", Line: 8, Column: 26},
							StepName:         &ExprString{Value: "step one", Line: 9, Column: 13},
							Env: []*KeyValue{
								{Name: "FOO", Value: ExprString{Value: "${{ vars.BAR }}", Line: 11, Column: 14}},
							},
						},
						{
							Line: 12, Column: 7, IsMapping: true,
							Keys: []string{"uses", "with"},
							Uses: new("actions/checkout@v5"),
							With: []*KeyValue{
								{Name: "token", Value: ExprString{Value: "${{ secrets.GH }}", Line: 14, Column: 16}},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			var have Metadata
			if err := yaml.Unmarshal([]byte(tc.input), &have); err != nil {
				t.Fatal(err)
			}
			testDiffActionMetadata(t, &tc.want, &have, cmpopts.IgnoreFields(Step{}, "Shell", "ContinueOnError", "WithExpression", "EnvExpression", "ID", "Node"))
		})
	}
}

func TestActionMetadataYAMLUnmarshalError(t *testing.T) {
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
			want: "into action.actionInputMetadata",
		},
		{
			what: "invalid outputs",
			input: `name: Test
outputs: "foo"`,
			want: "outputs must be mapping node",
		},
		{
			what: "unknown input metadata",
			input: `name: Test
inputs:
  input1:
    foo: bar`,
			want: "unexpected key \"foo\" for definition of input \"input1\"",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.what, func(t *testing.T) {
			var data Metadata
			err := yaml.Unmarshal([]byte(tc.input), &data)
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

func TestActionMetadataPreservesPartialDecode(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/action_metadata_lighthouse.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, using := range []string{"node16", "node20"} {
		t.Run(using, func(t *testing.T) {
			src := bytes.ReplaceAll(fixture, []byte("node16"), []byte(using))
			var md Metadata
			err := yaml.Unmarshal(src, &md)
			if err == nil || !strings.Contains(err.Error(), "unexpected key") || !strings.Contains(err.Error(), `input "temporaryPublicStorage"`) {
				t.Fatalf("expected the upstream typo error, got %v", err)
			}
			if md.Name != "Lighthouse CI Action" || md.Runs.Using != using || md.Runs.Main != "dist/index.js" {
				t.Fatalf("partially decoded metadata was lost: %+v", md)
			}
			if md.Inputs["temporarypublicstorage"] == nil || md.Outputs["links"] == nil {
				t.Fatalf("partially decoded inputs or outputs were lost: %+v", md)
			}
			if len(md.InputDefaults) != 1 || md.InputDefaults[0].Value.Line != 5 {
				t.Fatalf("positioned default was lost: %+v", md.InputDefaults)
			}
		})
	}
}
