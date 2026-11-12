package calls

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReusableWorkflowUnmarshalOK(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what string
		src  string
		want *Metadata
	}{
		{
			what: "minimal",
			src: `
			on:
			  workflow_call:
			    inputs:
			      i:
			        type: string
			    outputs:
			      o:
			        value: foo
			    secrets:
			      x:
			`,
			want: &Metadata{
				Inputs: Inputs{
					"i": {"i", false, InputTypeString},
				},
				Outputs: Outputs{
					"o": {"o"},
				},
				Secrets: Secrets{
					"x": {"x", false},
				},
			},
		},
		{
			what: "empty",
			src: `
			on:
			  workflow_call:
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty values",
			src: `
			on:
			  workflow_call:
			    inputs:
			    outputs:
			    secrets:
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "inputs",
			src: `
			on:
			  workflow_call:
			    inputs:
			      a:
			        type: string
			      b:
			        type: number
			      c:
			        type: boolean
			      d:
			      e:
			        type: string
			        required: false
			      f:
			        type: string
			        required: true
			      g:
			        type: string
			        default: abc
			      h:
			        type: string
			        default: abc
			        required: true
			      i:
			        required: true
			`,
			want: &Metadata{
				Inputs: Inputs{
					"a": {"a", false, InputTypeString},
					"b": {"b", false, InputTypeNumber},
					"c": {"c", false, InputTypeBoolean},
					"d": {"d", false, InputTypeAny},
					"e": {"e", false, InputTypeString},
					"f": {"f", true, InputTypeString},
					"g": {"g", false, InputTypeString},
					"h": {"h", false, InputTypeString},
					"i": {"i", true, InputTypeAny},
				},
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "outputs",
			src: `
			on:
			  workflow_call:
			    inputs:
			      i:
			        type: string
			    outputs:
			      o:
			        value: foo
			    secrets:
			      x:
			`,
			want: &Metadata{
				Inputs: Inputs{
					"i": {"i", false, InputTypeString},
				},
				Outputs: Outputs{
					"o": {"o"},
				},
				Secrets: Secrets{
					"x": {"x", false},
				},
			},
		},
		{
			what: "secrets",
			src: `
			on:
			  workflow_call:
			    secrets:
			      x:
			      y:
			        required: false
			      z:
			        required: true
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: Secrets{
					"x": {"x", false},
					"y": {"y", false},
					"z": {"z", true},
				},
			},
		},
		{
			what: "empty event in scalar node",
			src: `
			on: workflow_call
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty event in sequence node",
			src: `
			on: [workflow_call]
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "empty event in sequence node with other events",
			src: `
			on: [pull_request, workflow_call, push]
			`,
			want: &Metadata{
				Inputs:  nil,
				Outputs: nil,
				Secrets: nil,
			},
		},
		{
			what: "upper case",
			src: `
			on:
			  workflow_call:
			    inputs:
			      MY_INPUT1:
			        type: string
			      MY_INPUT2:
			        type: number
			    outputs:
			      MY_OUTPUT1:
			        value: foo
			      MY_OUTPUT2:
			        value: foo
			    secrets:
			      MY_SECRET1:
			      MY_SECRET2:
			        required: true
			`,
			want: &Metadata{
				Inputs: Inputs{
					"my_input1": {"MY_INPUT1", false, InputTypeString},
					"my_input2": {"MY_INPUT2", false, InputTypeNumber},
				},
				Outputs: Outputs{
					"my_output1": {"MY_OUTPUT1"},
					"my_output2": {"MY_OUTPUT2"},
				},
				Secrets: Secrets{
					"my_secret1": {"MY_SECRET1", false},
					"my_secret2": {"MY_SECRET2", true},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			src := strings.TrimSpace(tc.src)
			src = strings.ReplaceAll(src, "\t", "")

			m, err := ParseMetadata([]byte(src))
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(m, tc.want); diff != "" {
				t.Fatal("Parse result is unexpected. diff:\n" + diff)
			}
		})
	}
}

func TestReusableWorkflowUnmarshalOnNodeNotFound(t *testing.T) {
	t.Parallel()
	src := "hello: world"
	_, err := ParseMetadata([]byte(src))
	if err == nil {
		t.Fatal("Error did not happen")
	}
	if msg := err.Error(); msg != "\"on:\" is not found" {
		t.Fatal("Unexpected error:", msg)
	}
}

func TestReusableWorkflowUnmarshalEventNotFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		what string
		src  string
		line int
		col  int
	}{
		{
			what: "scalar",
			src: `
			on: push
			`,
			line: 1,
			col:  5,
		},
		{
			what: "sequence",
			src: `
			on: [push, pull_request]
			`,
			line: 1,
			col:  5,
		},
		{
			what: "empty sequence",
			src: `
			on: []
			`,
			line: 1,
			col:  5,
		},
		{
			what: "mapping",
			src: `
			on:
			  push:
			  pull_request:
			`,
			line: 2,
			col:  3,
		},
		{
			what: "empty mapping",
			src: `
			on: {}
			`,
			line: 1,
			col:  5,
		},
		{
			what: "null",
			src: `
			on:
			`,
			line: 1,
			col:  4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			t.Parallel()
			src := strings.TrimSpace(tc.src)
			src = strings.ReplaceAll(src, "\t", "")

			_, err := ParseMetadata([]byte(src))
			if err == nil {
				t.Fatal("Error did not happen")
			}

			msg := err.Error()
			if !strings.Contains(msg, "\"workflow_call\" event trigger is not found in \"on:\"") {
				t.Fatal("Unexpected error:", msg)
			}
			loc := fmt.Sprintf("line:%d, column:%d", tc.line, tc.col)
			if !strings.Contains(msg, loc) {
				t.Fatalf("location is not %q: %s", loc, msg)
			}
		})
	}
}

func TestReusableWorkflowMetadataInvalidAnchorName(t *testing.T) {
	t.Parallel()
	src := []byte(`on: workflow_call
jobs:
  first: &build+job
    runs-on: ubuntu-latest
    steps:
      - run: echo test
  second: *build+job
`)
	_, err := ParseMetadata(src)
	if err == nil || !strings.Contains(err.Error(), `line:3, column:10: anchor name "build+job"`) {
		t.Fatalf("wanted the invalid anchor diagnostic, got %v", err)
	}
}
